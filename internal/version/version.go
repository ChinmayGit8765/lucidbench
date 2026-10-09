// Package version holds the build version, set via ldflags:
//
//	-ldflags "-X github.com/ChinmayGit8765/lucidbench/internal/version.Version=v0.1.0"
package version

// Version is the Lucidbench version string.
var Version = "dev"

// Repo is the GitHub repository (owner/name) this build's releases are
// published from. The update check reads its releases unless the config's
// update_repo names another. A fork sets it with
//
//	-ldflags "-X github.com/ChinmayGit8765/lucidbench/internal/version.Repo=owner/name"
var Repo = "ChinmayGit8765/lucidbench"
