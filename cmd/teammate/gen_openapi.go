// gen_openapi.go 提供 gen-openapi 子命令,通过 chi.Walk 反射生产路由树生成 OpenAPI 3.1 spec。
//
// 设计原则:
//   - 路由注册是唯一真相源——反射结果必然和 routes_*.go 中的 r.Get/r.Post 一致
//   - 不连接真实 DB/Redis——gen-openapi 只需要路由结构,不需要运行时数据
//   - 生成产物 docs/apifox/teammate-openapi.json 可直接导入 Apifox/Postman
//
// 用法:
//
//	go run ./cmd/teammate gen-openapi -o docs/apifox/teammate-openapi.json
//
// CI 集成:生成后 git diff --exit-code 确保提交的 spec 与代码同步。
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/teammate/server/internal/server"
)

var (
	genOpenAPIOutput string // 输出文件路径
	genOpenAPIIndent bool   // 是否缩进 JSON
)

var genOpenAPICmd = &cobra.Command{
	Use:   "gen-openapi",
	Short: "[dev] Generate OpenAPI spec by reflecting the chi router tree",
	Long: `Generate OpenAPI 3.1 spec by reflecting the production chi router tree.

This command constructs the same router used by 'teammate-server', walks it
with chi.Walk, and emits an OpenAPI 3.1 JSON document. The router registration
in routes_*.go is the single source of truth—any route change is automatically
reflected in the generated spec, eliminating documentation drift.

Output:
  - Default: docs/apifox/teammate-openapi.json
  - Use -o to specify a different path
  - Use --compact for minified JSON (default: indented)

Exit codes:
  0 = success
  1 = generation error
`,
	RunE: runGenOpenAPI,
}

func init() {
	genOpenAPICmd.Flags().StringVarP(&genOpenAPIOutput, "output", "o",
		"docs/apifox/teammate-openapi.json", "output file path")
	genOpenAPICmd.Flags().BoolVar(&genOpenAPIIndent, "indent", true,
		"indent JSON output (pretty-print)")

	// gen-openapi 是开发工具,不需要认证
	skipAuthCommands["teammate gen-openapi"] = true
	rootCmd.AddCommand(genOpenAPICmd)
}

func runGenOpenAPI(cmd *cobra.Command, args []string) error {
	// 构建生产路由树(不连接真实 DB/Redis)
	router, err := server.BuildRouterForOpenAPI()
	if err != nil {
		return fmt.Errorf("build router: %w", err)
	}

	// 反射路由树生成 OpenAPI spec
	spec, err := server.ReflectOpenAPI(router)
	if err != nil {
		return fmt.Errorf("reflect openapi: %w", err)
	}

	// 序列化 JSON
	var data []byte
	if genOpenAPIIndent {
		data, err = json.MarshalIndent(spec, "", "  ")
	} else {
		data, err = json.Marshal(spec)
	}
	if err != nil {
		return fmt.Errorf("marshal spec: %w", err)
	}

	// 写入文件
	if err := os.WriteFile(genOpenAPIOutput, data, 0644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	// 统计端点数
	pathCount := len(spec.Paths)
	endpointCount := 0
	for _, item := range spec.Paths {
		endpointCount += len(item.Operations())
	}

	fmt.Fprintf(os.Stderr, "OpenAPI spec generated: %s\n", genOpenAPIOutput)
	fmt.Fprintf(os.Stderr, "  paths:      %d\n", pathCount)
	fmt.Fprintf(os.Stderr, "  endpoints:  %d\n", endpointCount)
	fmt.Fprintf(os.Stderr, "  schemas:    %d\n", len(spec.Components.Schemas))

	return nil
}
