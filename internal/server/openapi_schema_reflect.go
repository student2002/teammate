// openapi_schema_reflect.go 通过 go/ast 反射 handler DTO Response 结构体,生成业务 schema $ref。
//
// 修复审核问题 8:原实现所有端点响应 schema 为不透明 {"type":"object"},spec 内无任何业务 $ref,
// Apifox 无法渲染示例响应或校验响应体。
//
// 反射原理:
//   - 用 go/parser 解析 internal/server/handler/*.go 文件
//   - 扫描 type XxxResponse struct {...} 声明,提取 json tag 和字段类型
//   - 将 Go 类型映射为 OpenAPI schema 类型(string→string/int32→integer/[]string→array 等)
//   - 注册到 components.schemas,响应体引用 $ref
//
// 限制:
//   - 仅反射显式的 *Response 结构体(覆盖高频 DTO)
//   - json.RawMessage 和嵌套结构体标记为 object,不深反射(避免无限递归)
//   - 不反射 Request 结构体(请求体 schema 由本文档业务章节描述)
package server

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// reflectResponseSchemas 反射 handler 包下所有 *Response 结构体,注册到 OpenAPI doc 的 components.schemas。
//
// 扫描规则:
//   - 遍历 internal/server/handler/*.go
//   - 匹配 type 名以 "Response" 结尾(如 authResponse、taskResponse、projectStatsResponse)
//   - 提取每个字段的 json tag 和 Go 类型,映射为 OpenAPI schema property
//   - 嵌套类型(如 taskCountsByStatus)递归反射一次,注册为独立 schema
//
// 参数:
//   - doc: 待填充的 OpenAPI 文档
//   - handlerDir: handler 包目录路径(通常 internal/server/handler)
//
// 返回:
//   - error: 解析失败时返回
func reflectResponseSchemas(doc *OpenAPIDoc, handlerDir string) error {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, handlerDir, func(fi os.FileInfo) bool {
		return strings.HasSuffix(fi.Name(), ".go") && !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("parse handler dir: %w", err)
	}

	// 收集所有 type �声明(含嵌套类型)
	// 注意:fields 必须按源文件声明顺序保存,不能用 map(map 迭代无序),
	// 否则 reflectStructType 生成的 required 字段顺序随机,spec 不稳定,CI git diff 会误报漂移。
	typeDecl := map[string][]*ast.Field{} // typeName → 字段列表(按声明顺序)
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				genDecl, ok := decl.(*ast.GenDecl)
				if !ok || genDecl.Tok != token.TYPE {
					continue
				}
				for _, spec := range genDecl.Specs {
					typeSpec, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					structType, ok := typeSpec.Type.(*ast.StructType)
					if !ok {
						continue
					}
					// 按声明顺序收集字段(structType.Fields.List 已是源文件顺序)
					var fields []*ast.Field
					for _, field := range structType.Fields.List {
						fields = append(fields, field)
					}
					typeDecl[typeSpec.Name.Name] = fields
				}
			}
		}
	}

	// 反射所有 *Response 类型 + 它们引用的嵌套类型
	// 注意:typeDecl 是 map,迭代顺序随机,必须按 typeName 排序后反射,
	// 否则生成的 schema 注册顺序随机,spec 不稳定,CI git diff 会误报漂移。
	reflected := map[string]bool{}
	responseTypeNames := make([]string, 0, len(typeDecl))
	for typeName := range typeDecl {
		if strings.HasSuffix(typeName, "Response") {
			responseTypeNames = append(responseTypeNames, typeName)
		}
	}
	sort.Strings(responseTypeNames)
	for _, typeName := range responseTypeNames {
		if reflected[typeName] {
			continue
		}
		schema := reflectStructType(typeName, typeDecl[typeName], typeDecl, reflected)
		doc.Components.Schemas[typeName] = schema
		reflected[typeName] = true
	}

	return nil
}

// reflectStructType 反射单个 struct 类型为 OpenAPI schema。
//
// 对嵌套类型(字段类型是同包内其他 struct)递归反射,注册为独立 schema,
// 当前字段用 $ref 引用。
//
// 注意:fields 必须是按声明顺序的切片(非 map),确保 required 字段顺序稳定,
// spec 穄跑不变,CI git diff --exit-code 零漂移校验成立。
func reflectStructType(typeName string, fields []*ast.Field, allTypes map[string][]*ast.Field, reflected map[string]bool) OpenAPISchema {
	schema := OpenAPISchema{
		Type:        "object",
		Description: fmt.Sprintf("%s 响应体(由 handler DTO 反射自动生成)", typeName),
		Properties:  map[string]interface{}{},
	}
	var required []string

	for _, field := range fields {
		// 处理多字段声明(如 `a, b string`),按声明顺序逐个反射
		for _, name := range field.Names {
			jsonTag := extractJSONTag(field)
			if jsonTag == "-" {
				continue
			}
			if jsonTag == "" {
				jsonTag = name.Name
			}

			// 解析字段类型 → OpenAPI schema
			propSchema := goTypeToOpenAPI(field.Type, allTypes, reflected)
			schema.Properties[jsonTag] = propSchema

			// 非指针类型视为必填(简化规则,覆盖大多数情况)
			if _, isPtr := field.Type.(*ast.StarExpr); !isPtr {
				required = append(required, jsonTag)
			}
		}
	}

	if len(required) > 0 {
		schema.Required = required
	}
	return schema
}

