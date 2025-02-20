package scanners

import (
	"fmt"

	awslib "cloudsift/internal/aws"
	"cloudsift/internal/aws/utils"
	"cloudsift/internal/logging"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
)

// VPCScanner scans for unused VPCs
type VPCScanner struct{}

func init() {
	if err := awslib.DefaultRegistry.RegisterScanner(&VPCScanner{}); err != nil {
		panic(fmt.Sprintf("Failed to register VPC scanner: %v", err))
	}
}

// Name implements Scanner interface
func (s *VPCScanner) Name() string {
	return "vpcs"
}

// ArgumentName implements Scanner interface
func (s *VPCScanner) ArgumentName() string {
	return "vpcs"
}

// Label implements Scanner interface
func (s *VPCScanner) Label() string {
	return "VPCs"
}

// getVPCResourceCount counts the number of EC2 instances in a VPC
func (s *VPCScanner) getVPCResourceCount(ec2Client *ec2.EC2, vpcID string) (int, error) {
	input := &ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("vpc-id"),
				Values: []*string{aws.String(vpcID)},
			},
		},
	}

	var instanceCount int
	err := ec2Client.DescribeInstancesPages(input, func(page *ec2.DescribeInstancesOutput, lastPage bool) bool {
		for _, reservation := range page.Reservations {
			instanceCount += len(reservation.Instances)
		}
		return !lastPage
	})

	if err != nil {
		return 0, fmt.Errorf("failed to count instances in VPC %s: %w", vpcID, err)
	}

	return instanceCount, nil
}

// Scan implements Scanner interface
func (s *VPCScanner) Scan(opts awslib.ScanOptions) (awslib.ScanResults, error) {
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

	// Create EC2 service client
	ec2Client := ec2.New(sess)

	// Describe VPCs
	input := &ec2.DescribeVpcsInput{}
	vpcs, err := ec2Client.DescribeVpcs(input)
	if err != nil {
		logging.Error("Failed to describe VPCs", err, nil)
		return nil, fmt.Errorf("failed to describe VPCs: %w", err)
	}

	var results awslib.ScanResults

	// Analyze each VPC
	for _, vpc := range vpcs.Vpcs {
		vpcID := aws.StringValue(vpc.VpcId)
		isDefault := aws.BoolValue(vpc.IsDefault)

		// Skip default VPCs
		if isDefault {
			logging.Debug("Skipping default VPC", map[string]interface{}{
				"vpc_id": vpcID,
			})
			continue
		}

		// Get VPC name from tags
		var vpcName string
		for _, tag := range vpc.Tags {
			if aws.StringValue(tag.Key) == "Name" {
				vpcName = aws.StringValue(tag.Value)
				break
			}
		}
		if vpcName == "" {
			vpcName = vpcID
		}

		// Count resources in VPC
		resourceCount, err := s.getVPCResourceCount(ec2Client, vpcID)
		if err != nil {
			logging.Error("Failed to get VPC resource count", err, map[string]interface{}{
				"vpc_id": vpcID,
			})
			continue
		}

		// Only report VPCs with no resources
		if resourceCount == 0 {
			// Extract all tags
			tags := make(map[string]string)
			for _, tag := range vpc.Tags {
				tags[aws.StringValue(tag.Key)] = aws.StringValue(tag.Value)
			}

			// Create result
			result := awslib.ScanResult{
				ResourceType: s.Label(),
				ResourceName: vpcName,
				ResourceID:   vpcID,
				Reason:       "VPC has no resources",
				Details: map[string]interface{}{
					"account_id":     accountID,
					"region":         opts.Region,
					"cidr_block":     aws.StringValue(vpc.CidrBlock),
					"is_default":     isDefault,
					"state":          aws.StringValue(vpc.State),
					"resource_count": resourceCount,
				},
				Tags: tags,
			}

			results = append(results, result)
		}
	}

	return results, nil
}
