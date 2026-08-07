package cmd

import (
	"github.com/k0sproject/k0sind/pkg/cluster"
	"github.com/spf13/cobra"
)

func newExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export cluster artifacts",
	}
	cmd.AddCommand(newExportKubeconfigCmd())
	return cmd
}

func newExportKubeconfigCmd() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "kubeconfig",
		Short: "Export a cluster's kubeconfig into ~/.kube/config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cluster.NewProvider().ExportKubeconfig(name)
		},
	}
	cmd.Flags().StringVarP(&name, "name", "n", "", "cluster name (default \"k0sind\")")
	return cmd
}
