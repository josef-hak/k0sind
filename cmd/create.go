package cmd

import (
	"time"

	"github.com/k0sproject/k0sind/pkg/cluster"
	"github.com/spf13/cobra"
)

func newCreateCmd() *cobra.Command {
	var (
		name   string
		config string
		image  string
		wait   time.Duration
	)
	cmd := &cobra.Command{
		Use:   "create cluster",
		Short: "Create a k0s cluster in Docker",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return cluster.NewProvider().Create(cluster.CreateOptions{
				Name:       name,
				ConfigPath: config,
				Image:      image,
				Wait:       wait,
			})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "cluster name (default \"k0sind\")")
	cmd.Flags().StringVar(&config, "config", "", "path to a kind-compatible config file")
	cmd.Flags().StringVar(&image, "image", "", "k0s node image to use (overrides the default)")
	cmd.Flags().DurationVar(&wait, "wait", 0, "wait for all nodes to be Ready (e.g. 120s); 0 disables waiting")
	return cmd
}
