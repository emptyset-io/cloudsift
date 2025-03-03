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
	"github.com/aws/aws-sdk-go/service/iam"
)

// IAMRoleScanner scans for unused IAM roles
type IAMRoleScanner struct{}

// roleTask represents the task of processing a single IAM role
type roleTask struct {
	role        *iam.Role
	iamClient   *iam.IAM
	accountID   string
	region      string
	scanner     *IAMRoleScanner
	opts        awslib.ScanOptions
	now         time.Time
	rateLimiter *awslib.RateLimiter
}

// processRole processes a single IAM role and returns a scan result if the role is unused
func (t *roleTask) processRole(ctx context.Context) (*awslib.ScanResult, error) {
	roleName := aws.StringValue(t.role.RoleName)
	roleARN := aws.StringValue(t.role.Arn)

	// Skip reserved roles
	if t.scanner.isReservedRole(roleARN) {
		logging.Debug("Skipping reserved role", map[string]interface{}{
			"role_name": roleName,
			"role_arn":  roleARN,
		})
		return nil, nil
	}

	logging.Debug("Analyzing IAM role", map[string]interface{}{
		"role_name": roleName,
		"role_arn":  roleARN,
	})

	// Get last used time with rate limiting
	if err := t.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit wait error: %w", err)
	}
	lastUsedTime, err := t.scanner.getRoleLastUsed(t.iamClient, roleName)
	if err != nil {
		// Log error with throttling info
		if strings.Contains(err.Error(), "Throttling:") {
			logging.Info("TEMP: IAM API throttled", map[string]interface{}{
				"role_name": roleName,
				"error":     err.Error(),
			})
		}
		logging.Error("Failed to get role last used", err, map[string]interface{}{
			"role_name": roleName,
		})
		t.rateLimiter.OnFailure()
		return nil, err
	}
	t.rateLimiter.OnSuccess()

	// Get policies and instance profiles with rate limiting
	attachedPolicies, inlinePolicies, instanceProfiles, err := t.getRolePolicies(ctx)
	if err != nil {
		logging.Error("Failed to get role policies", err, map[string]interface{}{
			"role_name": roleName,
		})
		t.rateLimiter.OnFailure()
		return nil, err
	}
	t.rateLimiter.OnSuccess()

	// Calculate age string
	lastUsed := aws.TimeValue(lastUsedTime)
	if lastUsed.IsZero() {
		lastUsed = aws.TimeValue(t.role.CreateDate)
	}
	ageString := utils.FormatTimeDifference(t.now, &lastUsed)

	// Determine unused reasons
	reasons := t.scanner.determineUnusedReasons(lastUsedTime, attachedPolicies, inlinePolicies, instanceProfiles, ageString, t.opts.DaysUnused, t.opts)

	if len(reasons) > 0 {
		// Create details map with IAM-specific fields
		details := map[string]interface{}{
			"LastUsed":         ageString,
			"InstanceProfiles": len(instanceProfiles),
			"PoliciesAttached": len(attachedPolicies) + len(inlinePolicies),
			"AccountId":        t.accountID,
			"Region":           t.region,
		}

		// Add additional IAM-specific details
		details["role_path"] = aws.StringValue(t.role.Path)
		details["create_date"] = aws.TimeValue(t.role.CreateDate).Format(time.RFC3339)
		details["max_session_duration"] = aws.Int64Value(t.role.MaxSessionDuration)
		if t.role.Description != nil {
			details["description"] = aws.StringValue(t.role.Description)
		}
		if t.role.PermissionsBoundary != nil {
			details["permissions_boundary"] = aws.StringValue(t.role.PermissionsBoundary.PermissionsBoundaryArn)
		}

		return &awslib.ScanResult{
			ResourceType: t.scanner.Label(),
			ResourceName: roleName,
			ResourceID:   roleARN,
			Reason:       strings.Join(reasons, "\n"),
			Details:      details,
		}, nil
	}

	return nil, nil
}

func init() {
	awslib.DefaultRegistry.RegisterScanner(&IAMRoleScanner{})
}

// Name implements Scanner interface
func (s *IAMRoleScanner) Name() string {
	return "iam-roles"
}

// ArgumentName implements Scanner interface
func (s *IAMRoleScanner) ArgumentName() string {
	return "iam-roles"
}

// Label implements Scanner interface
func (s *IAMRoleScanner) Label() string {
	return "IAM Roles"
}

// isReservedRole checks if the role is reserved (service or AWS reserved)
func (s *IAMRoleScanner) isReservedRole(roleARN string) bool {
	return strings.Contains(roleARN, "aws-reserved")
}

// getRoleLastUsed retrieves the last used time for a role
func (s *IAMRoleScanner) getRoleLastUsed(iamClient *iam.IAM, roleName string) (*time.Time, error) {
	input := &iam.GetRoleInput{
		RoleName: aws.String(roleName),
	}

	result, err := iamClient.GetRole(input)
	if err != nil {
		return nil, fmt.Errorf("failed to get role: %w", err)
	}

	if result.Role != nil && result.Role.RoleLastUsed != nil && result.Role.RoleLastUsed.LastUsedDate != nil {
		lastUsed := aws.TimeValue(result.Role.RoleLastUsed.LastUsedDate)
		return &lastUsed, nil
	}

	return nil, nil
}

