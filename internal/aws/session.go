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

// AssumeRole creates a new session by assuming the specified role in the target account
func AssumeRole(targetAccountID, roleName string, sess *session.Session) (*session.Session, error) {
	if roleName == "" {
		return sess, nil
	}

	// Construct role ARN for target account
	roleARN := fmt.Sprintf("arn:aws:iam::%s:role/%s", targetAccountID, roleName)

	// Create new session with assumed role
	creds := stscreds.NewCredentials(sess, roleARN)
	return session.NewSession(aws.NewConfig().WithCredentials(creds))
}
