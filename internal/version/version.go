// Package version holds the k0sind build version.
package version

// Version is the current k0sind release. It defaults to the in-development
// version and is overridden at build time via -ldflags by GoReleaser / Makefile.
// The literal is kept in sync by release-please; do not edit it by hand.
var Version = "1.3.0" // x-release-please-version
