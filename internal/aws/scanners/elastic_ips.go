package scanners

import (
	"fmt"
	"time"

	awslib "cloudsift/internal/aws"
	"cloudsift/internal/logging"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/sts"
)

// ElasticIPScanner scans for unused Elastic IPs
type ElasticIPScanner struct{}

func init() {
	// Register this scanner with the default registry
	awslib.DefaultRegistry.RegisterScanner(&ElasticIPScanner{})
}

// Name implements Scanner interface
func (s *ElasticIPScanner) Name() string {
	return "elastic-ips"
}

// ArgumentName implements Scanner interface
func (s *ElasticIPScanner) ArgumentName() string {
	return "elastic-ips"
}

// Label implements Scanner interface
func (s *ElasticIPScanner) Label() string {
	return "Elastic IPs"
}

// checkNATGatewayAssociation checks if an Elastic IP is associated with a NAT Gateway
func (s *ElasticIPScanner) checkNATGatewayAssociation(ec2Client *ec2.EC2, allocationID string) bool {
	if allocationID == "" {
		logging.Debug("No Allocation ID provided for NAT Gateway association check", nil)
		return false
	}

	logging.Debug("Checking NAT Gateway association", map[string]interface{}{
		"allocation_id": allocationID,
	})

	input := &ec2.DescribeNatGatewaysInput{}
	var isAssociated bool

	err := ec2Client.DescribeNatGatewaysPages(input,
		func(page *ec2.DescribeNatGatewaysOutput, lastPage bool) bool {
			for _, natGateway := range page.NatGateways {
				for _, address := range natGateway.NatGatewayAddresses {
					if aws.StringValue(address.AllocationId) == allocationID {
						isAssociated = true
						return false // Stop paging
					}
				}
			}
			return true // Continue paging
		})

	if err != nil {
		logging.Error("Failed to check NAT Gateway association", err, map[string]interface{}{
			"allocation_id": allocationID,
		})
		return false
	}

	return isAssociated
}

// Scan implements Scanner interface
func (s *ElasticIPScanner) Scan(opts awslib.ScanOptions) (awslib.ScanResults, error) {
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
	stsSvc := sts.New(sess)
	identity, err := stsSvc.GetCallerIdentity(&sts.GetCallerIdentityInput{})
	if err != nil {
		logging.Error("Failed to get caller identity", err, nil)
		return nil, fmt.Errorf("failed to get caller identity: %w", err)
	}
	accountID := aws.StringValue(identity.Account)

	// Create EC2 client
	ec2Client := ec2.New(sess)

	// Get Elastic IPs
	addresses, err := ec2Client.DescribeAddresses(&ec2.DescribeAddressesInput{})
	if err != nil {
		logging.Error("Failed to describe addresses", err, nil)
		return nil, fmt.Errorf("failed to describe addresses: %w", err)
	}

	// Create cost estimator
	costEstimator, err := awslib.NewCostEstimator("cache/costs.json")
	if err != nil {
		logging.Error("Failed to create cost estimator", err, nil)
		return nil, fmt.Errorf("failed to create cost estimator: %w", err)
	}

	var results awslib.ScanResults

	for _, addr := range addresses.Addresses {
		allocationID := aws.StringValue(addr.AllocationId)
		publicIP := aws.StringValue(addr.PublicIp)

		// Convert AWS tags to map
		tags := make(map[string]string)
		for _, tag := range addr.Tags {
			tags[aws.StringValue(tag.Key)] = aws.StringValue(tag.Value)
		}

		// Get resource name from tags or use public IP
		resourceName := publicIP
		if name, ok := tags["Name"]; ok {
			resourceName = name
		}

		// Check if the Elastic IP is not associated with any resource
		if aws.StringValue(addr.InstanceId) == "" && aws.StringValue(addr.NetworkInterfaceId) == "" {
			if !s.checkNATGatewayAssociation(ec2Client, allocationID) {
				// Calculate costs - Elastic IPs have a flat rate of $0.005 per hour when not attached
				costs, err := costEstimator.CalculateCost(awslib.ResourceCostConfig{
					ResourceType:  "ElasticIP",
					ResourceSize: 1, // Flat rate per IP
					Region:       opts.Region,
					CreationTime: time.Now(), // Elastic IPs don't have creation time, use current time
				})
				if err != nil {
					logging.Error("Failed to calculate costs", err, map[string]interface{}{
						"account_id":    accountID,
						"region":        opts.Region,
						"resource_name": resourceName,
						"resource_id":   allocationID,
					})
				}

				result := awslib.ScanResult{
					ResourceID:   allocationID,
					ResourceName: resourceName,
					Details: map[string]interface{}{
						"public_ip":     publicIP,
						"allocation_id": allocationID,
						"tags":          tags,
						"reasons":       []string{"Not associated with any resource (EC2 Instance, Network Interface, or NAT Gateway)"},
					},
				}

				if costs != nil {
					result.Details["costs"] = costs
				}

				results = append(results, result)
			}
		}
	}

	return results, nil
}
