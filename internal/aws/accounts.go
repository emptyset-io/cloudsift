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
func ListAccounts() ([]Account, error) {
	// First try to list organization accounts
	orgAccounts, err := tryListOrganizationAccounts()
	if err == nil {
		return orgAccounts, nil
	}

	// If organization listing fails, fall back to current account
	return listCurrentAccount()
}

// tryListOrganizationAccounts attempts to list all accounts in the organization
func tryListOrganizationAccounts() ([]Account, error) {
	sess, err := GetSession("us-west-2") // Organizations API requires a region
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
	sess, err := GetSession() // STS works in any region
	if err != nil {
		return nil, err
	}

	svc := sts.New(sess)
	result, err := svc.GetCallerIdentity(&sts.GetCallerIdentityInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to get current account: %w", err)
	}

	return []Account{{
		ID:   aws.StringValue(result.Account),
		Name: "current", // We don't have access to the account name when not in an organization
	}}, nil
}
