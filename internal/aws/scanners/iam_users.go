package scanners

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	awslib "cloudsift/internal/aws"
	"cloudsift/internal/aws/ratelimit"
	"cloudsift/internal/aws/utils"
	"cloudsift/internal/logging"
	"cloudsift/internal/worker"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/iam"
)

var (
	// Track which accounts we've already scanned
	scannedAccounts = struct {
		sync.Mutex
		accounts map[string]bool
	}{
		accounts: make(map[string]bool),
	}
)

// IAMUserScanner scans for unused IAM users
type IAMUserScanner struct {
	client  *iam.IAM
	limiter *ratelimit.ServiceLimiter
}

func init() {
	if err := awslib.DefaultRegistry.RegisterScanner(&IAMUserScanner{}); err != nil {
		panic(fmt.Sprintf("Failed to register IAM User scanner: %v", err))
	}
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
func (s *IAMUserScanner) getLastConsoleLogin(ctx context.Context, userName string) (*time.Time, error) {
	err := s.limiter.Execute(ctx, "GetLoginProfile", func() error {
		_, err := s.client.GetLoginProfileWithContext(ctx, &iam.GetLoginProfileInput{
			UserName: aws.String(userName),
		})
		return err
	})

	if err != nil {
		if aerr, ok := err.(awserr.Error); ok && aerr.Code() == iam.ErrCodeNoSuchEntityException {
			return nil, nil // User has no login profile
		}
		return nil, err
	}

	return aws.Time(time.Now()), nil
}

// getLastKeyUsage gets the last time any access key was used for a user
func (s *IAMUserScanner) getLastKeyUsage(ctx context.Context, userName string) (*time.Time, error) {
	var accessKeys []*iam.AccessKeyMetadata
	err := s.limiter.Execute(ctx, "ListAccessKeys", func() error {
		result, err := s.client.ListAccessKeysWithContext(ctx, &iam.ListAccessKeysInput{
			UserName: aws.String(userName),
		})
		if err != nil {
			return fmt.Errorf("failed to list access keys: %w", err)
		}
		accessKeys = result.AccessKeyMetadata
		return nil
	})
	if err != nil {
		return nil, err
	}

	var latestUsage *time.Time
	for _, key := range accessKeys {
		var keyUsage *iam.GetAccessKeyLastUsedOutput
		err := s.limiter.Execute(ctx, "GetAccessKeyLastUsed", func() error {
			var err error
			keyUsage, err = s.client.GetAccessKeyLastUsedWithContext(ctx, &iam.GetAccessKeyLastUsedInput{
				AccessKeyId: key.AccessKeyId,
			})
			return err
		})
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

	// Check for users who have never logged in
	if lastLoginTime == nil {
		reasons = append(reasons, fmt.Sprintf("User has never logged in to the console in the last %d days.", opts.DaysUnused))
	} else {
		// Check if last login was beyond the threshold
		cutoffTime := time.Now().AddDate(0, 0, -opts.DaysUnused)
		if lastLoginTime.Before(cutoffTime) {
			reasons = append(reasons, fmt.Sprintf("User has not logged in to the console in the last %d days. Last login: %s", opts.DaysUnused, lastLoginTime.Format(time.RFC3339)))
		}
	}

	// Check for users who have never used their access keys
	if keyLastUsedTime == nil {
		reasons = append(reasons, fmt.Sprintf("User has never used their access keys in the last %d days.", opts.DaysUnused))
	} else {
		// Check if last key usage was beyond the threshold
		cutoffTime := time.Now().AddDate(0, 0, -opts.DaysUnused)
		if keyLastUsedTime.Before(cutoffTime) {
			reasons = append(reasons, fmt.Sprintf("User has not used their access keys in the last %d days. Last key usage: %s", opts.DaysUnused, keyLastUsedTime.Format(time.RFC3339)))
		}
	}

	return reasons
}

// Scan implements Scanner interface
func (s *IAMUserScanner) Scan(opts awslib.ScanOptions) (awslib.ScanResults, error) {
	// Create base session with region
	sess, err := awslib.GetSession(opts.Role, opts.Region)
	if err != nil {
		logging.Error("Failed to create AWS session", err, map[string]interface{}{
			"region": opts.Region,
			"role":   opts.Role,
		})
		return nil, fmt.Errorf("failed to create AWS session: %w", err)
	}

	// Get current account ID
	accountID, err := utils.GetAccountID(sess)
	if err != nil {
		logging.Error("Failed to get caller identity", err, nil)
		return nil, fmt.Errorf("failed to get caller identity: %w", err)
	}

	// Check if we've already scanned this account
	scannedAccounts.Lock()
	if scannedAccounts.accounts[accountID] {
		scannedAccounts.Unlock()
		logging.Debug("Skipping IAM users scan - account already scanned", map[string]interface{}{
			"account_id": accountID,
		})
		return nil, nil
	}
	scannedAccounts.accounts[accountID] = true
	scannedAccounts.Unlock()

	// Initialize scanner
	s.client = iam.New(sess)
	s.limiter = ratelimit.GetServiceLimiter("iam")
	ctx := context.Background()

	// Get all IAM users
	var users []*iam.User
	var marker *string

	for {
		err := s.limiter.Execute(ctx, "ListUsers", func() error {
			output, err := s.client.ListUsersWithContext(ctx, &iam.ListUsersInput{
				Marker: marker,
			})
			if err != nil {
				return err
			}
			users = append(users, output.Users...)
			marker = output.Marker
			return nil
		})
		if err != nil {
			logging.Error("Failed to list IAM users", err, nil)
			return nil, fmt.Errorf("failed to list IAM users: %w", err)
		}
		if marker == nil {
			break
		}
	}

	// Process users concurrently using worker pool
	pool := worker.NewPool(10)
	var results awslib.ScanResults
	var resultsMu sync.Mutex

	tasks := make([]worker.Task, len(users))
	for i, user := range users {
		userName := aws.StringValue(user.UserName)
		userARN := aws.StringValue(user.Arn)
		userData := user // Capture for closure

		tasks[i] = func(ctx context.Context) error {
			logging.Debug("Analyzing IAM user", map[string]interface{}{
				"user_name": userName,
				"user_arn":  userARN,
			})

			// Get last login time and key usage time
			lastLoginTime, err := s.getLastConsoleLogin(ctx, userName)
			if err != nil {
				logging.Error("Failed to get last console login", err, map[string]interface{}{
					"user_name": userName,
				})
				return nil
			}

			keyLastUsedTime, err := s.getLastKeyUsage(ctx, userName)
			if err != nil {
				logging.Error("Failed to get key usage time", err, map[string]interface{}{
					"user_name": userName,
				})
				return nil
			}

			// Determine unused reasons
			reasons := s.determineUnusedReasons(lastLoginTime, keyLastUsedTime, opts)

			if len(reasons) > 0 {
				// Create details map with IAM user-specific fields
				details := map[string]interface{}{
					"AccountId":        accountID,
					"Region":           "global",
					"Path":            aws.StringValue(userData.Path),
					"CreateDate":      aws.TimeValue(userData.CreateDate).Format(time.RFC3339),
					"LastLoginTime":   formatTimeOrNever(lastLoginTime),
					"LastKeyUsedTime": formatTimeOrNever(keyLastUsedTime),
				}

				// Add permissions boundary if present
				if userData.PermissionsBoundary != nil {
					details["PermissionsBoundary"] = aws.StringValue(userData.PermissionsBoundary.PermissionsBoundaryArn)
				}

				result := awslib.ScanResult{
					ResourceType: s.Label(),
					ResourceName: userName,
					ResourceID:   userARN,
					Reason:      strings.Join(reasons, "\n"),
					Details:     details,
				}

				resultsMu.Lock()
				results = append(results, result)
				resultsMu.Unlock()
			}
			return nil
		}
	}

	pool.ExecuteTasks(tasks)

	logging.Debug("Completed IAM users scan", map[string]interface{}{
		"total_users": len(users),
		"account_id": accountID,
	})

	return results, nil
}

// formatTimeOrNever formats a time pointer as RFC3339 or returns "Never" if nil
func formatTimeOrNever(t *time.Time) string {
	if t == nil {
		return "Never"
	}
	return t.Format(time.RFC3339)
}
