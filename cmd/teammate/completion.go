// completion.go 实现 shell 补全子命令，生成各 shell 的补全脚本。
package main

import (
	"github.com/spf13/cobra"
)

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell completion script",
	Long: `To load completions:

Bash:
  $ source <(teammate completion bash)

  # To load completions for each session, execute once:
  # Linux:
  $ teammate completion bash > /etc/bash_completion.d/teammate
  # macOS:
  $ teammate completion bash > /usr/local/etc/bash_completion.d/teammate

Zsh:
  # If shell completion is not already enabled in your environment,
  # you will need to enable it.  You can execute the following once:
  $ echo "autoload -U compinit; compinit" >> ~/.zshrc

  # To load completions for each session, execute once:
  $ teammate completion zsh > "${fpath[1]}/_teammate"

  # You will need to start a new shell for this setup to take effect.

fish:
  $ teammate completion fish | source

  # To load completions for each session, execute once:
  $ teammate completion fish > ~/.config/fish/completions/teammate.fish

PowerShell:
  PS> teammate completion powershell | Out-String | Invoke-Expression

  # To load completions for every new session, run:
  PS> teammate completion powershell > teammate.ps1
  # and source this file from your PowerShell profile.
`,
	DisableFlagsInUseLine: true,
	ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
	Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return rootCmd.GenBashCompletion(cmd.OutOrStdout())
		case "zsh":
			return rootCmd.GenZshCompletion(cmd.OutOrStdout())
		case "fish":
			return rootCmd.GenFishCompletion(cmd.OutOrStdout(), true)
		case "powershell":
			return rootCmd.GenPowerShellCompletionWithDesc(cmd.OutOrStdout())
		default:
			return nil
		}
	},
}

func init() {
	rootCmd.AddCommand(completionCmd)
}
