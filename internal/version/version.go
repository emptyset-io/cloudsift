package version

import "fmt"

var (
	// Version is the current version of CloudSift
	Version = "0.1.1"

	// GitCommit is the git commit hash, injected at build time
	GitCommit string

	// BuildTime is the build timestamp, injected at build time
	BuildTime string

	// GoVersion is the Go runtime version, injected at build time
	GoVersion string
)

// String returns the full version string
func String() string {
	if GitCommit != "" && BuildTime != "" {
		return fmt.Sprintf("%s (commit: %s, built: %s, %s)",
			Version, GitCommit[:8], BuildTime, GoVersion)
	}
	return Version
}

// ShortString returns just the version number
func ShortString() string {
	return Version
}
