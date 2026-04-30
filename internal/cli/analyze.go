package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/whtssub/kubectl-snapshot/internal/snapshot"
)

func newAnalyzeCommand() *cobra.Command {
	var maxItems int
	var minSeverity string
	var hideResourceMix bool
	var hideWarningEvents bool
	var outputFormat string

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
	cmd.Flags().StringVar(&outputFormat, "output", "text", "Output format: text (default) or json")
	return cmd
}
