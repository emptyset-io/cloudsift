package scanners

import (
	"fmt"
	"time"

	awslib "cloudsift/internal/aws"
	"cloudsift/internal/aws/utils"
	"cloudsift/internal/logging"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudwatch"
	"github.com/aws/aws-sdk-go/service/ec2"
)

// NATGatewayScanner scans for unused NAT Gateways
type NATGatewayScanner struct{}

func init() {
	awslib.DefaultRegistry.RegisterScanner(&NATGatewayScanner{})
}

// ArgumentName implements Scanner interface
func (s *NATGatewayScanner) ArgumentName() string {
	return "nat-gateways"
}

// Label implements Scanner interface
func (s *NATGatewayScanner) Label() string {
	return "NAT Gateways"
}

// fetchMetric fetches a CloudWatch metric for a NAT Gateway
func (s *NATGatewayScanner) fetchMetric(cwClient *cloudwatch.CloudWatch, natGatewayID string, metricName string, startTime, endTime time.Time) (float64, error) {
	config := utils.MetricConfig{
		Namespace:     "AWS/NATGateway",
		ResourceID:    natGatewayID,
		DimensionName: "NatGatewayId",
		MetricName:    metricName,
		Statistic:     "Sum",
		StartTime:     startTime,
		EndTime:       endTime,
		Period:        86400, // 1 day
	}

	return utils.GetResourceMetrics(cwClient, config)
}

// analyzeNATGatewayUsage analyzes the usage of a NAT Gateway based on CloudWatch metrics
func (s *NATGatewayScanner) analyzeNATGatewayUsage(cwClient *cloudwatch.CloudWatch, natGatewayID string, daysUnused int) (bool, string, error) {
	// Calculate time range for metrics
	endTime := time.Now()
	startTime := endTime.Add(-time.Duration(daysUnused) * 24 * time.Hour)

	// Fetch metrics to determine if NAT Gateway is unused
	bytesInFromSource, err := s.fetchMetric(cwClient, natGatewayID, "BytesInFromSource", startTime, endTime)
	if err != nil {
		return false, "", fmt.Errorf("failed to fetch BytesInFromSource metric: %w", err)
	}

	bytesOutToDestination, err := s.fetchMetric(cwClient, natGatewayID, "BytesOutToDestination", startTime, endTime)
	if err != nil {
		return false, "", fmt.Errorf("failed to fetch BytesOutToDestination metric: %w", err)
	}

	bytesInFromDestination, err := s.fetchMetric(cwClient, natGatewayID, "BytesInFromDestination", startTime, endTime)
	if err != nil {
		return false, "", fmt.Errorf("failed to fetch BytesInFromDestination metric: %w", err)
	}

	bytesOutToSource, err := s.fetchMetric(cwClient, natGatewayID, "BytesOutToSource", startTime, endTime)
	if err != nil {
		return false, "", fmt.Errorf("failed to fetch BytesOutToSource metric: %w", err)
	}

	// Calculate total bytes and traffic in each direction
	totalBytes := bytesInFromSource + bytesOutToDestination + bytesInFromDestination + bytesOutToSource
	inboundBytes := bytesInFromSource + bytesInFromDestination
	outboundBytes := bytesOutToDestination + bytesOutToSource

	// Check for different unused conditions
	if totalBytes == 0 {
		return true, fmt.Sprintf("NAT Gateway has no traffic in the last %d days", daysUnused), nil
	}

	// Check for very low traffic (less than 1 MB over the entire period)
	if totalBytes < 1024*1024 {
		return true, fmt.Sprintf("NAT Gateway has minimal traffic (%.2f MB) in the last %d days", totalBytes/(1024*1024), daysUnused), nil
	}

	// Check for one-way traffic only (might indicate a misconfiguration)
	if inboundBytes == 0 {
		return true, fmt.Sprintf("NAT Gateway has outbound traffic only, no inbound traffic in the last %d days", daysUnused), nil
	}

	if outboundBytes == 0 {
		return true, fmt.Sprintf("NAT Gateway has inbound traffic only, no outbound traffic in the last %d days", daysUnused), nil
	}

	// Not considered unused
	return false, "", nil
}

// calculateNATGatewayCost calculates the cost of a NAT Gateway
func (s *NATGatewayScanner) calculateNATGatewayCost(natGateway *ec2.NatGateway, region string) (*awslib.CostBreakdown, error) {
	// Get creation time
	creationTime := aws.TimeValue(natGateway.CreateTime)

	// NAT Gateways have a flat hourly rate based on region
	config := awslib.ResourceCostConfig{
		ResourceType: "NATGateway",
		Region:       region,
		CreationTime: creationTime,
	}

	// Use the default cost estimator to calculate costs
	if awslib.DefaultCostEstimator == nil {
		// Only use hardcoded values if the cost estimator is not available
		hoursRunning := time.Since(creationTime).Hours()
		hourlyRate := 0.045 // Default hourly rate if cost estimator is not available

		lifetime := hourlyRate * hoursRunning

		return &awslib.CostBreakdown{
			HourlyRate:   hourlyRate,
			DailyRate:    hourlyRate * 24,
			MonthlyRate:  hourlyRate * 24 * 30,
			YearlyRate:   hourlyRate * 24 * 365,
			HoursRunning: aws.Float64(hoursRunning),
			Lifetime:     aws.Float64(lifetime),
		}, nil
	}

	costBreakdown, err := awslib.DefaultCostEstimator.CalculateCost(config)
	if err != nil || costBreakdown.HourlyRate == 0 {
		// Fallback to default pricing if cost estimator fails or returns zero
		hoursRunning := time.Since(creationTime).Hours()
		hourlyRate := 0.045 // Default hourly rate as fallback

		lifetime := hourlyRate * hoursRunning

		return &awslib.CostBreakdown{
			HourlyRate:   hourlyRate,
			DailyRate:    hourlyRate * 24,
			MonthlyRate:  hourlyRate * 24 * 30,
			YearlyRate:   hourlyRate * 24 * 365,
			HoursRunning: aws.Float64(hoursRunning),
			Lifetime:     aws.Float64(lifetime),
		}, nil
	}

	return costBreakdown, nil
}

