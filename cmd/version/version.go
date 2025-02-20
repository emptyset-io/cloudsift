package version

import (
	"fmt"

	"cloudsift/internal/version"

	"github.com/spf13/cobra"
)

// NewVersionCmd creates and returns the version command
func NewVersionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version information",
		Long: `Print the version information for CloudSift CLI.
This includes the version number, git commit hash, build time, and Go version.`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("CloudSift %s\n", version.String())
		},
	}

	return cmd
}
