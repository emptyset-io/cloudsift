package aws

import (
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials/stscreds"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sts"
)

// GetSession creates a new AWS session with optional region and role
func GetSession(role string, region ...string) (*session.Session, error) {
	cfg := aws.NewConfig()
	if len(region) > 0 && region[0] != "" {
		cfg = cfg.WithRegion(region[0])
	}

	// Create base session
	sess, err := session.NewSession(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS session: %w", err)
	}

	// If no role specified, return base session
	if role == "" {
		return sess, nil
	}

	// Get current account ID
	svc := sts.New(sess)
	identity, err := svc.GetCallerIdentity(&sts.GetCallerIdentityInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to get caller identity: %w", err)
	}

	// Construct role ARN
	roleARN := fmt.Sprintf("arn:aws:iam::%s:role/%s", *identity.Account, role)

	// Create new session with assumed role
	creds := stscreds.NewCredentials(sess, roleARN)
	return session.NewSession(cfg.WithCredentials(creds))
}

// GetScannerSession creates a new AWS session for scanning
func GetScannerSession(opts ScanOptions) (*session.Session, error) {
	cfg := aws.NewConfig()
	if opts.Region != "" {
		cfg = cfg.WithRegion(opts.Region)
	}

	// Create base session
	sess, err := session.NewSession(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS session: %w", err)
	}

	// Get current identity to check if we're already running as a role
	svc := sts.New(sess)
	identity, err := svc.GetCallerIdentity(&sts.GetCallerIdentityInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to get caller identity: %w", err)
	}

	// If we're already running as the scanner role, just return the session
	if strings.Contains(*identity.Arn, opts.Role) {
		return sess, nil
	}

	currentAccountID := *identity.Account

	// If no scanner role specified, return base session
	if opts.Role == "" {
		return sess, nil
	}

	// If target account specified but no organization role, we can't do cross-account access
	if opts.TargetAccountID != "" && opts.TargetAccountID != currentAccountID && opts.OrganizationRole == "" {
		return nil, fmt.Errorf("organization role is required for cross-account access")
	}

	// If scanning current account or we're already running as the org role, assume scanner role directly
	if opts.TargetAccountID == "" || opts.TargetAccountID == currentAccountID || strings.Contains(*identity.Arn, opts.OrganizationRole) {
		roleARN := fmt.Sprintf("arn:aws:iam::%s:role/%s", currentAccountID, opts.Role)
		creds := stscreds.NewCredentials(sess, roleARN)
		return session.NewSession(cfg.WithCredentials(creds))
	}

	// For cross-account access, first assume organization role in current account
	orgRoleARN := fmt.Sprintf("arn:aws:iam::%s:role/%s", currentAccountID, opts.OrganizationRole)
	orgCreds := stscreds.NewCredentials(sess, orgRoleARN)
	orgSess, err := session.NewSession(cfg.WithCredentials(orgCreds))
	if err != nil {
		return nil, fmt.Errorf("failed to assume organization role: %w", err)
	}

	// Then use organization role to assume scanner role in target account
	scannerRoleARN := fmt.Sprintf("arn:aws:iam::%s:role/%s", opts.TargetAccountID, opts.Role)
	scannerCreds := stscreds.NewCredentials(orgSess, scannerRoleARN)
	return session.NewSession(cfg.WithCredentials(scannerCreds))
}

// AssumeRole creates a new session by assuming the specified role in the target account
func AssumeRole(targetAccountID, roleName string, sess *session.Session) (*session.Session, error) {
	if roleName == "" {
		return sess, nil
	}

	// Get current account ID if not provided
	if targetAccountID == "" {
		svc := sts.New(sess)
		identity, err := svc.GetCallerIdentity(&sts.GetCallerIdentityInput{})
		if err != nil {
			return nil, fmt.Errorf("failed to get caller identity: %w", err)
		}
		targetAccountID = *identity.Account
	}

	// Construct role ARN for target account
	roleARN := fmt.Sprintf("arn:aws:iam::%s:role/%s", targetAccountID, roleName)

	// Create new session with assumed role while preserving the original config
	cfg := sess.Config.Copy()
	creds := stscreds.NewCredentials(sess, roleARN)
	return session.NewSession(cfg.WithCredentials(creds))
}
