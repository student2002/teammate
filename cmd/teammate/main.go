// main.go is the entry point for the teammate CLI; it executes the root command and handles exit codes.
package main

import "os"

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
