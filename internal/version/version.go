// Package version holds the build version of the Mesh binaries. The default is
// overridden at release time via the linker:
//
//	go build -ldflags "-X github.com/AyushPramanik/mesh/internal/version.Version=v0.1.0"
//
// scripts/release.sh does this for every cross-compiled artifact.
package version

// Version is the build version. "dev" for unreleased local builds.
var Version = "dev"