// getRolePolicies retrieves the attached policies, inline policies, and instance profiles for the role
func (t *roleTask) getRolePolicies(ctx context.Context) ([]*iam.AttachedPolicy, []string, []*iam.InstanceProfile, error) {
	var attachedPolicies []*iam.AttachedPolicy
	var inlinePolicies []string
	var instanceProfiles []*iam.InstanceProfile
	roleName := aws.StringValue(t.role.RoleName)

	// Get attached policies with rate limiting
	if err := t.rateLimiter.Wait(ctx); err != nil {
		return nil, nil, nil, fmt.Errorf("rate limit wait error: %w", err)
	}
	err := t.iamClient.ListAttachedRolePoliciesPages(&iam.ListAttachedRolePoliciesInput{
		RoleName: aws.String(roleName),
	}, func(page *iam.ListAttachedRolePoliciesOutput, lastPage bool) bool {
		attachedPolicies = append(attachedPolicies, page.AttachedPolicies...)
		return !lastPage
	})
	if err != nil {
		t.rateLimiter.OnFailure()
		return nil, nil, nil, fmt.Errorf("failed to list attached policies: %w", err)
	}
	t.rateLimiter.OnSuccess()

	// Get inline policies with rate limiting
	if err := t.rateLimiter.Wait(ctx); err != nil {
		return nil, nil, nil, fmt.Errorf("rate limit wait error: %w", err)
	}
	err = t.iamClient.ListRolePoliciesPages(&iam.ListRolePoliciesInput{
		RoleName: aws.String(roleName),
	}, func(page *iam.ListRolePoliciesOutput, lastPage bool) bool {
		inlinePolicies = append(inlinePolicies, aws.StringValueSlice(page.PolicyNames)...)
		return !lastPage
	})
	if err != nil {
		t.rateLimiter.OnFailure()
		return nil, nil, nil, fmt.Errorf("failed to list inline policies: %w", err)
	}
	t.rateLimiter.OnSuccess()

	// Get instance profiles with rate limiting
	if err := t.rateLimiter.Wait(ctx); err != nil {
		return nil, nil, nil, fmt.Errorf("rate limit wait error: %w", err)
	}
	err = t.iamClient.ListInstanceProfilesForRolePages(&iam.ListInstanceProfilesForRoleInput{
		RoleName: aws.String(roleName),
	}, func(page *iam.ListInstanceProfilesForRoleOutput, lastPage bool) bool {
		instanceProfiles = append(instanceProfiles, page.InstanceProfiles...)
		return !lastPage
	})
	if err != nil {
		t.rateLimiter.OnFailure()
		return nil, nil, nil, fmt.Errorf("failed to list instance profiles: %w", err)
	}
	t.rateLimiter.OnSuccess()

	return attachedPolicies, inlinePolicies, instanceProfiles, nil
}

// determineUnusedReasons determines why a role is considered unused
func (s *IAMRoleScanner) determineUnusedReasons(lastUsedTime *time.Time, attachedPolicies []*iam.AttachedPolicy, inlinePolicies []string, instanceProfiles []*iam.InstanceProfile, ageString string, daysThreshold int, opts awslib.ScanOptions) []string {
	var reasons []string

	// Check for roles with no activity
	if lastUsedTime == nil {
		reasons = append(reasons, "Role has never been used.")
	} else {
		lastUsedDate := aws.TimeValue(lastUsedTime)
		age := time.Since(lastUsedDate)
		if age.Hours()/24 > float64(opts.DaysUnused) {
			reasons = append(reasons, fmt.Sprintf("Role has not been used in %s.", ageString))
		}
	}

	// Check for roles with no attached policies
	if len(attachedPolicies) == 0 {
		reasons = append(reasons, "Role has no attached policies.")
	}

	return reasons
}

// Scan implements Scanner interface
func (s *IAMRoleScanner) Scan(opts awslib.ScanOptions) (awslib.ScanResults, error) {
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
	logging.Info("Starting IAM role scan", map[string]interface{}{
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

	// Process roles in chunks to avoid memory issues
	processRole := func(ctx context.Context, role *iam.Role) error {
		defer wg.Done()
		atomic.AddInt32(&activeWorkers, 1)
		defer atomic.AddInt32(&activeWorkers, -1)

		task := &roleTask{
			role:        role,
			iamClient:   iamClient,
			accountID:   opts.AccountID,
			region:      opts.Region,
			scanner:     s,
			opts:        opts,
			now:         time.Now(),
			rateLimiter: rateLimiter,
		}

		result, err := task.processRole(ctx)
		if err != nil {
			if strings.Contains(err.Error(), "Throttling:") {
				// Log throttling events at debug level since they're expected and handled
				logging.Debug("Rate limited by AWS, backing off", map[string]interface{}{
					"role_name": aws.StringValue(role.RoleName),
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
				// If error channel is full, log the error
				logging.Error("Failed to process role", err, map[string]interface{}{
					"role_name": aws.StringValue(role.RoleName),
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

	// Start listing roles and immediately process each one
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

	// List and process roles
	err = iamClient.ListRolesPages(&iam.ListRolesInput{},
		func(page *iam.ListRolesOutput, lastPage bool) bool {
			for _, role := range page.Roles {
				// Skip if we've encountered an error
				select {
				case err := <-errorChan:
					processingError = err
					return false
				default:
					// Immediately submit each role to the worker pool
					role := role // Create new variable for closure
					wg.Add(1)
					pool.Submit(func(ctx context.Context) error {
						return processRole(ctx, role)
					})
				}
			}
			return !lastPage && processingError == nil
		})

	if err != nil {
		logging.Error("Failed to list IAM roles", err, nil)
		return nil, fmt.Errorf("failed to list IAM roles: %w", err)
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
