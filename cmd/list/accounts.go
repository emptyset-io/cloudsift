package list

import (
	"fmt"

	"cloudsift/internal/aws"
	"github.com/spf13/cobra"
)

// NewAccountsCmd creates and returns the accounts command
func NewAccountsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "accounts",
		Short: "List AWS accounts",
		Long: `List AWS accounts based on the current configuration.

If the current credentials have organization access, lists all accounts in the organization.
Otherwise, lists the current account information.

Use --profile to specify an AWS profile to use.
Use --organization-role to specify a role to assume for organization-wide operations.`,
		Example: `  # List accounts using default profile
  cloudsift list accounts

  # List accounts using specific profile
  cloudsift list accounts --profile dev

  # List accounts using organization role
  cloudsift list accounts --organization-role arn:aws:iam::123456789012:role/OrganizationRole`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAccounts()
		},
	}

	return cmd
}

func runAccounts() error {
	accounts, err := aws.ListAccounts()
	if err != nil {
		return fmt.Errorf("failed to list accounts: %w", err)
	}

	for _, account := range accounts {
		fmt.Printf("%s - %s\n", account.ID, account.Name)
	}

	return nil
}
