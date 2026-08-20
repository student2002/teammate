// output.go 提供 CLI 输出格式化工具，支持 JSON/YAML/表格等格式。
package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"text/tabwriter"

	"gopkg.in/yaml.v3"
)

// OutputFormat 表示输出格式类型。
type OutputFormat string

const (
	FormatTable OutputFormat = "table"
	FormatJSON  OutputFormat = "json"
	FormatYAML  OutputFormat = "yaml"
)

// parseOutputFormat 将字符串解析为 OutputFormat。
func parseOutputFormat(s string) OutputFormat {
	switch strings.ToLower(s) {
	case "json":
		return FormatJSON
	case "yaml":
		return FormatYAML
	default:
		return FormatTable
	}
}

// printOutput 以指定格式将数据输出到 stdout。
func printOutput(data interface{}, f OutputFormat) error {
	return fprintOutput(os.Stdout, data, f)
}

// fprintOutput 以指定格式将数据输出到给定的 writer。
func fprintOutput(w io.Writer, data interface{}, f OutputFormat) error {
	switch f {
	case FormatJSON:
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(data)
	case FormatYAML:
		enc := yaml.NewEncoder(w)
		return enc.Encode(data)
	case FormatTable:
		return printTable(w, data)
	default:
		return printTable(w, data)
	}
}

// printTable 使用 tabwriter 将数据渲染为简单表格。
func printTable(w io.Writer, data interface{}) error {
	v := reflect.ValueOf(data)

	if !v.IsValid() {
		fmt.Fprintln(w, "(no data)")
		return nil
	}

	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			fmt.Fprintln(w, "(no data)")
			return nil
		}
		v = v.Elem()
	}

	if v.Kind() == reflect.Slice || v.Kind() == reflect.Array {
		if v.Len() == 0 {
			fmt.Fprintln(w, "(no results)")
			return nil
		}
		return printSliceTable(w, v)
	}

	return printStructKV(w, v)
}

func printSliceTable(w io.Writer, v reflect.Value) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

	elem := v.Index(0)
	if elem.Kind() == reflect.Ptr {
		elem = elem.Elem()
	}
	if elem.Kind() != reflect.Struct {
		for i := 0; i < v.Len(); i++ {
			fmt.Fprintln(tw, v.Index(i).Interface())
		}
		return tw.Flush()
	}

	fields := getExportedFields(elem)

	headers := make([]string, len(fields))
	for i, f := range fields {
		headers[i] = strings.ToUpper(f)
	}
	fmt.Fprintln(tw, strings.Join(headers, "\t"))

	for i := 0; i < v.Len(); i++ {
		elem := v.Index(i)
		if elem.Kind() == reflect.Ptr {
			elem = elem.Elem()
		}
		row := make([]string, len(fields))
		for j, f := range fields {
			fv := elem.FieldByName(f)
			row[j] = fmtValue(fv)
		}
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}

	return tw.Flush()
}

func printStructKV(w io.Writer, v reflect.Value) error {
	if v.Kind() != reflect.Struct {
		fmt.Fprintln(w, v.Interface())
		return nil
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fields := getExportedFields(v)
	for _, f := range fields {
		fv := v.FieldByName(f)
		fmt.Fprintf(tw, "%s:\t%s\n", f, fmtValue(fv))
	}
	return tw.Flush()
}

func getExportedFields(v reflect.Value) []string {
	t := v.Type()
	var fields []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue
		}
		tag := f.Tag.Get("db")
		if tag == "-" {
			continue
		}
		fields = append(fields, f.Name)
	}
	return fields
}

func fmtValue(v reflect.Value) string {
	if !v.IsValid() {
		return ""
	}

	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return ""
		}
		v = v.Elem()
	}

	// 处理实现了 Stringer 接口的类型
	if v.CanInterface() {
		if s, ok := v.Interface().(fmt.Stringer); ok {
			return s.String()
		}
	}

	// 处理 sql.Null* 类型
	if v.CanInterface() {
		switch nv := v.Interface().(type) {
		case sql.NullString:
			if nv.Valid {
				return nv.String
			}
			return ""
		case sql.NullInt64:
			if nv.Valid {
				return fmt.Sprintf("%d", nv.Int64)
			}
			return ""
		case sql.NullFloat64:
			if nv.Valid {
				return fmt.Sprintf("%f", nv.Float64)
			}
			return ""
		case sql.NullBool:
			if nv.Valid {
				return fmt.Sprintf("%t", nv.Bool)
			}
			return ""
		case sql.NullTime:
			if nv.Valid {
				return nv.Time.Format("2006-01-02 15:04:05")
			}
			return ""
		}
	}

	switch v.Kind() {
	case reflect.Slice, reflect.Array:
		// 特殊情况：[16]byte (uuid.UUID)
		if v.Type().Elem().Kind() == reflect.Uint8 && v.Len() == 16 {
			// 尝试格式化为 UUID
			var buf [16]byte
			for i := 0; i < 16; i++ {
				buf[i] = byte(v.Index(i).Uint())
			}
			return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:])
		}
		if v.Len() == 0 {
			return ""
		}
		parts := make([]string, v.Len())
		for i := 0; i < v.Len(); i++ {
			parts[i] = fmtValue(v.Index(i))
		}
		return strings.Join(parts, ", ")
	case reflect.Interface:
		if v.IsNil() {
			return ""
		}
		return fmt.Sprintf("%v", v.Interface())
	case reflect.Struct:
		// 对于上述未捕获到的类似 NullString 的结构体，
		// 尝试查找并使用 .String 字段
		sf := v.FieldByName("String")
		if sf.IsValid() && sf.Kind() == reflect.String {
			validF := v.FieldByName("Valid")
			if validF.IsValid() && validF.Kind() == reflect.Bool {
				if validF.Bool() {
					return sf.String()
				}
				return ""
			}
		}
		return fmt.Sprintf("%v", v.Interface())
	default:
		return fmt.Sprintf("%v", v.Interface())
	}
}
