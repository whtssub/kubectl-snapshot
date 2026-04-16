package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/subhasmita/kubectl-snapshot/internal/snapshot"
)

func newCaptureCommand() *cobra.Command {
	var kubeconfigPath string
	var namespace string
	var outputPath string

	cmd := &cobra.Command{
		Use:   "capture",
		Short: "Capture a cluster snapshot bundle",
		RunE: func(cmd *cobra.Command, args []string) error {
			if outputPath == "" {
				return fmt.Errorf("--output is required")
			}

			client, cfg, err := newDynamicClient(kubeconfigPath)
			if err != nil {
				return err
			}

			ctx := context.Background()
			bundle, err := snapshot.Capture(ctx, client, namespace, cfg.CurrentContext)
			if err != nil {
				return err
			}

			if err := snapshot.WriteBundle(outputPath, bundle); err != nil {
				return err
			}

			cmd.Printf("snapshot saved: %s (%d records)\n", outputPath, len(bundle.Records))
			return nil
		},
	}

	cmd.Flags().StringVar(&kubeconfigPath, "kubeconfig", "", "Path to kubeconfig file")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Namespace scope (default all namespaces)")
	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Output bundle path (.json)")
	return cmd
}
