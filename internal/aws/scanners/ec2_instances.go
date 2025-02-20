package scanners

import (
	"fmt"
	"math"
	"strings"
	"time"

	awslib "cloudsift/internal/aws"
	"cloudsift/internal/logging"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudwatch"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/sts"
)

// EC2InstanceScanner scans for EC2 instances
type EC2InstanceScanner struct{}

func init() {
	// Register this scanner with the default registry
	awslib.DefaultRegistry.RegisterScanner(&EC2InstanceScanner{})
}

// Name implements Scanner interface
func (s *EC2InstanceScanner) Name() string {
	return "ec2-instances"
}

// ArgumentName implements Scanner interface
func (s *EC2InstanceScanner) ArgumentName() string {
	return "ec2-instances"
}

// Label implements Scanner interface
func (s *EC2InstanceScanner) Label() string {
	return "EC2 Instances"
}

// fetchMetric gets CloudWatch metrics for a given resource
func (s *EC2InstanceScanner) fetchMetric(cwClient *cloudwatch.CloudWatch, namespace, resourceID, dimensionName, metricName, stat string, startTime, endTime time.Time) ([]float64, error) {
	input := &cloudwatch.GetMetricDataInput{
		MetricDataQueries: []*cloudwatch.MetricDataQuery{
			{
				Id: aws.String(fmt.Sprintf("%sQuery", metricName)),
				MetricStat: &cloudwatch.MetricStat{
					Metric: &cloudwatch.Metric{
						Namespace:  aws.String(namespace),
						MetricName: aws.String(metricName),
						Dimensions: []*cloudwatch.Dimension{
							{
								Name:  aws.String(dimensionName),
								Value: aws.String(resourceID),
							},
						},
					},
					Period: aws.Int64(3600), // 1-hour granularity
					Stat:   aws.String(stat),
				},
				ReturnData: aws.Bool(true),
			},
		},
		StartTime: aws.Time(startTime),
		EndTime:   aws.Time(endTime),
	}

	result, err := cwClient.GetMetricData(input)
	if err != nil {
		return nil, err
	}

	if len(result.MetricDataResults) == 0 || len(result.MetricDataResults[0].Values) == 0 {
		return []float64{}, nil
	}

	values := make([]float64, len(result.MetricDataResults[0].Values))
	for i, v := range result.MetricDataResults[0].Values {
		values[i] = aws.Float64Value(v)
	}

	return values, nil
}

// analyzeInstanceUsage checks if an instance is underutilized
func (s *EC2InstanceScanner) analyzeInstanceUsage(cwClient *cloudwatch.CloudWatch, instance *ec2.Instance, startTime, endTime time.Time) ([]string, error) {
	instanceID := aws.StringValue(instance.InstanceId)
	var reasons []string

	// Fetch CPU Usage
	cpuUsage, err := s.fetchMetric(cwClient, "AWS/EC2", instanceID, "InstanceId", "CPUUtilization", "Average", startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch CPU metrics: %w", err)
	}

	// Fetch Network Traffic
	networkIn, err := s.fetchMetric(cwClient, "AWS/EC2", instanceID, "InstanceId", "NetworkPacketsIn", "Sum", startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch NetworkIn metrics: %w", err)
	}

	networkOut, err := s.fetchMetric(cwClient, "AWS/EC2", instanceID, "InstanceId", "NetworkPacketsOut", "Sum", startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch NetworkOut metrics: %w", err)
	}

	// Calculate averages and sums
	if len(cpuUsage) > 0 {
		var sum float64
		for _, v := range cpuUsage {
			sum += v
		}
		cpuAvg := sum / float64(len(cpuUsage))
		if cpuAvg < 2 {
			reasons = append(reasons, fmt.Sprintf("Low CPU usage: %.2f%% average", cpuAvg))
		}
	}

	if len(networkIn) > 0 && len(networkOut) > 0 {
		var networkInSum, networkOutSum float64
		for _, v := range networkIn {
			networkInSum += v
		}
		for _, v := range networkOut {
			networkOutSum += v
		}

		totalPackets := networkInSum + networkOutSum
		if totalPackets < 1_000_000 {
			reasons = append(reasons, fmt.Sprintf("Low network traffic: %.0f packets total", totalPackets))
		}
	}

	return reasons, nil
}

