// Package version holds the k0sind build version.
package version

// Version is the current k0sind release. It defaults to the in-development
// version and is overridden at build time via -ldflags by GoReleaser / Makefile.
var Version = "0.1.0"
