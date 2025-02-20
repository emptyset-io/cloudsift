package aws

import (
	"fmt"
	"strings"

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
	// If organization role provided, try to list organization accounts
	if organizationRole != "" {
		orgAccounts, err := tryListOrganizationAccounts(organizationRole)
		if err != nil {
			return nil, fmt.Errorf("failed to list organization accounts with role %s: %w", organizationRole, err)
		}
		return orgAccounts, nil
	}

	// If no organization role, fall back to current account
	return listCurrentAccount()
}

// tryListOrganizationAccounts attempts to list all accounts in the organization
func tryListOrganizationAccounts(organizationRole string) ([]Account, error) {
	// Create session with organization role in us-west-2 (Organizations API requires a region)
	sess, err := GetSession(organizationRole, "us-west-2")
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
	// First get current account ID
	sess, err := GetSession("")
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
	orgSess, err := GetSession("", "us-west-2") // Organizations API requires a region
	if err != nil {
		return nil, err
	}

	orgSvc := organizations.New(orgSess)
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

	// If we can't get the name from Organizations API, try to get it from account alias
	iamSvc := sts.New(sess)
	aliasResult, err := iamSvc.GetCallerIdentity(&sts.GetCallerIdentityInput{})
	if err == nil && aliasResult.Arn != nil {
		// Extract account alias from ARN if available
		arn := aws.StringValue(aliasResult.Arn)
		if arnParts := strings.Split(arn, ":"); len(arnParts) >= 6 {
			if userParts := strings.Split(arnParts[5], "/"); len(userParts) >= 2 {
				return []Account{
					{
						ID:   accountID,
						Name: userParts[1], // Use the IAM user/role name as a fallback
					},
				}, nil
			}
		}
	}

	// If all else fails, use a generic name based on the account type
	if strings.Contains(aws.StringValue(identity.Arn), ":root") {
		return []Account{
			{
				ID:   accountID,
				Name: "Root Account",
			},
		}, nil
	}

	return []Account{
		{
			ID:   accountID,
			Name: "Member Account",
		},
	}, nil
}
