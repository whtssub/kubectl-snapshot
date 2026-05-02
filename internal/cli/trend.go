package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/whtssub/kubectl-snapshot/internal/snapshot"
)

func newTrendCommand() *cobra.Command {
	var indexPath string
	var last int

	cmd := &cobra.Command{
		Use:   "trend [snapshot.json ...]",
		Short: "Show restart/pod-count trends across multiple snapshots",
		Long: `Compare pod counts, node counts, restarts, and warning events across snapshots.

Provide snapshot files as positional arguments, or omit them to read the last N
snapshots from the local history index (~/.kubectl-snapshot/history.json).

Examples:
  kubectl snapshot trend snap-before.json snap-after.json
  kubectl snapshot trend snap1.json snap2.json snap3.json
  kubectl snapshot trend --last 5`,
		RunE: func(cmd *cobra.Command, args []string) error {
			paths := args

			if len(paths) == 0 {
				if indexPath == "" {
					var err error
					indexPath, err = snapshot.DefaultIndexPath()
					if err != nil {
						return err
					}
				}
				idx, err := snapshot.LoadIndex(indexPath)
				if err != nil {
					return fmt.Errorf("load history index: %w", err)
				}
				if len(idx.Entries) == 0 {
					return fmt.Errorf("no snapshots in history index — provide snapshot files as arguments or run capture first")
				}
				entries := idx.Entries
				if len(entries) > last {
					entries = entries[len(entries)-last:]
				}
				for _, e := range entries {
					paths = append(paths, e.Path)
				}
			}

			if len(paths) < 2 {
				return fmt.Errorf("trend requires at least 2 snapshots; got %d", len(paths))
			}

			bundles := make([]*snapshot.Bundle, 0, len(paths))
			for _, p := range paths {
				b, err := snapshot.ReadBundle(p)
				if err != nil {
					return fmt.Errorf("read %s: %w", p, err)
				}
				bundles = append(bundles, b)
			}

			report := snapshot.ComputeTrend(bundles)
			cmd.Print(snapshot.RenderTrend(report))
			return nil
		},
	}

	cmd.Flags().StringVar(&indexPath, "index", "", "Path to history index file (default: ~/.kubectl-snapshot/history.json)")
	cmd.Flags().IntVar(&last, "last", 5, "Number of recent snapshots from history to compare (when no files given)")
	return cmd
}
