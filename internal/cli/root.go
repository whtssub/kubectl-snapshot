package cli

import (
	"github.com/spf13/cobra"
)

func NewRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "kubectl-snapshot",
		Short: "Capture and diff Kubernetes cluster snapshots",
		Long:  "kubectl-snapshot captures a portable cluster-state bundle and diffs snapshots for post-incident analysis.",
	}

	cmd.AddCommand(newCaptureCommand())
	cmd.AddCommand(newDiffCommand())
	cmd.AddCommand(newAnalyzeCommand())
	return cmd
}
