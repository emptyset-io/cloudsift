package scanners

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	awslib "cloudsift/internal/aws"
	"cloudsift/internal/aws/utils"
	"cloudsift/internal/config"
	"cloudsift/internal/logging"
	"cloudsift/internal/worker"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/iam"
)

// IAMUserScanner scans for unused IAM users
type IAMUserScanner struct{}

// userTask represents the task of processing a single IAM user
type userTask struct {
	user        *iam.User
	iamClient   *iam.IAM
	accountID   string
	region      string
	scanner     *IAMUserScanner
	opts        awslib.ScanOptions
	now         time.Time
	rateLimiter *awslib.RateLimiter
}

func init() {
	awslib.DefaultRegistry.RegisterScanner(&IAMUserScanner{})
}

// ArgumentName implements Scanner interface
func (s *IAMUserScanner) ArgumentName() string {
	return "iam-users"
}

// Label implements Scanner interface
func (s *IAMUserScanner) Label() string {
	return "IAM Users"
}

// processUser processes a single IAM user and returns a scan result if the user is unused
func (t *userTask) processUser(ctx context.Context) (*awslib.ScanResult, error) {
	userName := aws.StringValue(t.user.UserName)
	userARN := aws.StringValue(t.user.Arn)

	logging.Debug("Analyzing IAM user", map[string]interface{}{
		"user_name": userName,
		"user_arn":  userARN,
	})

	// Get last console login time with rate limiting
	if err := t.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit wait error: %w", err)
	}
	lastLoginTime, err := t.scanner.getLastConsoleLogin(t.iamClient, userName)
	if err != nil {
		if strings.Contains(err.Error(), "Throttling:") {
			logging.Info("TEMP: IAM API throttled", map[string]interface{}{
				"user_name": userName,
				"error":     err.Error(),
			})
		}
		logging.Error("Failed to get last console login", err, map[string]interface{}{
			"user_name": userName,
		})
		t.rateLimiter.OnFailure()
		return nil, err
	}
	t.rateLimiter.OnSuccess()

	// Get last key usage time with rate limiting
	if err := t.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit wait error: %w", err)
	}
	keyLastUsedTime, err := t.scanner.getLastKeyUsage(t.iamClient, userName)
	if err != nil {
		logging.Error("Failed to get key usage time", err, map[string]interface{}{
			"user_name": userName,
		})
		t.rateLimiter.OnFailure()
		return nil, err
	}
	t.rateLimiter.OnSuccess()

	// Calculate age strings
	var lastUsedTime *time.Time
	if lastLoginTime != nil && keyLastUsedTime != nil {
		if lastLoginTime.After(*keyLastUsedTime) {
			lastUsedTime = lastLoginTime
		} else {
			lastUsedTime = keyLastUsedTime
		}
	} else if lastLoginTime != nil {
		lastUsedTime = lastLoginTime
	} else if keyLastUsedTime != nil {
		lastUsedTime = keyLastUsedTime
	}

	if lastUsedTime == nil {
		lastUsedTime = t.user.CreateDate
	}

	ageString := utils.FormatTimeDifference(t.now, lastUsedTime)

	// Determine unused reasons
	reasons := t.scanner.determineUnusedReasons(lastLoginTime, keyLastUsedTime, t.opts)
	if len(reasons) > 0 {
		details := map[string]interface{}{
			"LastUsed":         ageString,
			"LastConsoleLogin": formatTimeOrNever(lastLoginTime),
			"LastKeyUsed":      formatTimeOrNever(keyLastUsedTime),
			"HasLoginProfile":  lastLoginTime != nil,
			"HasAccessKeys":    keyLastUsedTime != nil,
			"CreatedAt":        aws.TimeValue(t.user.CreateDate).Format(time.RFC3339),
		}

		return &awslib.ScanResult{
			ResourceType: t.scanner.Label(),
			ResourceName: userName,
			ResourceID:   userARN,
			Reason:       strings.Join(reasons, "\n"),
			Details:      details,
		}, nil
	}

	return nil, nil
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
		reasons = append(reasons, "User has never logged in to the console")
	} else {
		age := time.Since(*lastLoginTime)
		if age.Hours()/24 > float64(opts.DaysUnused) {
			loginAge := utils.FormatTimeDifference(now, lastLoginTime)
			reasons = append(reasons, fmt.Sprintf("User has not logged in to the console in %s", loginAge))
		}
	}

	// Check access key usage
	if keyLastUsedTime == nil {
		reasons = append(reasons, "User has never used access keys")
	} else {
		age := time.Since(*keyLastUsedTime)
		if age.Hours()/24 > float64(opts.DaysUnused) {
			keyAge := utils.FormatTimeDifference(now, keyLastUsedTime)
			reasons = append(reasons, fmt.Sprintf("User has not used access keys in %s", keyAge))
		}
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

	// Create IAM client
	iamClient := iam.New(sess)

	// Log scan start
	logging.Info("Starting IAM user scan", map[string]interface{}{
		"account_id": opts.AccountID,
		"region":     opts.Region,
	})

	// Create rate limiter for IAM API
	rateLimiterKey := fmt.Sprintf("%s-%s-iam", opts.AccountID, opts.Region)
	iamConfig := &config.RateLimitConfig{
		RequestsPerSecond: 35.0,              // IAM has lower rate limits
		MaxRetries:        10,                // Keep retrying on throttling
		BaseDelay:         1 * time.Second,   // Start with higher base delay
		MaxDelay:          120 * time.Second, // Keep 2 minute max delay
	}
	rateLimiter := awslib.GetGlobalRegistry().GetRateLimiter(rateLimiterKey, iamConfig)

	// Get the shared worker pool
	pool := worker.GetSharedPool()

	// Channel to collect results with enough buffer to avoid blocking
	resultChan := make(chan *awslib.ScanResult, 10000)
	errorChan := make(chan error, 1)

	// WaitGroup to track all submitted tasks
	var wg sync.WaitGroup
	var activeWorkers int32

	// Process users in chunks to avoid memory issues
	processUser := func(ctx context.Context, user *iam.User) error {
		defer wg.Done()
		atomic.AddInt32(&activeWorkers, 1)
		defer atomic.AddInt32(&activeWorkers, -1)

		task := &userTask{
			user:        user,
			iamClient:   iamClient,
			accountID:   opts.AccountID,
			region:      opts.Region,
			scanner:     s,
			opts:        opts,
			now:         time.Now(),
			rateLimiter: rateLimiter,
		}

		result, err := task.processUser(ctx)
		if err != nil {
			if strings.Contains(err.Error(), "Throttling:") {
				logging.Debug("Rate limited by AWS, backing off", map[string]interface{}{
					"user_name": aws.StringValue(user.UserName),
					"account":   opts.AccountID,
					"region":    opts.Region,
					"error":     err.Error(),
				})
				rateLimiter.OnFailure()
				return err
			}
			select {
			case errorChan <- err:
			default:
				logging.Error("Failed to process user", err, map[string]interface{}{
					"user_name": aws.StringValue(user.UserName),
					"account":   opts.AccountID,
					"region":    opts.Region,
				})
			}
			return err
		}
		rateLimiter.OnSuccess()
		if result != nil {
			select {
			case resultChan <- result:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}

	// Start listing users and immediately process each one
	var results awslib.ScanResults
	var processingError error
	done := make(chan bool)

	// Start a goroutine to collect results
	go func() {
		for result := range resultChan {
			results = append(results, *result)
		}
		done <- true
	}()

	// List and process users
	err = iamClient.ListUsersPages(&iam.ListUsersInput{},
		func(page *iam.ListUsersOutput, lastPage bool) bool {
			for _, user := range page.Users {
				// Skip if we've encountered an error
				select {
				case err := <-errorChan:
					processingError = err
					return false
				default:
					// Skip users that are newer than DaysUnused
					age := time.Since(aws.TimeValue(user.CreateDate))
					if age.Hours()/24 <= float64(opts.DaysUnused) {
						continue
					}

					// Immediately submit each user to the worker pool
					user := user // Create new variable for closure
					wg.Add(1)
					pool.Submit(func(ctx context.Context) error {
						return processUser(ctx, user)
					})
				}
			}
			return !lastPage && processingError == nil
		})

	if err != nil {
		logging.Error("Failed to list IAM users", err, nil)
		return nil, fmt.Errorf("failed to list IAM users: %w", err)
	}

	// Wait for all workers to complete
	wg.Wait()

	// Now it's safe to close the result channel
	close(resultChan)

	// Wait for result collection to complete
	<-done

	if processingError != nil {
		return nil, processingError
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
