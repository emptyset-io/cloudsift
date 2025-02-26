package scanners

import (
	"context"
	"fmt"
	"strings"
	"time"

	awslib "cloudsift/internal/aws"
	"cloudsift/internal/aws/utils"
	"cloudsift/internal/logging"
	"cloudsift/internal/worker"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/iam"
)

// IAMRoleScanner scans for unused IAM roles
type IAMRoleScanner struct{}

// roleTask represents the task of processing a single IAM role
type roleTask struct {
	role       *iam.Role
	iamClient  *iam.IAM
	accountID  string
	region     string
	scanner    *IAMRoleScanner
	opts       awslib.ScanOptions
	now        time.Time
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
		logging.Error("Failed to get role last used", err, map[string]interface{}{
			"role_name": roleName,
		})
		t.rateLimiter.OnFailure()
		return nil, err
	}
	t.rateLimiter.OnSuccess()

	// Get policies and instance profiles with rate limiting
	if err := t.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit wait error: %w", err)
	}
	attachedPolicies, inlinePolicies, instanceProfiles, err := t.scanner.getRolePolicies(t.iamClient, roleName)
	if err != nil {
		logging.Error("Failed to get role policies", err, map[string]interface{}{
			"role_name": roleName,
		})
		t.rateLimiter.OnFailure()
		return nil, err
	}
	t.rateLimiter.OnSuccess()

	// Calculate age string
	ageString := t.scanner.calculateAgeString(t.now, lastUsedTime)

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
func (s *IAMRoleScanner) getRolePolicies(iamClient *iam.IAM, roleName string) ([]*iam.AttachedPolicy, []string, []*iam.InstanceProfile, error) {
	var attachedPolicies []*iam.AttachedPolicy
	var inlinePolicies []string
	var instanceProfiles []*iam.InstanceProfile

	// Get attached policies
	err := iamClient.ListAttachedRolePoliciesPages(&iam.ListAttachedRolePoliciesInput{
		RoleName: aws.String(roleName),
	}, func(page *iam.ListAttachedRolePoliciesOutput, lastPage bool) bool {
		attachedPolicies = append(attachedPolicies, page.AttachedPolicies...)
		return !lastPage
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to list attached policies: %w", err)
	}

	// Get inline policies
	err = iamClient.ListRolePoliciesPages(&iam.ListRolePoliciesInput{
		RoleName: aws.String(roleName),
	}, func(page *iam.ListRolePoliciesOutput, lastPage bool) bool {
		inlinePolicies = append(inlinePolicies, aws.StringValueSlice(page.PolicyNames)...)
		return !lastPage
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to list inline policies: %w", err)
	}

	// Get instance profiles
	err = iamClient.ListInstanceProfilesForRolePages(&iam.ListInstanceProfilesForRoleInput{
		RoleName: aws.String(roleName),
	}, func(page *iam.ListInstanceProfilesForRoleOutput, lastPage bool) bool {
		instanceProfiles = append(instanceProfiles, page.InstanceProfiles...)
		return !lastPage
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to list instance profiles: %w", err)
	}

	return attachedPolicies, inlinePolicies, instanceProfiles, nil
}

// calculateAgeString formats the time difference between now and a given time
func (s *IAMRoleScanner) calculateAgeString(now time.Time, t *time.Time) string {
	if t == nil {
		return "Never used"
	}

	duration := now.Sub(*t)
	days := int(duration.Hours() / 24)

	if days < 30 {
		return fmt.Sprintf("%d days ago", days)
	} else if days < 365 {
		months := days / 30
		return fmt.Sprintf("%d months ago", months)
	}
	years := days / 365
	return fmt.Sprintf("%d years ago", years)
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

	// Get current account ID
	accountID, err := utils.GetAccountID(sess)
	if err != nil {
		logging.Error("Failed to get caller identity", err, nil)
		return nil, fmt.Errorf("failed to get caller identity: %w", err)
	}

	// Create IAM client
	iamClient := iam.New(sess)

	// Get all IAM roles
	var roles []*iam.Role
	err = iamClient.ListRolesPages(&iam.ListRolesInput{},
		func(page *iam.ListRolesOutput, lastPage bool) bool {
			roles = append(roles, page.Roles...)
			return !lastPage
		})
	if err != nil {
		logging.Error("Failed to list IAM roles", err, nil)
		return nil, fmt.Errorf("failed to list IAM roles: %w", err)
	}

	// Create rate limiter with default config
	rateLimiter := awslib.NewRateLimiter(nil)

	// Get the shared worker pool
	pool := worker.GetSharedPool()

	// Channel to collect results
	resultChan := make(chan *awslib.ScanResult, len(roles))

	// Submit tasks for each role
	for _, role := range roles {
		role := role // Create new variable for closure
		task := func(ctx context.Context) error {
			rt := &roleTask{
				role:        role,
				iamClient:   iamClient,
				accountID:   accountID,
				region:      opts.Region,
				scanner:     s,
				opts:        opts,
				now:        time.Now(),
				rateLimiter: rateLimiter,
			}
			result, err := rt.processRole(ctx)
			if err != nil {
				return err
			}
			if result != nil {
				resultChan <- result
			}
			return nil
		}
		pool.Submit(task)
	}

	// Wait for all tasks to complete
	pool.WaitForTasks()
	close(resultChan)

	// Collect results
	var results awslib.ScanResults
	for result := range resultChan {
		results = append(results, *result)
	}

	return results, nil
}
