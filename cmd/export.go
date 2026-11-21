package cmd

import (
	"fmt"

	"github.com/k0sproject/k0sind/pkg/cluster"
	"github.com/spf13/cobra"
)

func newExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export cluster artifacts",
	}
	cmd.AddCommand(newExportKubeconfigCmd(), newExportLogsCmd())
	return cmd
}

func newExportLogsCmd() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "logs [output-dir]",
		Short: "Export cluster logs and diagnostics to a directory",
		Long: "Collects each node's docker log, pod logs, k0s status and, while the\n" +
			"API server still answers, the cluster's nodes, pods and events. Writes\n" +
			"them to [output-dir], or to a temp directory whose path is printed.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var dir string
			if len(args) == 1 {
				dir = args[0]
			}
			dir, err := cluster.NewProvider().ExportLogs(name, dir)
			if err != nil {
				return err
			}
			// The path is the command's output; progress goes to stderr.
			fmt.Fprintln(cmd.OutOrStdout(), dir)
			return nil
		},
	}
	cmd.Flags().StringVarP(&name, "name", "n", "", "cluster name (default \"k0sind\")")
	return cmd
}

func newExportKubeconfigCmd() *cobra.Command {
	var name, kubeconfigPath string
	cmd := &cobra.Command{
		Use:   "kubeconfig",
		Short: "Export a cluster's kubeconfig into ~/.kube/config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cluster.NewProvider().ExportKubeconfig(name, kubeconfigPath)
		},
	}
	cmd.Flags().StringVarP(&name, "name", "n", "", "cluster name (default \"k0sind\")")
	cmd.Flags().StringVar(&kubeconfigPath, "kubeconfig", "", "sets kubeconfig path instead of $KUBECONFIG or $HOME/.kube/config")
	return cmd
}
