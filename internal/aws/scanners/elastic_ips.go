package scanners

import (
	"fmt"
	"time"

	awslib "cloudsift/internal/aws"
	"cloudsift/internal/aws/utils"
	"cloudsift/internal/logging"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
)

// ElasticIPScanner scans for unused Elastic IPs
type ElasticIPScanner struct{}

func init() {
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

// Scan implements Scanner interface
func (s *ElasticIPScanner) Scan(opts awslib.ScanOptions) (awslib.ScanResults, error) {
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

	// Create EC2 client
	ec2Client := ec2.New(sess)

	// Get Elastic IPs
	addresses, err := ec2Client.DescribeAddresses(&ec2.DescribeAddressesInput{})
	if err != nil {
		logging.Error("Failed to describe addresses", err, nil)
		return nil, fmt.Errorf("failed to describe addresses: %w", err)
	}

	// Use default cost estimator
	costEstimator := awslib.DefaultCostEstimator

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
			// Calculate costs - Elastic IPs have a flat rate of $0.005 per hour when not attached
			costs, err := costEstimator.CalculateCost(awslib.ResourceCostConfig{
				ResourceType: "ElasticIP",
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
				ResourceType: s.Label(),
				ResourceName: resourceName,
				ResourceID:   allocationID,
				Reason:       "Not associated with any resource",
				Details: map[string]interface{}{
					"account_id":               accountID,
					"region":                   opts.Region,
					"public_ip":                publicIP,
					"allocation_id":            allocationID,
					"domain":                   aws.StringValue(addr.Domain),
					"network_interface_id":     aws.StringValue(addr.NetworkInterfaceId),
					"network_interface_owner":  aws.StringValue(addr.NetworkInterfaceOwnerId),
					"private_ip_address":       aws.StringValue(addr.PrivateIpAddress),
					"public_ipv4_pool":         aws.StringValue(addr.PublicIpv4Pool),
					"carrier_ip":               aws.StringValue(addr.CarrierIp),
					"customer_owned_ip":        aws.StringValue(addr.CustomerOwnedIp),
					"customer_owned_ipv4_pool": aws.StringValue(addr.CustomerOwnedIpv4Pool),
					"network_border_group":     aws.StringValue(addr.NetworkBorderGroup),
					"association_id":           aws.StringValue(addr.AssociationId),
				},
				Tags: tags,
			}

			if costs != nil {
				result.Cost = map[string]interface{}{
					"total": costs,
				}
			}

			results = append(results, result)
		}
	}

	return results, nil
}