// Scan implements Scanner interface
func (s *NATGatewayScanner) Scan(opts awslib.ScanOptions) (awslib.ScanResults, error) {
	// Get regional session
	sess, err := awslib.GetSessionInRegion(opts.Session, opts.Region)
	if err != nil {
		logging.Error("Failed to create regional session", err, map[string]interface{}{
			"region": opts.Region,
		})
		return nil, fmt.Errorf("failed to create regional session: %w", err)
	}

	// Create EC2 and CloudWatch service clients
	ec2Client := ec2.New(sess)
	cwClient := cloudwatch.New(sess)

	// Describe NAT Gateways
	input := &ec2.DescribeNatGatewaysInput{}
	natGateways, err := ec2Client.DescribeNatGateways(input)
	if err != nil {
		logging.Error("Failed to describe NAT Gateways", err, nil)
		return nil, fmt.Errorf("failed to describe NAT Gateways: %w", err)
	}

	var results awslib.ScanResults

	// Get days unused from options, default to 30 if not specified
	daysUnused := utils.Max(opts.DaysUnused, 30)

	// Analyze each NAT Gateway
	for _, natGateway := range natGateways.NatGateways {
		natGatewayID := aws.StringValue(natGateway.NatGatewayId)

		// Skip NAT Gateways that are not in 'available' state
		if aws.StringValue(natGateway.State) != "available" {
			logging.Debug("Skipping NAT Gateway not in 'available' state", map[string]interface{}{
				"nat_gateway_id": natGatewayID,
				"state":          aws.StringValue(natGateway.State),
			})
			continue
		}

		// Get NAT Gateway name from tags
		var natGatewayName string
		for _, tag := range natGateway.Tags {
			if aws.StringValue(tag.Key) == "Name" {
				natGatewayName = aws.StringValue(tag.Value)
				break
			}
		}
		if natGatewayName == "" {
			natGatewayName = natGatewayID
		}

		// Check if NAT Gateway is unused
		isUnused, reason, err := s.analyzeNATGatewayUsage(cwClient, natGatewayID, daysUnused)
		if err != nil {
			logging.Error("Failed to analyze NAT Gateway usage", err, map[string]interface{}{
				"nat_gateway_id": natGatewayID,
			})
			continue
		}

		if isUnused {
			// Calculate cost
			cost, err := s.calculateNATGatewayCost(natGateway, opts.Region)
			if err != nil {
				logging.Error("Failed to calculate NAT Gateway cost", err, map[string]interface{}{
					"nat_gateway_id": natGatewayID,
				})

				// Get creation time for fallback calculation
				creationTime := aws.TimeValue(natGateway.CreateTime)
				hoursRunning := time.Since(creationTime).Hours()
				hourlyRate := 0.045 // Default hourly rate as fallback
				lifetime := hourlyRate * hoursRunning

				cost = &awslib.CostBreakdown{
					HourlyRate:   hourlyRate,
					DailyRate:    hourlyRate * 24,
					MonthlyRate:  hourlyRate * 24 * 30,
					YearlyRate:   hourlyRate * 24 * 365,
					HoursRunning: aws.Float64(hoursRunning),
					Lifetime:     aws.Float64(lifetime),
				}
			}

			// Extract all tags
			tags := make(map[string]string)
			for _, tag := range natGateway.Tags {
				tags[aws.StringValue(tag.Key)] = aws.StringValue(tag.Value)
			}

			// Get creation time
			creationTime := aws.TimeValue(natGateway.CreateTime)
			hoursRunning := time.Since(creationTime).Hours()

			// Format cost details in the same way as other scanners (like EBS snapshots)
			costDetails := map[string]interface{}{
				"total": cost,
			}

			// Create result
			result := awslib.ScanResult{
				ResourceType: s.Label(),
				ResourceName: natGatewayName,
				ResourceID:   natGatewayID,
				Reason:       reason,
				Details: map[string]interface{}{
					"account_id":    opts.AccountID,
					"region":        opts.Region,
					"state":         aws.StringValue(natGateway.State),
					"vpc_id":        aws.StringValue(natGateway.VpcId),
					"subnet_id":     aws.StringValue(natGateway.SubnetId),
					"creation_time": creationTime,
					"hours_running": hoursRunning,
					"days_unused":   daysUnused,
				},
				Tags: tags,
				Cost: costDetails,
			}

			results = append(results, result)
		}
	}

	return results, nil
}
