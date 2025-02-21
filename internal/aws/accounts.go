package aws

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/organizations"
	"github.com/aws/aws-sdk-go/service/sts"
)

// Account represents an AWS account
type Account struct {
	ID   string
	Name string
}

// ListAccounts attempts to list all accounts in the organization, falling back to current account if not in an org
// If organizationRole is provided, assumes that role before listing accounts
func ListAccounts(organizationRole string) ([]Account, error) {
	// First get current account ID
	sess, err := GetSession("", "")
	if err != nil {
		return nil, err
	}

	stsSvc := sts.New(sess)
	identity, err := stsSvc.GetCallerIdentity(&sts.GetCallerIdentityInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to get caller identity: %w", err)
	}

	accountID := aws.StringValue(identity.Account)

	// Create session for Organizations API in us-west-2 (Organizations API requires a region)
	orgSess, err := GetSession(organizationRole, "us-west-2")
	if err != nil {
		return nil, err
	}

	orgSvc := organizations.New(orgSess)

	// If organization role provided, try to list all accounts
	if organizationRole != "" {
		accounts, err := listOrganizationAccounts(orgSvc)
		if err != nil {
			return nil, fmt.Errorf("failed to list organization accounts: %w", err)
		}
		return accounts, nil
	}

	// If no organization role, just get the current account name from Organizations API
	account, err := getAccountFromOrganization(orgSvc, accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to get account from organization: %w", err)
	}
	return []Account{account}, nil
}

// listOrganizationAccounts lists all accounts in the organization
func listOrganizationAccounts(svc *organizations.Organizations) ([]Account, error) {
	input := &organizations.ListAccountsInput{}
	var accounts []Account

	err := svc.ListAccountsPages(input, func(page *organizations.ListAccountsOutput, lastPage bool) bool {
		for _, account := range page.Accounts {
			accounts = append(accounts, Account{
				ID:   aws.StringValue(account.Id),
				Name: aws.StringValue(account.Name),
			})
		}
		return !lastPage
	})

	if err != nil {
		return nil, fmt.Errorf("failed to list accounts: %w", err)
	}

	return accounts, nil
}

// getAccountFromOrganization gets a single account's details from Organizations API
func getAccountFromOrganization(svc *organizations.Organizations, accountID string) (Account, error) {
	describeResult, err := svc.DescribeAccount(&organizations.DescribeAccountInput{
		AccountId: aws.String(accountID),
	})
	if err != nil {
		return Account{}, fmt.Errorf("failed to describe account: %w", err)
	}

	if describeResult.Account == nil || describeResult.Account.Name == nil {
		return Account{}, fmt.Errorf("no account details found")
	}

	return Account{
		ID:   accountID,
		Name: aws.StringValue(describeResult.Account.Name),
	}, nil
}
