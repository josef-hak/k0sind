package cmd

import (
	"github.com/k0sproject/k0sind/pkg/cluster"
	"github.com/spf13/cobra"
)

func newLoadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "load",
		Short: "Load images into cluster nodes",
	}
	cmd.AddCommand(newLoadDockerImageCmd())
	return cmd
}

func newLoadDockerImageCmd() *cobra.Command {
	var (
		name  string
		nodes []string
	)
	cmd := &cobra.Command{
		Use:   "docker-image IMAGE [IMAGE...]",
		Short: "Load local docker images into the nodes of a cluster",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return cluster.NewProvider().LoadImages(cluster.LoadOptions{
				Name:   name,
				Images: args,
				Nodes:  nodes,
			})
		},
	}
	cmd.Flags().StringVarP(&name, "name", "n", "", "cluster name (default \"k0sind\")")
	cmd.Flags().StringSliceVar(&nodes, "nodes", nil, "comma separated list of nodes to load images into (default all)")
	return cmd
}
