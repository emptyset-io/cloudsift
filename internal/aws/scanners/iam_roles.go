package scanners

import (
	"context"
	"fmt"
	"strings"
	"sync"
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

func init() {
	if err := awslib.DefaultRegistry.RegisterScanner(&IAMRoleScanner{}); err != nil {
		panic(fmt.Sprintf("Failed to register IAM Role scanner: %v", err))
	}
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
	return strings.Contains(roleARN, "service-role") || strings.Contains(roleARN, "aws-reserved")
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
		reasons = append(reasons, fmt.Sprintf("Role has never been used in the last %d days.", opts.DaysUnused))
	} else {
		lastUsedDate := aws.TimeValue(lastUsedTime)
		age := time.Since(lastUsedDate)
		if age.Hours()/24 > float64(opts.DaysUnused) {
			reasons = append(reasons, fmt.Sprintf("Role has not been used in %d days (last used: %s).", 
				opts.DaysUnused, lastUsedDate.Format("2006-01-02")))
		}
	}

	// Check for roles with no attached policies
	if len(attachedPolicies) == 0 {
		reasons = append(reasons, fmt.Sprintf("Role has no attached policies for %d days.", opts.DaysUnused))
	}

	return reasons
}

// Scan implements Scanner interface
func (s *IAMRoleScanner) Scan(opts awslib.ScanOptions) (awslib.ScanResults, error) {
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

	var (
		results   awslib.ScanResults
		resultsMu sync.Mutex
		now       = time.Now()
	)

	// Create tasks for concurrent processing
	tasks := make([]worker.Task, 0, len(roles))
	for _, role := range roles {
		role := role // Create a new variable for the closure
		tasks = append(tasks, func(ctx context.Context) error {
			roleName := aws.StringValue(role.RoleName)
			roleARN := aws.StringValue(role.Arn)

			// Skip reserved roles
			if s.isReservedRole(roleARN) {
				logging.Debug("Skipping reserved role", map[string]interface{}{
					"role_name": roleName,
					"role_arn":  roleARN,
				})
				return nil
			}

			logging.Debug("Analyzing IAM role", map[string]interface{}{
				"role_name": roleName,
				"role_arn":  roleARN,
			})

			// Get last used time
			lastUsedTime, err := s.getRoleLastUsed(iamClient, roleName)
			if err != nil {
				logging.Error("Failed to get role last used", err, map[string]interface{}{
					"role_name": roleName,
				})
				return nil
			}

			// Get policies and instance profiles
			attachedPolicies, inlinePolicies, instanceProfiles, err := s.getRolePolicies(iamClient, roleName)
			if err != nil {
				logging.Error("Failed to get role policies", err, map[string]interface{}{
					"role_name": roleName,
				})
				return nil
			}

			// Calculate age string
			ageString := s.calculateAgeString(now, lastUsedTime)

			// Determine unused reasons
			reasons := s.determineUnusedReasons(lastUsedTime, attachedPolicies, inlinePolicies, instanceProfiles, ageString, opts.DaysUnused, opts)

			if len(reasons) > 0 {
				// Create details map with IAM-specific fields
				details := map[string]interface{}{
					"LastUsed":         ageString,
					"InstanceProfiles": len(instanceProfiles),
					"PoliciesAttached": len(attachedPolicies) + len(inlinePolicies),
					"AccountId":        accountID,
					"Region":           opts.Region,
				}

				// Add additional IAM-specific details
				details["role_path"] = aws.StringValue(role.Path)
				details["create_date"] = aws.TimeValue(role.CreateDate).Format(time.RFC3339)
				details["max_session_duration"] = aws.Int64Value(role.MaxSessionDuration)
				if role.Description != nil {
					details["description"] = aws.StringValue(role.Description)
				}
				if role.PermissionsBoundary != nil {
					details["permissions_boundary"] = aws.StringValue(role.PermissionsBoundary.PermissionsBoundaryArn)
				}

				result := awslib.ScanResult{
					ResourceType: s.Label(),
					ResourceName: roleName,
					ResourceID:   roleARN,
					Reason:      strings.Join(reasons, "\n"),
					Details:     details,
				}

				// Safely append to results
				resultsMu.Lock()
				results = append(results, result)
				resultsMu.Unlock()
			}
			return nil
		})
	}

	// Create and run worker pool with 10 workers
	pool := worker.NewPool(10)
	pool.ExecuteTasks(tasks)

	return results, nil
}
