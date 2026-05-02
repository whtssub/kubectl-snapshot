package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func NewRootCommand(version, commit, date string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "kubectl-snapshot",
		Short: "Capture and diff Kubernetes cluster snapshots",
		Long:  "kubectl-snapshot captures a portable cluster-state bundle and diffs snapshots for post-incident analysis.",
		RunE: func(cmd *cobra.Command, args []string) error {
			showVersion, _ := cmd.Flags().GetBool("version")
			if showVersion {
				if len(version) > 0 && version[0] != 'v' {
					version = "v" + version
				}
				fmt.Fprintf(cmd.OutOrStdout(), "kubectl-snapshot %s (commit: %s, built: %s)\n", version, commit, date)
				return nil
			}
			return cmd.Help()
		},
	}

	cmd.AddCommand(newCaptureCommand())
	cmd.AddCommand(newDiffCommand())
	cmd.AddCommand(newAnalyzeCommand())
	cmd.AddCommand(newVersionCommand(version, commit, date))
	cmd.AddCommand(newHistoryCommand())
	cmd.AddCommand(newTrendCommand())

	cmd.Flags().BoolVarP(new(bool), "version", "v", false, "print version and exit")

	return cmd
}

func newVersionCommand(version, commit, date string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			if len(version) > 0 && version[0] != 'v' {
				version = "v" + version
			}
			fmt.Fprintf(cmd.OutOrStdout(), "kubectl-snapshot %s (commit: %s, built: %s)\n", version, commit, date)
		},
	}
}
