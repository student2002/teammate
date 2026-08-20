// boundary_test.go 验证分层架构边界与依赖方向的约束。
package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func goFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if strings.Contains(path, string(filepath.Separator)+"generated") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return files
}

func importsOf(t *testing.T, path string) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	imports := make([]string, 0, len(file.Imports))
	for _, spec := range file.Imports {
		imports = append(imports, strings.Trim(spec.Path.Value, `"`))
	}
	return imports
}

func TestServiceDoesNotImportServerPackages(t *testing.T) {
	for _, path := range goFiles(t, filepath.Join("..", "..", "internal", "service")) {
		for _, imp := range importsOf(t, path) {
			if strings.Contains(imp, "/internal/server/") {
				t.Fatalf("service file %s imports server package %s", path, imp)
			}
		}
	}
}

func TestHandlersDoNotImportStorePackage(t *testing.T) {
	for _, path := range goFiles(t, filepath.Join("..", "..", "internal", "server", "handler")) {
		for _, imp := range importsOf(t, path) {
			if strings.HasSuffix(imp, "/internal/store") {
				t.Fatalf("handler file %s imports store directly", path)
			}
		}
	}
}

// isDTOFile 报告给定文件路径是否为 handler 包中的 DTO 映射文件。
// DTO 文件包含可合法引用生成类型的映射辅助函数。
// DTO 文件包含可合法引用生成类型的映射辅助函数。
func isDTOFile(path string) bool {
	clean := filepath.ToSlash(filepath.Clean(path))
	return strings.HasSuffix(clean, "_dto.go")
}

func TestHandlersDoNotReachSqlcGeneratedDirectly(t *testing.T) {
	for _, path := range goFiles(t, filepath.Join("..", "..", "internal", "server", "handler")) {
		if isDTOFile(path) {
			continue
		}
		for _, imp := range importsOf(t, path) {
			if strings.HasSuffix(imp, "/internal/db/generated") {
				t.Fatalf("handler file %s imports sqlc generated package directly", path)
			}
		}
	}
}

func TestHandlersDoNotAccessStoreOrDB(t *testing.T) {
	handlerRoot := filepath.Join("..", "..", "internal", "server", "handler")
	fset := token.NewFileSet()

	for _, path := range goFiles(t, handlerRoot) {
		if isDTOFile(path) {
			continue
		}

		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}

		// 构建导入包名集合，以排除类似 sql.DB 的类型引用。
		importedNames := make(map[string]bool)
		for _, imp := range file.Imports {
			if imp.Name != nil {
				importedNames[imp.Name.Name] = true
			} else {
				// 提取导入路径的最后一段："database/sql" → "sql"
				p := strings.Trim(imp.Path.Value, `"`)
				if i := strings.LastIndex(p, "/"); i >= 0 {
					importedNames[p[i+1:]] = true
				} else {
					importedNames[p] = true
				}
			}
		}

		// 收集作为结构体字段类型出现的选择器的位置，
		// 例如 `DB *sql.DB`——选择器 `sql.DB` 是类型，而非访问。
		fieldTypePositions := make(map[token.Pos]bool)
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}
				for _, field := range st.Fields.List {
					ast.Inspect(field.Type, func(n ast.Node) bool {
						if sel, ok := n.(*ast.SelectorExpr); ok {
							fieldTypePositions[sel.Pos()] = true
						}
						return true
					})
				}
			}
		}

		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}

			outerName := sel.Sel.Name
			if outerName != "Store" && outerName != "DB" {
				return true
			}

			// 排除类似 sql.DB 的包限定类型引用。
			if ident, ok := sel.X.(*ast.Ident); ok && importedNames[ident.Name] {
				return true
			}

			// 排除作为结构体字段类型出现的选择器。
			if fieldTypePositions[sel.Pos()] {
				return true
			}

			pos := fset.Position(sel.Pos())
			t.Fatalf("handler file %s:%d accesses .%s — "+
				"handlers must go through service methods, not the store/DB directly",
				path, pos.Line, outerName)
			return true
		})
	}
}

