// main.go 是 teammate CLI 的入口，执行根命令并处理退出码。
package main

import "os"

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
