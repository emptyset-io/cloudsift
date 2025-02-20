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
		Short: "List available AWS accounts",
		Long: `List all AWS accounts that can be scanned.
If no organization role is provided, only shows the current account.
If an organization role is provided, shows all accounts in the organization.`,
		Example: `  # List current account
  cloudsift list accounts

  # List all accounts in organization
  cloudsift list accounts --organization-role OrganizationAccessRole`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAccounts(cmd)
		},
	}

	cmd.Flags().String("organization-role", "", "Role to assume for listing organization accounts")
	return cmd
}

func runAccounts(cmd *cobra.Command) error {
	organizationRole, _ := cmd.Flags().GetString("organization-role")
	accounts, err := aws.ListAccounts(organizationRole)
	if err != nil {
		return fmt.Errorf("failed to list accounts: %w", err)
	}

	if len(accounts) == 0 {
		fmt.Println("No accounts found")
		return nil
	}

	fmt.Println("Available accounts:")
	for _, account := range accounts {
		fmt.Printf("  %s - %s\n", account.ID, account.Name)
	}

	return nil
}
