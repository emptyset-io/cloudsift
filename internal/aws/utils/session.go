package utils

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudwatch"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/rds"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/sts"
)

// ServiceClients holds AWS service clients for commonly used services
type ServiceClients struct {
	EC2        *ec2.EC2
	S3         *s3.S3
	CloudWatch *cloudwatch.CloudWatch
	RDS        *rds.RDS
}

// GetAccountID retrieves the AWS account ID for the current session
func GetAccountID(sess *session.Session) (string, error) {
	stsSvc := sts.New(sess)
	identity, err := stsSvc.GetCallerIdentity(&sts.GetCallerIdentityInput{})
	if err != nil {
		return "", fmt.Errorf("failed to get caller identity: %w", err)
	}
	if identity.Account == nil {
		return "", fmt.Errorf("account ID is nil")
	}
	return *identity.Account, nil
}

// CreateServiceClients creates commonly used AWS service clients from a session
func CreateServiceClients(sess *session.Session) *ServiceClients {
	return &ServiceClients{
		EC2:        ec2.New(sess),
		S3:         s3.New(sess),
		CloudWatch: cloudwatch.New(sess),
		RDS:        rds.New(sess),
	}
}
