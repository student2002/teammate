// gen_openapi.go provides the gen-openapi subcommand, which generates an OpenAPI 3.1 spec by reflecting the production route tree via chi.Walk.
//
// Design principles:
//   - Route registration is the single source of truth — the reflection result is necessarily consistent with the r.Get/r.Post calls in routes_*.go
//   - Does not connect to a real DB/Redis — gen-openapi only needs the route structure, not runtime data
//   - The generated artifact docs/apifox/teammate-openapi.json can be imported directly into Apifox/Postman
//
// Usage:
//
//	go run ./cmd/teammate gen-openapi -o docs/apifox/teammate-openapi.json
//
// CI integration: after generation, run git diff --exit-code to ensure the committed spec stays in sync with the code.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/teammate/server/internal/server"
)

var (
	genOpenAPIOutput string // output file path
	genOpenAPIIndent bool   // whether to indent the JSON
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

	// gen-openapi is a development tool and does not require authentication
	skipAuthCommands["teammate gen-openapi"] = true
	rootCmd.AddCommand(genOpenAPICmd)
}

func runGenOpenAPI(cmd *cobra.Command, args []string) error {
	// Build the production route tree (does not connect to a real DB/Redis)
	router, err := server.BuildRouterForOpenAPI()
	if err != nil {
		return fmt.Errorf("build router: %w", err)
	}

	// Reflect the route tree to generate the OpenAPI spec
	spec, err := server.ReflectOpenAPI(router)
	if err != nil {
		return fmt.Errorf("reflect openapi: %w", err)
	}

	// Serialize JSON
	var data []byte
	if genOpenAPIIndent {
		data, err = json.MarshalIndent(spec, "", "  ")
	} else {
		data, err = json.Marshal(spec)
	}
	if err != nil {
		return fmt.Errorf("marshal spec: %w", err)
	}

	// Write to file
	if err := os.WriteFile(genOpenAPIOutput, data, 0644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	// Count endpoints
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
