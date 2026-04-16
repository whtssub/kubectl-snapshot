package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/subhasmita/kubectl-snapshot/internal/snapshot"
)

func newDiffCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff <before.json> <after.json>",
		Short: "Diff two snapshot bundles",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			before, err := snapshot.ReadBundle(args[0])
			if err != nil {
				return fmt.Errorf("read before bundle: %w", err)
			}
			after, err := snapshot.ReadBundle(args[1])
			if err != nil {
				return fmt.Errorf("read after bundle: %w", err)
			}

			report, err := snapshot.Diff(before, after)
			if err != nil {
				return err
			}
			cmd.Print(report)
			return nil
		},
	}
	return cmd
}
