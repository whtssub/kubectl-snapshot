package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newCompletionCommand(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:       "completion [bash|zsh|fish|powershell]",
		Short:     "Generate shell completion scripts",
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		Args:      cobra.ExactArgs(1),
		Long: `Generate shell completion scripts for kubectl-snapshot.

Source the output in your shell profile to enable tab-completion:

  # bash (~/.bashrc or ~/.bash_profile)
  source <(kubectl snapshot completion bash)

  # zsh (~/.zshrc)
  source <(kubectl snapshot completion zsh)

  # fish (~/.config/fish/config.fish)
  kubectl snapshot completion fish | source

  # PowerShell ($PROFILE)
  kubectl snapshot completion powershell | Out-String | Invoke-Expression`,
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return root.GenBashCompletionV2(cmd.OutOrStdout(), true)
			case "zsh":
				return root.GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return root.GenFishCompletion(cmd.OutOrStdout(), true)
			case "powershell":
				return root.GenPowerShellCompletionWithDesc(cmd.OutOrStdout())
			default:
				return fmt.Errorf("unsupported shell %q: choose bash, zsh, fish, or powershell", args[0])
			}
		},
	}
}
