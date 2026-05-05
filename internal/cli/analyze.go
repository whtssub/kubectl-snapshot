package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/whtssub/kubectl-snapshot/internal/snapshot"
)

func newAnalyzeCommand() *cobra.Command {
	var maxItems int
	var minSeverity string
	var hideResourceMix bool
	var hideWarningEvents bool
	var outputFormat string
	var since time.Duration
	var namespace string

	cmd := &cobra.Command{
		Use:   "analyze <snapshot.json>",
		Short: "Analyze a snapshot for incident signals",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			bundle, err := snapshot.ReadBundle(args[0])
			if err != nil {
				return fmt.Errorf("read snapshot bundle: %w", err)
			}

			report, err := snapshot.AnalyzeWithOptions(bundle, snapshot.AnalyzeOptions{
				MaxItems:          maxItems,
				MinSeverity:       minSeverity,
				HideResourceMix:   hideResourceMix,
				HideWarningEvents: hideWarningEvents,
				OutputFormat:      outputFormat,
				Since:             since,
				Namespace:         namespace,
			})
			if err != nil {
				return err
			}
			cmd.Print(report)
			return nil
		},
	}
	cmd.Flags().IntVar(&maxItems, "max-items", 15, "Maximum entries shown per section")
	cmd.Flags().StringVar(&minSeverity, "severity-threshold", "", "Only print details when severity is at least: low|medium|high")
	cmd.Flags().BoolVar(&hideResourceMix, "no-resource-mix", false, "Hide resource mix section")
	cmd.Flags().BoolVar(&hideWarningEvents, "no-warning-events", false, "Hide warning events section")
	cmd.Flags().StringVar(&outputFormat, "output", "text", "Output format: text (default), json, or sarif")
	cmd.Flags().DurationVar(&since, "since", 0, "Only include warning events from the last duration (e.g. 1h, 30m, 24h)")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Restrict analysis to a single namespace (cluster-scoped resources are always included)")
	return cmd
}