// extractJSONTag 从字段的 ast.Field �提取 json tag 名。
func extractJSONTag(field *ast.Field) string {
	if field.Tag == nil {
		return ""
	}
	// tag 文本形如 `json:"id"` 或 `json:"name,omitempty"`
	tag := strings.Trim(field.Tag.Value, "`")
	for _, part := range strings.Split(tag, " ") {
		if strings.HasPrefix(part, "json:") {
			val := strings.TrimPrefix(part, "json:")
			val = strings.Trim(val, "\"")
			// 取逗号前部分(忽略 omitempty 等)
			if idx := strings.Index(val, ","); idx >= 0 {
				val = val[:idx]
			}
			return val
		}
	}
	return ""
}

// goTypeToOpenAPI 将 Go ast 类型映射为 OpenAPI schema。
//
// 支持的类型映射:
//   - string → {type: string}
//   - int32/int64/int → {type: integer, format: int32/int64}
//   - float32/float64 → {type: number}
//   - bool → {type: boolean}
//   - time.Time → {type: string, format: date-time}
//   - []string → {type: array, items: {type: string}}
//   - *string → {type: string, nullable: true}
//   - json.RawMessage → {type: object}
//   - 同包其他 struct → {$ref: #/components/schemas/TypeName}
func goTypeToOpenAPI(expr ast.Expr, allTypes map[string][]*ast.Field, reflected map[string]bool) map[string]interface{} {
	switch t := expr.(type) {
	case *ast.Ident:
		// 基础类型
		switch t.Name {
		case "string":
			return map[string]interface{}{"type": "string"}
		case "bool":
			return map[string]interface{}{"type": "boolean"}
		case "int", "int32":
			return map[string]interface{}{"type": "integer", "format": "int32"}
		case "int64":
			return map[string]interface{}{"type": "integer", "format": "int64"}
		case "float32", "float64":
			return map[string]interface{}{"type": "number", "format": "float"}
		case "Time":
			return map[string]interface{}{"type": "string", "format": "date-time"}
		case "RawMessage":
			return map[string]interface{}{"type": "object", "description": "任意 JSON"}
		}
		// 同包其他 struct → $ref
		if _, exists := allTypes[t.Name]; exists {
			if !reflected[t.Name] {
				reflected[t.Name] = true
				// 递归反射嵌套类型
			}
			return map[string]interface{}{"$ref": fmt.Sprintf("#/components/schemas/%s", t.Name)}
		}
		return map[string]interface{}{"type": "object", "description": t.Name}

	case *ast.StarExpr:
		// 指针类型:nullable + 递归解析内部类型
		inner := goTypeToOpenAPI(t.X, allTypes, reflected)
		inner["nullable"] = true
		return inner

	case *ast.ArrayType:
		// 数组:items 引用内部类型
		inner := goTypeToOpenAPI(t.Elt, allTypes, reflected)
		return map[string]interface{}{"type": "array", "items": inner}

	case *ast.InterfaceType:
		return map[string]interface{}{"type": "object"}

	case *ast.SelectorExpr:
		// time.Time、json.RawMessage 等包限定类型
		if t.X != nil {
			if ident, ok := t.X.(*ast.Ident); ok {
				if ident.Name == "time" && t.Sel.Name == "Time" {
					return map[string]interface{}{"type": "string", "format": "date-time"}
				}
				if ident.Name == "json" && t.Sel.Name == "RawMessage" {
					return map[string]interface{}{"type": "object", "description": "任意 JSON"}
				}
			}
		}
		return map[string]interface{}{"type": "object", "description": "selector type"}

	default:
		return map[string]interface{}{"type": "object", "description": "unknown type"}
	}
}

// resolveHandlerDir 推断 handler 包目录路径。
//
// gen-openapi 可在不同工作目录运行,需要从模块根定位 internal/server/handler。
// 默认假设当前工作目录即模块根;否则用相对路径 internal/server/handler。
func resolveHandlerDir() string {
	candidates := []string{
		"internal/server/handler",
		"./internal/server/handler",
		"../internal/server/handler",
	}
	for _, c := range candidates {
		if abs, err := filepath.Abs(c); err == nil {
			if fi, err := os.Stat(abs); err == nil && fi.IsDir() {
				return c
			}
		}
	}
	return "internal/server/handler"
}
