package aws

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/organizations"
	"github.com/aws/aws-sdk-go/service/sts"

	"cloudsift/internal/logging"
)

const (
	// Organizations API requires a specific region
	organizationsRegion = "us-west-2"
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
	sess, err := GetSessionChain("", "", "", organizationsRegion)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	return ListCurrentAccount(sess)
}

// tryListOrganizationAccounts attempts to list all accounts in the organization
func tryListOrganizationAccounts(organizationRole string) ([]Account, error) {
	sess, err := GetSessionChain(organizationRole, "", "", organizationsRegion)
	if err != nil {
		return nil, err
	}
	return ListAccountsWithSession(sess)
}

// ListAccountsWithSession lists accounts using an existing session
func ListAccountsWithSession(sess *session.Session) ([]Account, error) {
	svc := organizations.New(sess)
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
		return nil, fmt.Errorf("failed to list organization accounts: %w", err)
	}

	logging.Info("Successfully listed organization accounts", map[string]interface{}{
		"account_count": len(accounts),
	})
	return accounts, nil
}

// getCurrentAccountID gets the current account ID using STS
func getCurrentAccountID(sess *session.Session) (string, error) {
	stsSvc := sts.New(sess)
	identity, err := stsSvc.GetCallerIdentity(&sts.GetCallerIdentityInput{})
	if err != nil {
		return "", fmt.Errorf("failed to get caller identity: %w", err)
	}
	return aws.StringValue(identity.Account), nil
}

// getAccountName attempts to get the account name from Organizations API
func getAccountName(sess *session.Session, accountID string) (string, error) {
	orgSvc := organizations.New(sess)
	describeResult, err := orgSvc.DescribeAccount(&organizations.DescribeAccountInput{
		AccountId: aws.String(accountID),
	})

	if err != nil {
		return "", err
	}

	if describeResult.Account != nil && describeResult.Account.Name != nil {
		return aws.StringValue(describeResult.Account.Name), nil
	}

	return "", fmt.Errorf("account name not available")
}

// ListCurrentAccount gets the current account information using an existing session
func ListCurrentAccount(sess *session.Session) ([]Account, error) {
	accountID, err := getCurrentAccountID(sess)
	if err != nil {
		return nil, err
	}

	// Try to get account name from Organizations API
	accountName, err := getAccountName(sess, accountID)
	if err != nil {
		logging.Warn("Could not get account name from Organizations API, using account ID as name", map[string]interface{}{
			"account_id": accountID,
			"error":     err,
		})
		accountName = accountID
	}

	return []Account{
		{
			ID:   accountID,
			Name: accountName,
		},
	}, nil
}
