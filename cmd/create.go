package cmd

import (
	"time"

	"github.com/k0sproject/k0sind/pkg/cluster"
	"github.com/spf13/cobra"
)

func newCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a cluster",
	}
	cmd.AddCommand(newCreateClusterCmd())
	return cmd
}

func newCreateClusterCmd() *cobra.Command {
	var (
		name      string
		config    string
		k0sConfig string
		image     string
		wait      time.Duration
	)
	cmd := &cobra.Command{
		Use:   "cluster",
		Short: "Create a k0s cluster in Docker",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cluster.NewProvider().Create(cluster.CreateOptions{
				Name:          name,
				ConfigPath:    config,
				K0sConfigPath: k0sConfig,
				Image:         image,
				Wait:          wait,
			})
		},
	}
	cmd.Flags().StringVarP(&name, "name", "n", "", "cluster name (default \"k0sind\")")
	cmd.Flags().StringVar(&config, "config", "", "path to a kind-compatible config file")
	cmd.Flags().StringVar(&k0sConfig, "k0s-config", "", "path to a k0s configuration file (k0s.yaml)")
	cmd.Flags().StringVar(&image, "image", "", "k0s node image to use (overrides the default)")
	cmd.Flags().DurationVar(&wait, "wait", 0, "wait for all nodes to be Ready (e.g. 120s); 0 disables waiting")
	return cmd
}