// getEBSVolumes gets the EBS volumes attached to an instance
func (s *EC2InstanceScanner) getEBSVolumes(ec2Client *ec2.EC2, instance *ec2.Instance, hoursRunning float64) ([]map[string]interface{}, error) {
	var ebsDetails []map[string]interface{}

	for _, blockDevice := range instance.BlockDeviceMappings {
		if blockDevice.Ebs == nil {
			continue
		}

		volumeID := aws.StringValue(blockDevice.Ebs.VolumeId)
		input := &ec2.DescribeVolumesInput{
			VolumeIds: []*string{aws.String(volumeID)},
		}

		result, err := ec2Client.DescribeVolumes(input)
		if err != nil {
			return nil, fmt.Errorf("failed to describe volume %s: %w", volumeID, err)
		}

		if len(result.Volumes) > 0 {
			volume := result.Volumes[0]
			ebsDetails = append(ebsDetails, map[string]interface{}{
				"VolumeId":     volumeID,
				"VolumeType":   aws.StringValue(volume.VolumeType),
				"SizeGB":       aws.Int64Value(volume.Size),
				"HoursRunning": hoursRunning,
			})
		}
	}

	return ebsDetails, nil
}

// Scan implements Scanner interface
func (s *EC2InstanceScanner) Scan(opts awslib.ScanOptions) (awslib.ScanResults, error) {
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

	// Create service clients
	ec2Client := ec2.New(sess)
	cwClient := cloudwatch.New(sess)

	// Get instances
	var results awslib.ScanResults
	err = ec2Client.DescribeInstancesPages(&ec2.DescribeInstancesInput{},
		func(page *ec2.DescribeInstancesOutput, lastPage bool) bool {
			for _, reservation := range page.Reservations {
				for _, instance := range reservation.Instances {
					// Convert AWS tags to map
					tags := make(map[string]string)
					for _, tag := range instance.Tags {
						tags[aws.StringValue(tag.Key)] = aws.StringValue(tag.Value)
					}

					// Get resource name from tags or use instance ID
					resourceName := aws.StringValue(instance.InstanceId)
					if name, ok := tags["Name"]; ok {
						resourceName = name
					}

					instanceState := aws.StringValue(instance.State.Name)
					launchTime := aws.TimeValue(instance.LaunchTime)
					age := time.Since(launchTime)
					ageInDays := int(age.Hours() / 24)
					hoursRunning := age.Hours()

					// Skip if instance is not old enough
					if ageInDays < opts.DaysUnused {
						continue
					}

					var reasons []string
					var ebsDetails []map[string]interface{}
					var err error

					// Handle stopped instances
					if instanceState == "stopped" {
						if ageInDays >= opts.DaysUnused {
							reasons = append(reasons, fmt.Sprintf("Stopped for %d days", ageInDays))
						}
					} else if instanceState != "running" {
						// Handle non-running instances
						reasons = append(reasons, fmt.Sprintf("Non-running state: %s for %d days", instanceState, ageInDays))
					} else {
						// Analyze running instances
						startTime := time.Now().Add(-time.Duration(opts.DaysUnused) * 24 * time.Hour)
						reasons, err = s.analyzeInstanceUsage(cwClient, instance, startTime, time.Now())
						if err != nil {
							logging.Error("Failed to analyze instance usage", err, map[string]interface{}{
								"instance_id": aws.StringValue(instance.InstanceId),
							})
						}
					}

					// Skip if no reasons found
					if len(reasons) == 0 {
						continue
					}

					// Get EBS volumes
					ebsDetails, err = s.getEBSVolumes(ec2Client, instance, hoursRunning)
					if err != nil {
						logging.Error("Failed to get EBS volumes", err, map[string]interface{}{
							"instance_id": aws.StringValue(instance.InstanceId),
						})
					}

					// Calculate costs
					costEstimator, err := awslib.NewCostEstimator("cache/costs.json")
					if err != nil {
						logging.Error("Failed to create cost estimator", err, map[string]interface{}{
							"account_id":    accountID,
							"region":        opts.Region,
							"resource_name": resourceName,
							"resource_id":   aws.StringValue(instance.InstanceId),
						})
					}

					var instanceCosts *awslib.CostBreakdown
					var ebsCosts *awslib.CostBreakdown
					var totalCosts *awslib.CostBreakdown

					// Only calculate instance costs if the instance is running
					if aws.StringValue(instance.State.Name) == "running" && costEstimator != nil {
						instanceCosts, err = costEstimator.CalculateCost(awslib.ResourceCostConfig{
							ResourceType:  "EC2",
							ResourceSize: aws.StringValue(instance.InstanceType),
							Region:       opts.Region,
							CreationTime: *instance.LaunchTime,
						})
						if err != nil {
							logging.Error("Failed to calculate instance costs", err, map[string]interface{}{
								"account_id":    accountID,
								"region":        opts.Region,
								"resource_name": resourceName,
								"resource_id":   aws.StringValue(instance.InstanceId),
							})
						}
					}

					// Calculate EBS volume costs
					if len(ebsDetails) > 0 && costEstimator != nil {
						ebsCosts = &awslib.CostBreakdown{}
						for _, ebs := range ebsDetails {
							volumeSize := ebs["SizeGB"].(int64)
							volumeType := ebs["VolumeType"].(string)
							volumeCost, err := costEstimator.CalculateCost(awslib.ResourceCostConfig{
								ResourceType:  "EBSVolumes",
								ResourceSize: volumeSize,
								Region:       opts.Region,
								CreationTime: *instance.LaunchTime,
								VolumeType:   volumeType,
							})
							if err != nil {
								logging.Error("Failed to calculate EBS costs", err, map[string]interface{}{
									"account_id":    accountID,
									"region":        opts.Region,
									"resource_name": resourceName,
									"resource_id":   aws.StringValue(instance.InstanceId),
									"volume_size":   volumeSize,
								})
								continue
							}
							// Add this volume's costs to total EBS costs
							ebsCosts.Hourly += volumeCost.Hourly
							ebsCosts.Daily += volumeCost.Daily
							ebsCosts.Monthly += volumeCost.Monthly
							ebsCosts.Yearly += volumeCost.Yearly
							ebsCosts.Lifetime += volumeCost.Lifetime
						}
					}

					// Calculate total costs
					if instanceCosts != nil || ebsCosts != nil {
						totalCosts = &awslib.CostBreakdown{}
						if instanceCosts != nil {
							totalCosts.Hourly += instanceCosts.Hourly
							totalCosts.Daily += instanceCosts.Daily
							totalCosts.Monthly += instanceCosts.Monthly
							totalCosts.Yearly += instanceCosts.Yearly
							totalCosts.Lifetime += instanceCosts.Lifetime
						}
						if ebsCosts != nil {
							totalCosts.Hourly += ebsCosts.Hourly
							totalCosts.Daily += ebsCosts.Daily
							totalCosts.Monthly += ebsCosts.Monthly
							totalCosts.Yearly += ebsCosts.Yearly
							totalCosts.Lifetime += ebsCosts.Lifetime
						}
					}

					// Collect all relevant details
					details := map[string]interface{}{
						"instance_id":   aws.StringValue(instance.InstanceId),
						"instance_type": aws.StringValue(instance.InstanceType),
						"state":         aws.StringValue(instance.State.Name),
						"launch_time":   instance.LaunchTime.Format("2006-01-02T15:04:05Z07:00"),
						"age_days":      ageInDays,
						"hours_running": math.Round(hoursRunning*100) / 100,
					}

					if len(ebsDetails) > 0 {
						details["ebs_volumes"] = ebsDetails
					}

					if instanceCosts != nil {
						details["instance_costs"] = instanceCosts
					}
					if ebsCosts != nil {
						details["ebs_costs"] = ebsCosts
					}
					if totalCosts != nil {
						details["total_costs"] = totalCosts
					}

					// Log that we found a result
					logging.Debug("Found unused EC2 instance", map[string]interface{}{
						"account_id":    accountID,
						"region":        opts.Region,
						"resource_name": resourceName,
						"resource_id":   aws.StringValue(instance.InstanceId),
					})

					results = append(results, awslib.ScanResult{
						ResourceType: s.Label(),
						ResourceName: resourceName,
						ResourceID:   aws.StringValue(instance.InstanceId),
						Reason:      strings.Join(reasons, ", "),
						Tags:        tags,
						Details:     details,
					})
				}
			}
			return !lastPage
		})

	if err != nil {
		logging.Error("Failed to describe EC2 instances", err, nil)
		return nil, fmt.Errorf("failed to describe EC2 instances: %w", err)
	}

	return results, nil
}
