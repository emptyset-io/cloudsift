package aws

import (
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials/stscreds"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sts"

	"cloudsift/internal/logging"
	"net/http"

	"cloudsift/internal/config"
)

// GetSession creates a new AWS session with optional region and role
// Deprecated: Use GetSessionChain + GetSessionInRegion instead
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

	// Get current account ID for role ARN
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

// GetSessionChain creates a new AWS session with proper role assumption chain:
// Base Profile -> Organization Role (optional) -> Scanner Role (optional, in target account)
func GetSessionChain(organizationRole, scannerRole string, targetAccountID string, region string) (*session.Session, error) {
	logging.Debug("Creating AWS session chain", map[string]interface{}{
		"organization_role": organizationRole,
		"scanner_role":      scannerRole,
		"target_account":    targetAccountID,
		"region":            region,
	})

	// Create base session with profile and region
	baseSession, err := NewSession(config.Config.Profile, region)
	if err != nil {
		return nil, fmt.Errorf("failed to create base AWS session: %w", err)
	}

	// Get base session identity for logging
	stsSvc := sts.New(baseSession)
	baseIdentity, err := stsSvc.GetCallerIdentity(&sts.GetCallerIdentityInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to get base session identity: %w", err)
	}
	logging.Info("Created base session", map[string]interface{}{
		"account_id": *baseIdentity.Account,
		"arn":        *baseIdentity.Arn,
	})

	currentSession := baseSession

	// Assume organization role if provided
	if organizationRole != "" {
		logging.Debug("Attempting to assume organization role", map[string]interface{}{
			"role": organizationRole,
		})

		orgRoleARN := fmt.Sprintf("arn:aws:iam::%s:role/%s", *baseIdentity.Account, organizationRole)
		orgCreds := stscreds.NewCredentials(currentSession, orgRoleARN)
		orgSession, err := session.NewSession(aws.NewConfig().WithCredentials(orgCreds))
		if err != nil {
			return nil, fmt.Errorf("failed to assume organization role %s: %w", organizationRole, err)
		}

		// Verify org role assumption
		orgStsSvc := sts.New(orgSession)
		orgIdentity, err := orgStsSvc.GetCallerIdentity(&sts.GetCallerIdentityInput{})
		if err != nil {
			return nil, fmt.Errorf("failed to verify organization role assumption: %w", err)
		}
		logging.Debug("Assumed organization role", map[string]interface{}{
			"role_arn": *orgIdentity.Arn,
		})

		currentSession = orgSession
	}

	// Assume scanner role if provided
	if scannerRole != "" {
		// If target account specified, assume scanner role directly in that account
		if targetAccountID != "" {
			logging.Debug("Attempting to assume scanner role in target account", map[string]interface{}{
				"role":           scannerRole,
				"target_account": targetAccountID,
			})

			scannerRoleARN := fmt.Sprintf("arn:aws:iam::%s:role/%s", targetAccountID, scannerRole)
			scannerCreds := stscreds.NewCredentials(currentSession, scannerRoleARN)
			scannerSession, err := session.NewSession(aws.NewConfig().WithCredentials(scannerCreds))
			if err != nil {
				return nil, fmt.Errorf("failed to assume scanner role %s in account %s: %w", scannerRole, targetAccountID, err)
			}

			// Verify scanner role assumption
			scannerStsSvc := sts.New(scannerSession)
			scannerIdentity, err := scannerStsSvc.GetCallerIdentity(&sts.GetCallerIdentityInput{})
			if err != nil {
				return nil, fmt.Errorf("failed to verify scanner role assumption: %w", err)
			}
			logging.Debug("Assumed scanner role in target account", map[string]interface{}{
				"role_arn":       *scannerIdentity.Arn,
				"target_account": targetAccountID,
			})

			currentSession = scannerSession
		} else {
			// No target account, assume scanner role in current account
			stsSvc := sts.New(currentSession)
			identity, err := stsSvc.GetCallerIdentity(&sts.GetCallerIdentityInput{})
			if err != nil {
				return nil, fmt.Errorf("failed to get identity for scanner role assumption: %w", err)
			}

			scannerRoleARN := fmt.Sprintf("arn:aws:iam::%s:role/%s", *identity.Account, scannerRole)
			scannerCreds := stscreds.NewCredentials(currentSession, scannerRoleARN)
			scannerSession, err := session.NewSession(aws.NewConfig().WithCredentials(scannerCreds))
			if err != nil {
				return nil, fmt.Errorf("failed to assume scanner role %s: %w", scannerRole, err)
			}

			// Verify scanner role assumption
			scannerStsSvc := sts.New(scannerSession)
			scannerIdentity, err := scannerStsSvc.GetCallerIdentity(&sts.GetCallerIdentityInput{})
			if err != nil {
				return nil, fmt.Errorf("failed to verify scanner role assumption: %w", err)
			}
			logging.Debug("Assumed scanner role in current account", map[string]interface{}{
				"role_arn": *scannerIdentity.Arn,
			})

			currentSession = scannerSession
		}
	}

	return currentSession, nil
}

// NewSession creates a new AWS session with the specified profile and region
func NewSession(profile string, region string) (*session.Session, error) {
	cfg := aws.NewConfig()
	if region != "" {
		cfg = cfg.WithRegion(region)
	}

	// Create session options with profile
	opts := session.Options{
		Config:            *cfg,
		Profile:          profile,
		SharedConfigState: session.SharedConfigEnable,
	}

	// Create session with profile
	return session.NewSessionWithOptions(opts)
}

// GetSessionInRegion creates a new session in the specified region using credentials from an existing session
func GetSessionInRegion(sess *session.Session, region string) (*session.Session, error) {
	if region == "" {
		return sess, nil
	}

	// Create HTTP client with timeout
	httpClient := &http.Client{
		Timeout: 25 * time.Second, // Set timeout slightly less than worker pool timeout
	}

	// Create new session with updated region and timeout while preserving other config options
	newSess, err := session.NewSession(sess.Config.WithRegion(region).WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	return newSess, nil
}

// AssumeRole creates a new session by assuming the specified role in the target account
func AssumeRole(targetAccountID, roleName string, sess *session.Session) (*session.Session, error) {
	if roleName == "" {
		return sess, nil
	}

	logging.Debug("Attempting cross-account role assumption", map[string]interface{}{
		"target_account": targetAccountID,
		"role":           roleName,
	})

	// Construct role ARN for target account
	roleARN := fmt.Sprintf("arn:aws:iam::%s:role/%s", targetAccountID, roleName)

	// Create new session with assumed role
	creds := stscreds.NewCredentials(sess, roleARN)
	assumedSession, err := session.NewSession(aws.NewConfig().WithCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("failed to assume role %s in account %s: %w", roleName, targetAccountID, err)
	}

	// Verify role assumption
	stsSvc := sts.New(assumedSession)
	identity, err := stsSvc.GetCallerIdentity(&sts.GetCallerIdentityInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to verify cross-account role assumption: %w", err)
	}
	logging.Debug("Assumed cross-account role", map[string]interface{}{
		"role_arn": *identity.Arn,
	})

	return assumedSession, nil
}
