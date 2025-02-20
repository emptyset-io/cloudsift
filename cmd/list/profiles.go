package list

import (
	"fmt"

	"cloudsift/internal/aws"
	"github.com/spf13/cobra"
)

// NewProfilesCmd creates and returns the profiles command
func NewProfilesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profiles",
		Short: "List available AWS profiles",
		Long: `List all available AWS credential profiles from the system.
These profiles are read from the AWS credentials and config files.`,
		Example: `  # List all available AWS profiles
  cloudsift list profiles`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProfiles()
		},
	}

	return cmd
}

func runProfiles() error {
	profiles, err := aws.ListProfiles()
	if err != nil {
		return fmt.Errorf("failed to list profiles: %w", err)
	}

	// Print profiles
	for _, profile := range profiles {
		fmt.Println(profile)
	}

	return nil
}
