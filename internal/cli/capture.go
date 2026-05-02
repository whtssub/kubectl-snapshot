package cli

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/whtssub/kubectl-snapshot/internal/snapshot"
)

func newCaptureCommand() *cobra.Command {
	var kubeconfigPath string
	var namespace string
	var outputPath string
	var resources []string
	var compress string
	var labelSelector string
	var indexPath string
	var noIndex bool

	cmd := &cobra.Command{
		Use:   "capture",
		Short: "Capture a cluster snapshot bundle",
		Long: `Capture a point-in-time snapshot of cluster resources to a JSON bundle.

By default all supported resource types are captured. Use --resources to limit
the capture to specific types:

  kubectl snapshot capture -o snap.json
  kubectl snapshot capture --resources pods,deployments,services -o snap.json
  kubectl snapshot capture --resources deploy,cm,pvc -o snap.json
  kubectl snapshot capture --resources myapp.io/v1/widgets -o snap.json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if outputPath == "" {
				return fmt.Errorf("--output is required")
			}

			client, cfg, err := newDynamicClient(kubeconfigPath)
			if err != nil {
				return err
			}

			opts := snapshot.CaptureOptions{Resources: resources, LabelSelector: labelSelector}

			ctx := context.Background()
			bundle, err := snapshot.Capture(ctx, client, namespace, cfg.CurrentContext, opts)
			if err != nil {
				return err
			}

			wopts := snapshot.WriteOptions{Compress: compress}
			if err := snapshot.WriteBundleWithOptions(outputPath, bundle, wopts); err != nil {
				return err
			}

			cmd.Printf("snapshot saved: %s (%d records across %d resource types)\n",
				outputPath, len(bundle.Records), len(bundle.Metadata.CapturedResources))

			if len(bundle.Metadata.SkippedResources) > 0 {
				cmd.Printf("skipped: %v\n", bundle.Metadata.SkippedResources)
			}

			if !noIndex {
				idxPath := indexPath
				if idxPath == "" {
					idxPath, err = snapshot.DefaultIndexPath()
					if err != nil {
						cmd.Printf("warning: could not resolve history index path: %v\n", err)
						return nil
					}
				}
				absOutput, absErr := filepath.Abs(outputPath)
				if absErr != nil {
					absOutput = outputPath
				}
				fi, statErr := snapshot.StatBundle(outputPath)
				var sizeBytes int64
				if statErr == nil {
					sizeBytes = fi
				}
				indexErr := snapshot.AddToIndex(idxPath, snapshot.HistoryEntry{
					Path:         absOutput,
					CapturedAt:   bundle.Metadata.CapturedAt,
					Cluster:      bundle.Metadata.ClusterHint,
					TotalRecords: len(bundle.Records),
					SizeBytes:    sizeBytes,
				})
				if indexErr != nil {
					cmd.Printf("warning: could not update history index: %v\n", indexErr)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&kubeconfigPath, "kubeconfig", "", "Path to kubeconfig file")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Namespace scope (default all namespaces)")
	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Output bundle path (.json)")
	cmd.Flags().StringSliceVar(&resources, "resources", nil,
		"Comma-separated resource types to capture (default: all).\n"+
			"Accepts short names (pods,deploy,cm), plural names, or\n"+
			"group/version/resource triples (myapp.io/v1/widgets).")
	cmd.Flags().StringVar(&compress, "compress", "", "Compress output bundle: gzip")
	cmd.Flags().StringVarP(&labelSelector, "selector", "l", "", "Label selector to filter captured resources (e.g. app=frontend)")
	cmd.Flags().StringVar(&indexPath, "index", "", "Path to history index file (default: ~/.kubectl-snapshot/history.json)")
	cmd.Flags().BoolVar(&noIndex, "no-index", false, "Skip adding this snapshot to the local history index")
	return cmd
}