// TestServiceDoesNotAccessStoreQDirectly 确保服务层文件不会
// 通过直接读取公共 Q 字段来绕过 Store 包装器。Q 字段
// 将在后续任务中私有化；此测试用于防止回归
// 一旦发生即可及时发现，并暴露现有的违规情况。
func TestServiceDoesNotAccessStoreQDirectly(t *testing.T) {
	serviceRoot := filepath.Join("..", "..", "internal", "service")
	fset := token.NewFileSet()

	for _, path := range goFiles(t, serviceRoot) {
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}

		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			// 匹配类似 `svc.Store.Q` 或 `s.svc.Store.Q` 的模式。
			if sel.Sel.Name != "Q" {
				return true
			}
			pos := fset.Position(sel.Pos())
			t.Fatalf("service file %s:%d accesses .Q directly — "+
				"services must call Store wrapper methods, not Store.Q",
				path, pos.Line)
			return true
		})
	}
}

func TestUpperLayersDoNotUseStoreEscapeHatches(t *testing.T) {
	roots := []string{
		filepath.Join("..", "..", "cmd"),
		filepath.Join("..", "..", "internal", "scheduler"),
		filepath.Join("..", "..", "internal", "service"),
	}
	fset := token.NewFileSet()

	for _, root := range roots {
		for _, path := range goFiles(t, root) {
			file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}

			ast.Inspect(file, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				switch sel.Sel.Name {
				case "Queries":
					pos := fset.Position(sel.Pos())
					t.Fatalf("%s:%d calls Store.Queries escape hatch; use Store methods instead", path, pos.Line)
				case "DB":
					if isPackageSelector(file, sel) {
						return true
					}
					if !isStoreFieldSelector(sel) {
						return true
					}
					pos := fset.Position(sel.Pos())
					t.Fatalf("%s:%d accesses Store.DB escape hatch; keep transactions inside store", path, pos.Line)
				}
				return true
			})
		}
	}
}

func TestMiddlewareDoesNotQueryDatabaseDirectly(t *testing.T) {
	middlewareRoot := filepath.Join("..", "..", "internal", "server", "middleware")
	fset := token.NewFileSet()

	for _, path := range goFiles(t, middlewareRoot) {
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}

		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch sel.Sel.Name {
			case "QueryContext", "QueryRowContext", "ExecContext":
				pos := fset.Position(sel.Pos())
				t.Fatalf("%s:%d middleware queries database directly; inject service/store-backed checkers instead", path, pos.Line)
			}
			return true
		})
	}
}

func TestServiceDoesNotExposeDB(t *testing.T) {
	path := filepath.Join("..", "..", "internal", "service", "service.go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != "Service" {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			for _, field := range st.Fields.List {
				for _, name := range field.Names {
					if name.IsExported() && name.Name == "DB" {
						pos := fset.Position(name.Pos())
						t.Fatalf("%s:%d Service exposes DB; services must access persistence through Store", path, pos.Line)
					}
				}
			}
		}
	}
}

func isStoreFieldSelector(sel *ast.SelectorExpr) bool {
	parent, ok := sel.X.(*ast.SelectorExpr)
	return ok && parent.Sel.Name == "Store"
}

func isPackageSelector(file *ast.File, sel *ast.SelectorExpr) bool {
	ident, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	for _, imp := range file.Imports {
		name := ""
		if imp.Name != nil {
			name = imp.Name.Name
		} else {
			path := strings.Trim(imp.Path.Value, `"`)
			if i := strings.LastIndex(path, "/"); i >= 0 {
				name = path[i+1:]
			} else {
				name = path
			}
		}
		if ident.Name == name {
			return true
		}
	}
	return false
}

