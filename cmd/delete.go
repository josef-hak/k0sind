package cmd

import (
	"github.com/k0sproject/k0sind/pkg/cluster"
	"github.com/spf13/cobra"
)

func newDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a cluster",
	}
	cmd.AddCommand(newDeleteClusterCmd())
	return cmd
}

func newDeleteClusterCmd() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "cluster",
		Short: "Delete a k0s cluster and its kubeconfig context",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cluster.NewProvider().Delete(name)
		},
	}
	cmd.Flags().StringVarP(&name, "name", "n", "", "cluster name (default \"k0sind\")")
	return cmd
}
