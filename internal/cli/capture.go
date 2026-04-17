package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/whtssub/kubectl-snapshot/internal/snapshot"
)

func newCaptureCommand() *cobra.Command {
	var kubeconfigPath string
	var namespace string
	var outputPath string
	var resources []string
	var compress string

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

			opts := snapshot.CaptureOptions{Resources: resources}

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
	return cmd
}
