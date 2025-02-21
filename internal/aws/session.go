package aws

import (
	"fmt"

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

// GetScannerSession creates a new AWS session for scanning, using organization role if provided
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

	// If organization role is set, assume it first
	if opts.OrganizationRole != "" {
		// Get organization account ID from base session
		svc := sts.New(sess)
		identity, err := svc.GetCallerIdentity(&sts.GetCallerIdentityInput{})
		if err != nil {
			return nil, fmt.Errorf("failed to get caller identity: %w", err)
		}
		orgAccountID := *identity.Account

		// Assume organization role in the organization account
		orgRoleARN := fmt.Sprintf("arn:aws:iam::%s:role/%s", orgAccountID, opts.OrganizationRole)
		orgCreds := stscreds.NewCredentials(sess, orgRoleARN)
		orgSess, err := session.NewSession(cfg.WithCredentials(orgCreds))
		if err != nil {
			return nil, fmt.Errorf("failed to assume organization role: %w", err)
		}

		// If scanner role is specified, use org role session to assume scanner role in target account
		if opts.Role != "" {
			// Use target account ID if provided, otherwise default to org account
			targetAccountID := opts.TargetAccountID
			if targetAccountID == "" {
				targetAccountID = orgAccountID
			}

			// Now assume the scanner role in the target account using the org role session
			scannerRoleARN := fmt.Sprintf("arn:aws:iam::%s:role/%s", targetAccountID, opts.Role)
			scannerCreds := stscreds.NewCredentials(orgSess, scannerRoleARN)
			return session.NewSession(cfg.WithCredentials(scannerCreds))
		}

		return orgSess, nil
	}

	// If no organization role but scanner role specified, assume scanner role directly in current account
	if opts.Role != "" {
		svc := sts.New(sess)
		identity, err := svc.GetCallerIdentity(&sts.GetCallerIdentityInput{})
		if err != nil {
			return nil, fmt.Errorf("failed to get caller identity: %w", err)
		}
		currentAccountID := *identity.Account

		roleARN := fmt.Sprintf("arn:aws:iam::%s:role/%s", currentAccountID, opts.Role)
		creds := stscreds.NewCredentials(sess, roleARN)
		return session.NewSession(cfg.WithCredentials(creds))
	}

	return sess, nil
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
