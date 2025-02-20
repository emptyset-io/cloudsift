package cmd

import (
	"github.com/spf13/cobra"
	"cloudsift/cmd/list"
)

// NewListCmd creates and returns the list command
func NewListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List AWS resources",
		Long: `List various AWS resources and configurations.
Currently supports listing:
  - AWS accounts in an organization or current account
  - Available AWS credential profiles`,
	}

	// Add subcommands
	cmd.AddCommand(list.NewAccountsCmd())
	cmd.AddCommand(list.NewProfilesCmd())

	return cmd
}
