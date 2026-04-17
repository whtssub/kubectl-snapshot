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
	}

	cmd.AddCommand(newCaptureCommand())
	cmd.AddCommand(newDiffCommand())
	cmd.AddCommand(newAnalyzeCommand())
	cmd.AddCommand(newVersionCommand(version, commit, date))
	return cmd
}

func newVersionCommand(version, commit, date string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "kubectl-snapshot %s (commit: %s, built: %s)\n", version, commit, date)
		},
	}
}
