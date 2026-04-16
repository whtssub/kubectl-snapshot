package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/subhasmita/kubectl-snapshot/internal/snapshot"
)

func newAnalyzeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "analyze <snapshot.json>",
		Short: "Analyze a snapshot for incident signals",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			bundle, err := snapshot.ReadBundle(args[0])
			if err != nil {
				return fmt.Errorf("read snapshot bundle: %w", err)
			}

			report, err := snapshot.Analyze(bundle)
			if err != nil {
				return err
			}
			cmd.Print(report)
			return nil
		},
	}
	return cmd
}
