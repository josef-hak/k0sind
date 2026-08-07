// Package cmd wires the k0sind CLI on top of the cluster provider.
package cmd

import (
	"github.com/spf13/cobra"
)

// NewRootCmd builds the root `k0sind` command with all subcommands attached.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "k0sind",
		Short:         "k0sind runs k0s clusters in Docker from kind-compatible configs",
		Long:          "k0sind is like kind, but each node runs k0s instead of kubeadm-based Kubernetes.\nIt accepts kind (kind.x-k8s.io/v1alpha4) config files.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(
		newCreateCmd(),
		newDeleteCmd(),
		newGetCmd(),
		newExportCmd(),
		newVersionCmd(),
	)
	return root
}

// Execute runs the root command.
func Execute() error {
	return NewRootCmd().Execute()
}