func TestStoreDoesNotExposeDBOrQueries(t *testing.T) {
	path := filepath.Join("..", "..", "internal", "store", "store.go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || ts.Name.Name != "Store" {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}
				for _, field := range st.Fields.List {
					for _, name := range field.Names {
						if name.IsExported() && name.Name == "DB" {
							pos := fset.Position(name.Pos())
							t.Fatalf("%s:%d Store exposes DB; make it private and keep transaction helpers in store", path, pos.Line)
						}
					}
				}
			}
		case *ast.FuncDecl:
			if d.Recv != nil && d.Name.Name == "Queries" && d.Name.IsExported() {
				pos := fset.Position(d.Name.Pos())
				t.Fatalf("%s:%d Store exposes Queries(); keep sqlc queries private to store", path, pos.Line)
			}
		}
	}
}

// TestTypesDoesNotImportUpperLayers 确保共享的 types 包保持
// 无依赖：它不得导入 service、handler 或 store 包。
func TestTypesDoesNotImportUpperLayers(t *testing.T) {
	for _, path := range goFiles(t, filepath.Join("..", "..", "internal", "types")) {
		for _, imp := range importsOf(t, path) {
			if strings.Contains(imp, "/internal/service") ||
				strings.Contains(imp, "/internal/server") ||
				strings.Contains(imp, "/internal/store") {
				t.Fatalf("types file %s imports upper layer %s", path, imp)
			}
		}
	}
}

// TestServiceLayerDoesNotImportDbGenerated 确保 service 层不直接 import sqlc 生成包。
// service 必须通过 Store 的封装方法访问数据，类型用 internal/types 的 domain 类型。
func TestServiceLayerDoesNotImportDbGenerated(t *testing.T) {
	for _, path := range goFiles(t, filepath.Join("..", "..", "internal", "service")) {
		for _, imp := range importsOf(t, path) {
			if strings.HasSuffix(imp, "/internal/db/generated") {
				t.Fatalf("service file %s imports sqlc generated package directly — "+
					"services must use internal/types domain types, not db types", path)
			}
		}
	}
}

// TestHandlerDtoFilesDoNotImportDbGenerated 确保 handler 的 _dto.go 文件
// 不再通过类型别名引入 sqlc 生成类型。domain 类型应来自 internal/types。
func TestHandlerDtoFilesDoNotImportDbGenerated(t *testing.T) {
	for _, path := range goFiles(t, filepath.Join("..", "..", "internal", "server", "handler")) {
		if !isDTOFile(path) {
			continue // 非 DTO 文件由 TestHandlersDoNotReachSqlcGeneratedDirectly 兜底
		}
		for _, imp := range importsOf(t, path) {
			if strings.HasSuffix(imp, "/internal/db/generated") {
				t.Fatalf("handler DTO file %s imports sqlc generated package — "+
					"DTOs must use internal/types domain types", path)
			}
		}
	}
}

// TestStoreMethodsReturnDomainTypes 确保 Store 的公共方法签名
// 不再返回/接受 db.Xxx 类型，强制通过 domain 类型隔离。
// 用 AST 解析 Store 的方法签名，检查返回值和参数类型不引用 db 包。
func TestStoreMethodsReturnDomainTypes(t *testing.T) {
	storeRoot := filepath.Join("..", "..", "internal", "store")
	fset := token.NewFileSet()
	for _, path := range goFiles(t, storeRoot) {
		if strings.HasSuffix(path, "converter.go") {
			continue // 转换层本就该用 db 类型，跳过
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
				continue // 只看方法
			}
			// 检查 Recv 是否是 *Store（仅对 Store 的公共方法生效）
			recv, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
			if !ok {
				continue
			}
			recvIdent, ok := recv.X.(*ast.Ident)
			if !ok || recvIdent.Name != "Store" {
				continue
			}
			// 检查返回值和参数类型不引用 db. selector
			ast.Inspect(fn.Type, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "db" {
					pos := fset.Position(sel.Pos())
					t.Fatalf("store method %s in %s:%d references db.%s — "+
						"Store public methods must use types.* domain types, "+
						"db types only allowed in converter.go",
						fn.Name.Name, path, pos.Line, sel.Sel.Name)
				}
				return true
			})
		}
	}
}
