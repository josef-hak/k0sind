// Command k0sind runs k0s clusters in Docker from kind-compatible config files.
package main

import (
	"fmt"
	"os"

	"github.com/k0sproject/k0sind/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
