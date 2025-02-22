package aws

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/organizations"
	"github.com/aws/aws-sdk-go/service/sts"

	"cloudsift/internal/logging"
)

// Account represents an AWS account
type Account struct {
	ID   string
	Name string
}

// ListAccounts attempts to list all accounts in the organization, falling back to current account if not in an org
// If organizationRole is provided, assumes that role before listing accounts
func ListAccounts(organizationRole string) ([]Account, error) {
	logging.Debug("Listing AWS accounts", map[string]interface{}{
		"organization_role": organizationRole,
	})

	// If organization role provided, try to list organization accounts
	if organizationRole != "" {
		orgAccounts, err := tryListOrganizationAccounts(organizationRole)
		if err != nil {
			logging.Error("Failed to list organization accounts", err, map[string]interface{}{
				"organization_role": organizationRole,
			})
			return nil, fmt.Errorf("failed to list organization accounts with role %s: %w", organizationRole, err)
		}
		logging.Info("Successfully listed organization accounts", map[string]interface{}{
			"account_count": len(orgAccounts),
		})
		return orgAccounts, nil
	}

	logging.Info("No organization role provided, falling back to current account")
	return listCurrentAccount()
}

// tryListOrganizationAccounts attempts to list all accounts in the organization
func tryListOrganizationAccounts(organizationRole string) ([]Account, error) {
	// Create session chain with organization role in us-west-2 (Organizations API requires a region)
	sess, err := GetSessionChain(organizationRole, "", "us-west-2")
	if err != nil {
		return nil, err
	}

	svc := organizations.New(sess)
	input := &organizations.ListAccountsInput{}

	var accounts []Account
	err = svc.ListAccountsPages(input, func(page *organizations.ListAccountsOutput, lastPage bool) bool {
		for _, account := range page.Accounts {
			accounts = append(accounts, Account{
				ID:   aws.StringValue(account.Id),
				Name: aws.StringValue(account.Name),
			})
		}
		return !lastPage
	})

	if err != nil {
		return nil, fmt.Errorf("failed to list organization accounts: %w", err)
	}

	return accounts, nil
}

// listCurrentAccount gets the current account information
func listCurrentAccount() ([]Account, error) {
	// Create base session without any roles
	sess, err := GetSessionChain("", "", "us-west-2") // Organizations API requires a region
	if err != nil {
		return nil, err
	}

	stsSvc := sts.New(sess)
	identity, err := stsSvc.GetCallerIdentity(&sts.GetCallerIdentityInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to get caller identity: %w", err)
	}

	accountID := aws.StringValue(identity.Account)

	// Try to get account name from Organizations API
	orgSvc := organizations.New(sess)
	describeResult, err := orgSvc.DescribeAccount(&organizations.DescribeAccountInput{
		AccountId: aws.String(accountID),
	})

	// If we can get the account name from Organizations API, use it
	if err == nil && describeResult.Account != nil && describeResult.Account.Name != nil {
		return []Account{
			{
				ID:   accountID,
				Name: aws.StringValue(describeResult.Account.Name),
			},
		}, nil
	}

	logging.Warn("Could not get account name from Organizations API, using account ID as name", map[string]interface{}{
		"account_id": accountID,
	})

	// If we can't get the name, just use the account ID as the name
	return []Account{
		{
			ID:   accountID,
			Name: accountID,
		},
	}, nil
}
