package cmd

import (
	"fmt"

	"github.com/k0sproject/k0sind/pkg/cluster"
	"github.com/spf13/cobra"
)

func newGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Display clusters, nodes, or kubeconfig",
	}
	cmd.AddCommand(newGetClustersCmd(), newGetNodesCmd(), newGetKubeconfigCmd())
	return cmd
}

func newGetClustersCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clusters",
		Short: "List k0sind clusters",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			names, err := cluster.NewProvider().List()
			if err != nil {
				return err
			}
			if len(names) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No k0sind clusters found.")
				return nil
			}
			for _, n := range names {
				fmt.Fprintln(cmd.OutOrStdout(), n)
			}
			return nil
		},
	}
}

func newGetKubeconfigCmd() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "kubeconfig",
		Short: "Print the kubeconfig for a cluster",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := cluster.NewProvider().GetKubeconfig(name)
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(data)
			return err
		},
	}
	cmd.Flags().StringVarP(&name, "name", "n", "", "cluster name (default \"k0sind\")")
	return cmd
}

func newGetNodesCmd() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "nodes",
		Short: "List the nodes of a cluster",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			nodes, err := cluster.NewProvider().ListNodes(name)
			if err != nil {
				return err
			}
			for _, n := range nodes {
				fmt.Fprintln(cmd.OutOrStdout(), n)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&name, "name", "n", "", "cluster name (default \"k0sind\")")
	return cmd
}
