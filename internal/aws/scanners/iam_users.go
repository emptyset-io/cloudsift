package scanners

import (
	"fmt"
	"strings"
	"time"

	awslib "cloudsift/internal/aws"
	"cloudsift/internal/aws/utils"
	"cloudsift/internal/logging"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/iam"
)

// IAMUserScanner scans for unused IAM users
type IAMUserScanner struct{}

func init() {
	awslib.DefaultRegistry.RegisterScanner(&IAMUserScanner{})
}

// Name implements Scanner interface
func (s *IAMUserScanner) Name() string {
	return "iam-users"
}

// ArgumentName implements Scanner interface
func (s *IAMUserScanner) ArgumentName() string {
	return "iam-users"
}

// Label implements Scanner interface
func (s *IAMUserScanner) Label() string {
	return "IAM Users"
}

// getLastConsoleLogin gets the last console login time for a user
func (s *IAMUserScanner) getLastConsoleLogin(iamClient *iam.IAM, userName string) (*time.Time, error) {
	input := &iam.GetLoginProfileInput{
		UserName: aws.String(userName),
	}

	_, err := iamClient.GetLoginProfile(input)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok && aerr.Code() == iam.ErrCodeNoSuchEntityException {
			return nil, nil // User has no login profile
		}
		return nil, err
	}

	return aws.Time(time.Now()), nil
}

// getLastKeyUsage gets the last time any access key was used for a user
func (s *IAMUserScanner) getLastKeyUsage(iamClient *iam.IAM, userName string) (*time.Time, error) {
	input := &iam.ListAccessKeysInput{
		UserName: aws.String(userName),
	}

	result, err := iamClient.ListAccessKeys(input)
	if err != nil {
		return nil, fmt.Errorf("failed to list access keys: %w", err)
	}

	var latestUsage *time.Time
	for _, key := range result.AccessKeyMetadata {
		keyInput := &iam.GetAccessKeyLastUsedInput{
			AccessKeyId: key.AccessKeyId,
		}

		keyUsage, err := iamClient.GetAccessKeyLastUsed(keyInput)
		if err != nil {
			logging.Error("Failed to get access key last used", err, map[string]interface{}{
				"user_name":     userName,
				"access_key_id": aws.StringValue(key.AccessKeyId),
			})
			continue
		}

		if keyUsage.AccessKeyLastUsed != nil && keyUsage.AccessKeyLastUsed.LastUsedDate != nil {
			lastUsed := aws.TimeValue(keyUsage.AccessKeyLastUsed.LastUsedDate)
			if latestUsage == nil || lastUsed.After(*latestUsage) {
				latestUsage = &lastUsed
			}
		}
	}

	return latestUsage, nil
}

// determineUnusedReasons determines why a user is considered unused
func (s *IAMUserScanner) determineUnusedReasons(lastLoginTime, keyLastUsedTime *time.Time, opts awslib.ScanOptions) []string {
	var reasons []string
	now := time.Now()

	// Check if user has ever logged in
	if lastLoginTime == nil {
		reasons = append(reasons, "User has never logged in to the console.")
	} else {
		loginAge := awslib.FormatTimeDifference(now, lastLoginTime)
		reasons = append(reasons, fmt.Sprintf("User has not logged in to the console in %s.", loginAge))
	}

	// Check access key usage
	if keyLastUsedTime == nil {
		reasons = append(reasons, "User has never used access keys.")
	} else {
		keyAge := awslib.FormatTimeDifference(now, keyLastUsedTime)
		reasons = append(reasons, fmt.Sprintf("User has not used access keys in %s.", keyAge))
	}

	return reasons
}

// Scan implements Scanner interface
func (s *IAMUserScanner) Scan(opts awslib.ScanOptions) (awslib.ScanResults, error) {
	// Get regional session
	sess, err := awslib.GetSessionInRegion(opts.Session, opts.Region)
	if err != nil {
		logging.Error("Failed to create regional session", err, map[string]interface{}{
			"region": opts.Region,
		})
		return nil, fmt.Errorf("failed to create regional session: %w", err)
	}

	// Get current account ID
	accountID, err := utils.GetAccountID(sess)
	if err != nil {
		logging.Error("Failed to get caller identity", err, nil)
		return nil, fmt.Errorf("failed to get caller identity: %w", err)
	}

	// Create IAM client
	iamClient := iam.New(sess)

	// Get all IAM users
	var users []*iam.User
	err = iamClient.ListUsersPages(&iam.ListUsersInput{},
		func(page *iam.ListUsersOutput, lastPage bool) bool {
			users = append(users, page.Users...)
			return !lastPage
		})
	if err != nil {
		logging.Error("Failed to list IAM users", err, nil)
		return nil, fmt.Errorf("failed to list IAM users: %w", err)
	}

	var results awslib.ScanResults
	for _, user := range users {
		userName := aws.StringValue(user.UserName)
		userARN := aws.StringValue(user.Arn)

		logging.Debug("Analyzing IAM user", map[string]interface{}{
			"user_name": userName,
			"user_arn":  userARN,
		})

		// Get last login time and key usage time
		lastLoginTime, err := s.getLastConsoleLogin(iamClient, userName)
		if err != nil {
			logging.Error("Failed to get last console login", err, map[string]interface{}{
				"user_name": userName,
			})
			continue
		}

		keyLastUsedTime, err := s.getLastKeyUsage(iamClient, userName)
		if err != nil {
			logging.Error("Failed to get key usage time", err, map[string]interface{}{
				"user_name": userName,
			})
			continue
		}

		// Determine unused reasons
		reasons := s.determineUnusedReasons(lastLoginTime, keyLastUsedTime, opts)

		if len(reasons) > 0 {
			// Create details map with IAM user-specific fields
			details := map[string]interface{}{
				"AccountId":       accountID,
				"Region":          opts.Region,
				"Path":            aws.StringValue(user.Path),
				"CreateDate":      aws.TimeValue(user.CreateDate).Format(time.RFC3339),
				"LastLoginTime":   formatTimeOrNever(lastLoginTime),
				"LastKeyUsedTime": formatTimeOrNever(keyLastUsedTime),
			}

			// Add permissions boundary if present
			if user.PermissionsBoundary != nil {
				details["PermissionsBoundary"] = aws.StringValue(user.PermissionsBoundary.PermissionsBoundaryArn)
			}

			result := awslib.ScanResult{
				ResourceType: s.Label(),
				ResourceName: userName,
				ResourceID:   userARN,
				Reason:       strings.Join(reasons, "\n"),
				Details:      details,
			}

			results = append(results, result)
		}
	}

	return results, nil
}

// formatTimeOrNever formats a time pointer as RFC3339 or returns "Never" if nil
func formatTimeOrNever(t *time.Time) string {
	if t == nil {
		return "Never"
	}
	return t.Format(time.RFC3339)
}
