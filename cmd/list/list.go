package list

import (
	_ "cloudsift/internal/aws/scanners" // Import for side effects (scanner registration)
	"github.com/spf13/cobra"
)

// NewListCmd creates the list command
func NewListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List various AWS resources",
		Long: `List various AWS resources and configurations.
Currently supports listing:
  - AWS accounts in an organization or current account
  - Available AWS credential profiles
  - Available resource scanners`,
	}

	// Add subcommands
	cmd.AddCommand(NewAccountsCmd())
	cmd.AddCommand(NewProfilesCmd())
	cmd.AddCommand(NewScannersCmd())

	return cmd
}
