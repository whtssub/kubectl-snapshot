package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/whtssub/kubectl-snapshot/internal/snapshot"
)

func newHistoryCommand() *cobra.Command {
	var indexPath string

	cmd := &cobra.Command{
		Use:   "history",
		Short: "List previously captured snapshots",
		Long: `Show the local index of captured snapshots.

Snapshots are automatically indexed when captured with kubectl-snapshot capture.
The index is stored at ~/.kubectl-snapshot/history.json by default.`,
		RunE: func(cmd *cobra.Command, args []string) error {
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
				cmd.Println("no snapshots indexed yet — run `kubectl snapshot capture` to add one")
				return nil
			}

			cmd.Printf("%-5s  %-22s  %-20s  %7s  %s\n", "#", "Captured At", "Cluster", "Records", "Path")
			cmd.Println(strings.Repeat("─", 90))
			// Print newest first
			for i := len(idx.Entries) - 1; i >= 0; i-- {
				e := idx.Entries[i]
				num := len(idx.Entries) - i
				cluster := e.Cluster
				if cluster == "" {
					cluster = "(unknown)"
				}
				if len(cluster) > 20 {
					cluster = cluster[:17] + "..."
				}
				cmd.Printf("%-5d  %-22s  %-20s  %7d  %s\n",
					num,
					e.CapturedAt.Format("2006-01-02 15:04:05"),
					cluster,
					e.TotalRecords,
					e.Path,
				)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&indexPath, "index", "", "Path to history index file (default: ~/.kubectl-snapshot/history.json)")
	return cmd
}
