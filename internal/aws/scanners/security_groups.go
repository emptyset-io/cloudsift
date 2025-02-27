package scanners

import (
	"fmt"

	awslib "cloudsift/internal/aws"
	"cloudsift/internal/logging"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
)

// SecurityGroupScanner scans for unused security groups
type SecurityGroupScanner struct{}

func init() {
	awslib.DefaultRegistry.RegisterScanner(&SecurityGroupScanner{})
}

// Name implements Scanner interface
func (s *SecurityGroupScanner) Name() string {
	return "security-groups"
}

// ArgumentName implements Scanner interface
func (s *SecurityGroupScanner) ArgumentName() string {
	return "security-groups"
}

// Label implements Scanner interface
func (s *SecurityGroupScanner) Label() string {
	return "Security Groups"
}

// Scan implements Scanner interface
func (s *SecurityGroupScanner) Scan(opts awslib.ScanOptions) (awslib.ScanResults, error) {
	// Get regional session
	sess, err := awslib.GetSessionInRegion(opts.Session, opts.Region)
	if err != nil {
		logging.Error("Failed to create regional session", err, map[string]interface{}{
			"region": opts.Region,
		})
		return nil, fmt.Errorf("failed to create regional session: %w", err)
	}

	// Create EC2 client
	ec2Client := ec2.New(sess)

	// Get all security groups
	var securityGroups []*ec2.SecurityGroup
	err = ec2Client.DescribeSecurityGroupsPages(&ec2.DescribeSecurityGroupsInput{},
		func(page *ec2.DescribeSecurityGroupsOutput, lastPage bool) bool {
			securityGroups = append(securityGroups, page.SecurityGroups...)
			return !lastPage
		})
	if err != nil {
		logging.Error("Failed to describe security groups", err, nil)
		return nil, fmt.Errorf("failed to describe security groups: %w", err)
	}

	var results awslib.ScanResults

	for _, sg := range securityGroups {
		sgID := aws.StringValue(sg.GroupId)
		sgName := aws.StringValue(sg.GroupName)

		// Skip default security group
		if sgName == "default" {
			continue
		}

		logging.Debug("Analyzing security group", map[string]interface{}{
			"group_id":   sgID,
			"group_name": sgName,
		})

		// Check for network interface associations
		input := &ec2.DescribeNetworkInterfacesInput{
			Filters: []*ec2.Filter{
				{
					Name:   aws.String("group-id"),
					Values: []*string{sg.GroupId},
				},
			},
		}

		var associations []*ec2.NetworkInterface
		err = ec2Client.DescribeNetworkInterfacesPages(input,
			func(page *ec2.DescribeNetworkInterfacesOutput, lastPage bool) bool {
				associations = append(associations, page.NetworkInterfaces...)
				return !lastPage
			})
		if err != nil {
			logging.Error("Failed to describe network interfaces", err, map[string]interface{}{
				"group_id": sgID,
			})
			continue
		}

		if len(associations) == 0 {
			// Get name from tags
			var resourceName string
			for _, tag := range sg.Tags {
				if aws.StringValue(tag.Key) == "Name" {
					resourceName = aws.StringValue(tag.Value)
					break
				}
			}
			if resourceName == "" {
				resourceName = sgName
			}

			// Create comprehensive details map
			details := map[string]interface{}{
				"GroupName":      sgName,
				"opts.AccountID": opts.AccountID,
				"Region":         opts.Region,
				"VpcId":          aws.StringValue(sg.VpcId),
				"GroupDesc":      aws.StringValue(sg.Description),
			}

			// Add inbound rules analysis
			var inboundRules []map[string]interface{}
			for _, rule := range sg.IpPermissions {
				ruleDetails := map[string]interface{}{
					"Protocol": aws.StringValue(rule.IpProtocol),
					"FromPort": aws.Int64Value(rule.FromPort),
					"ToPort":   aws.Int64Value(rule.ToPort),
				}

				// Add IP ranges
				if len(rule.IpRanges) > 0 {
					var ipRanges []string
					for _, ipRange := range rule.IpRanges {
						ipRanges = append(ipRanges, aws.StringValue(ipRange.CidrIp))
					}
					ruleDetails["IpRanges"] = ipRanges
				}

				// Add IPv6 ranges
				if len(rule.Ipv6Ranges) > 0 {
					var ipv6Ranges []string
					for _, ipv6Range := range rule.Ipv6Ranges {
						ipv6Ranges = append(ipv6Ranges, aws.StringValue(ipv6Range.CidrIpv6))
					}
					ruleDetails["Ipv6Ranges"] = ipv6Ranges
				}

				// Add security group references
				if len(rule.UserIdGroupPairs) > 0 {
					var groupRefs []string
					for _, group := range rule.UserIdGroupPairs {
						groupRefs = append(groupRefs, aws.StringValue(group.GroupId))
					}
					ruleDetails["ReferencedGroups"] = groupRefs
				}

				inboundRules = append(inboundRules, ruleDetails)
			}
			details["InboundRules"] = inboundRules

			// Add outbound rules analysis
			var outboundRules []map[string]interface{}
			for _, rule := range sg.IpPermissionsEgress {
				ruleDetails := map[string]interface{}{
					"Protocol": aws.StringValue(rule.IpProtocol),
					"FromPort": aws.Int64Value(rule.FromPort),
					"ToPort":   aws.Int64Value(rule.ToPort),
				}

				// Add IP ranges
				if len(rule.IpRanges) > 0 {
					var ipRanges []string
					for _, ipRange := range rule.IpRanges {
						ipRanges = append(ipRanges, aws.StringValue(ipRange.CidrIp))
					}
					ruleDetails["IpRanges"] = ipRanges
				}

				// Add IPv6 ranges
				if len(rule.Ipv6Ranges) > 0 {
					var ipv6Ranges []string
					for _, ipv6Range := range rule.Ipv6Ranges {
						ipv6Ranges = append(ipv6Ranges, aws.StringValue(ipv6Range.CidrIpv6))
					}
					ruleDetails["Ipv6Ranges"] = ipv6Ranges
				}

				// Add security group references
				if len(rule.UserIdGroupPairs) > 0 {
					var groupRefs []string
					for _, group := range rule.UserIdGroupPairs {
						groupRefs = append(groupRefs, aws.StringValue(group.GroupId))
					}
					ruleDetails["ReferencedGroups"] = groupRefs
				}

				outboundRules = append(outboundRules, ruleDetails)
			}
			details["OutboundRules"] = outboundRules

			result := awslib.ScanResult{
				ResourceType: s.Label(),
				ResourceName: resourceName,
				ResourceID:   sgID,
				Reason:       "Not associated with any resource (EC2 Instance or ENI)",
				Details:      details,
			}

			results = append(results, result)
		}
	}

	return results, nil
}
