package scanners

import (
	"fmt"
	"math"
	"strings"
	"time"

	awslib "cloudsift/internal/aws"
	"cloudsift/internal/aws/utils"
	"cloudsift/internal/logging"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudwatch"
	"github.com/aws/aws-sdk-go/service/ec2"
)

// EC2InstanceScanner scans for EC2 instances
type EC2InstanceScanner struct{}

func init() {
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
	// Ensure start time is before end time and they're not equal
	if startTime.Equal(endTime) {
		startTime = startTime.Add(-1 * time.Hour)
	}

	// Use the utility but preserve our need for array of values
	config := utils.MetricConfig{
		Namespace:     namespace,
		ResourceID:    resourceID,
		DimensionName: dimensionName,
		MetricName:    metricName,
		Statistic:     stat,
		StartTime:     startTime,
		EndTime:       endTime,
	}

	// We need to use GetMetricData directly since we need array of values
	metricID := strings.ToLower(strings.ReplaceAll(metricName, ".", "_"))
	input := &cloudwatch.GetMetricDataInput{
		MetricDataQueries: []*cloudwatch.MetricDataQuery{
			{
				Id: aws.String(fmt.Sprintf("m_%s", metricID)),
				MetricStat: &cloudwatch.MetricStat{
					Metric: &cloudwatch.Metric{
						Namespace:  aws.String(config.Namespace),
						MetricName: aws.String(config.MetricName),
						Dimensions: []*cloudwatch.Dimension{
							{
								Name:  aws.String(config.DimensionName),
								Value: aws.String(config.ResourceID),
							},
						},
					},
					Period: aws.Int64(3600), // 1-hour granularity
					Stat:   aws.String(config.Statistic),
				},
				ReturnData: aws.Bool(true),
			},
		},
		StartTime: aws.Time(config.StartTime),
		EndTime:   aws.Time(config.EndTime),
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
func (s *EC2InstanceScanner) analyzeInstanceUsage(cwClient *cloudwatch.CloudWatch, instance *ec2.Instance, startTime, endTime time.Time, daysUnused int) ([]string, error) {
	instanceID := aws.StringValue(instance.InstanceId)
	var reasons []string

	// Fetch CPU Usage
	logging.Debug("Fetching CPU metrics", map[string]interface{}{
		"instance_id": instanceID,
		"start_time":  startTime,
		"end_time":    endTime,
	})
	cpuUsage, err := s.fetchMetric(cwClient, "AWS/EC2", instanceID, "InstanceId", "CPUUtilization", "Average", startTime, endTime)
	if err != nil {
		logging.Error("Failed to fetch CPU metrics", err, map[string]interface{}{
			"instance_id": instanceID,
			"start_time":  startTime,
			"end_time":    endTime,
		})
		return nil, fmt.Errorf("failed to fetch CPU metrics: %w", err)
	}

	// Fetch Network Traffic
	logging.Debug("Fetching network metrics", map[string]interface{}{
		"instance_id": instanceID,
		"start_time":  startTime,
		"end_time":    endTime,
	})
	networkIn, err := s.fetchMetric(cwClient, "AWS/EC2", instanceID, "InstanceId", "NetworkPacketsIn", "Sum", startTime, endTime)
	if err != nil {
		logging.Error("Failed to fetch NetworkIn metrics", err, map[string]interface{}{
			"instance_id": instanceID,
			"start_time":  startTime,
			"end_time":    endTime,
		})
		return nil, fmt.Errorf("failed to fetch NetworkIn metrics: %w", err)
	}

	networkOut, err := s.fetchMetric(cwClient, "AWS/EC2", instanceID, "InstanceId", "NetworkPacketsOut", "Sum", startTime, endTime)
	if err != nil {
		logging.Error("Failed to fetch NetworkOut metrics", err, map[string]interface{}{
			"instance_id": instanceID,
			"start_time":  startTime,
			"end_time":    endTime,
		})
		return nil, fmt.Errorf("failed to fetch NetworkOut metrics: %w", err)
	}

	// Calculate averages and sums
	if len(cpuUsage) > 0 {
		var sum float64
		for _, v := range cpuUsage {
			sum += v
		}
		cpuAvg := sum / float64(len(cpuUsage))
		logging.Debug("CPU utilization analysis", map[string]interface{}{
			"instance_id":     instanceID,
			"cpu_avg":         cpuAvg,
			"samples_count":   len(cpuUsage),
			"analysis_period": fmt.Sprintf("%d days", daysUnused),
		})
		if cpuAvg < 5 {
			reasons = append(reasons, fmt.Sprintf("Very low CPU utilization (%.2f%%) in the last %d days.", cpuAvg, daysUnused))
		}
	} else {
		logging.Debug("No CPU metrics available", map[string]interface{}{
			"instance_id": instanceID,
			"period":      fmt.Sprintf("%d days", daysUnused),
		})
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
		logging.Debug("Network activity analysis", map[string]interface{}{
			"instance_id":       instanceID,
			"network_in_sum":    networkInSum,
			"network_out_sum":   networkOutSum,
			"total_packets":     totalPackets,
			"samples_count_in":  len(networkIn),
			"samples_count_out": len(networkOut),
			"analysis_period":   fmt.Sprintf("%d days", daysUnused),
		})
		if totalPackets < 1_000_000 {
			reasons = append(reasons, fmt.Sprintf("Very low network activity (in: %.2f KB/s, out: %.2f KB/s) in the last %d days.", networkInSum/1024, networkOutSum/1024, daysUnused))
		}
	} else {
		logging.Debug("No network metrics available", map[string]interface{}{
			"instance_id": instanceID,
			"period":      fmt.Sprintf("%d days", daysUnused),
		})
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

func (s *EC2InstanceScanner) Scan(opts awslib.ScanOptions) (awslib.ScanResults, error) {
	// Get regional session
	sess, err := awslib.GetSessionInRegion(opts.Session, opts.Region)
	if err != nil {
		logging.Error("Failed to create regional session", err, map[string]interface{}{
			"scanner": s.Label(),
			"region":  opts.Region,
		})
		return nil, fmt.Errorf("failed to create regional session: %w", err)
	}

	// Get current account ID
	accountID, err := utils.GetAccountID(sess)
	if err != nil {
		logging.Error("Failed to get caller identity", err, nil)
		return nil, fmt.Errorf("failed to get caller identity: %w", err)
	}

	// Initialize metrics
	var totalInstances int
	var costCalculations int
	startTime := time.Now()

	// Log scan start
	logging.Info("Starting EC2 instance scan", map[string]interface{}{
		"account_id": accountID,
		"region":     opts.Region,
	})

	// Create service clients
	clients := utils.CreateServiceClients(sess)
	ec2Client := ec2.New(sess) // Keep direct EC2 client for backward compatibility

	// Get instances
	var results awslib.ScanResults
	endTime := time.Now().UTC()
	metricStartTime := endTime.Add(-time.Duration(opts.DaysUnused) * 24 * time.Hour)

	input := &ec2.DescribeInstancesInput{
		MaxResults: nil, // Ensure we don't limit results per page
	}

	err = ec2Client.DescribeInstancesPages(input, func(page *ec2.DescribeInstancesOutput, lastPage bool) bool {
		// Log page processing
		logging.Debug("Processing instance page", map[string]interface{}{
			"account_id":   accountID,
			"region":       opts.Region,
			"reservations": len(page.Reservations),
			"is_last_page": lastPage,
		})

		for _, reservation := range page.Reservations {
			for _, instance := range reservation.Instances {
				totalInstances++

				// Skip terminated instances
				if aws.StringValue(instance.State.Name) == "terminated" {
					logging.Debug("Skipping terminated instance", map[string]interface{}{
						"instance_id": aws.StringValue(instance.InstanceId),
					})
					continue
				}

				// Get instance name from tags
				name := aws.StringValue(instance.InstanceId)
				for _, tag := range instance.Tags {
					if aws.StringValue(tag.Key) == "Name" {
						name = aws.StringValue(tag.Value)
						break
					}
				}

				// Convert AWS tags to map
				tags := make(map[string]string)
				for _, tag := range instance.Tags {
					tags[aws.StringValue(tag.Key)] = aws.StringValue(tag.Value)
				}

				// Get EBS details for the instance
				ebsDetails, err := s.getEBSVolumes(ec2Client, instance, time.Since(*instance.LaunchTime).Hours())
				if err != nil {
					logging.Warn("Failed to get EBS details for instance", map[string]interface{}{
						"instance_id": aws.StringValue(instance.InstanceId),
						"error":       err.Error(),
					})
				}

				// Check if instance is unused based on state
				var reasons []string
				if aws.StringValue(instance.State.Name) == "stopped" {
					logging.Info("Found stopped instance", map[string]interface{}{
						"instance_id": aws.StringValue(instance.InstanceId),
						"name":        name,
					})
					reasons = append(reasons, "Instance is stopped")
				} else if aws.StringValue(instance.State.Name) != "running" {
					reasons = append(reasons, fmt.Sprintf("Non-running state: %s", aws.StringValue(instance.State.Name)))
				} else {
					// Analyze running instances using launch time
					usageReasons, err := s.analyzeInstanceUsage(clients.CloudWatch, instance, metricStartTime, endTime, opts.DaysUnused)
					if err != nil {
						logging.Error("Failed to analyze instance usage", err, map[string]interface{}{
							"instance_id": aws.StringValue(instance.InstanceId),
						})
					} else {
						reasons = append(reasons, usageReasons...)
					}
				}

				// If we found reasons the instance is unused, add it to results
				if len(reasons) > 0 {
					// Build result details with all available attributes
					details := map[string]interface{}{
						"architecture":        aws.StringValue(instance.Architecture),
						"ami_id":              aws.StringValue(instance.ImageId),
						"instance_id":         aws.StringValue(instance.InstanceId),
						"instance_type":       aws.StringValue(instance.InstanceType),
						"kernel_id":           aws.StringValue(instance.KernelId),
						"key_name":            aws.StringValue(instance.KeyName),
						"launch_time":         instance.LaunchTime.Format(time.RFC3339),
						"platform":            aws.StringValue(instance.Platform),
						"private_dns_name":    aws.StringValue(instance.PrivateDnsName),
						"private_ip_address":  aws.StringValue(instance.PrivateIpAddress),
						"public_dns_name":     aws.StringValue(instance.PublicDnsName),
						"public_ip_address":   aws.StringValue(instance.PublicIpAddress),
						"ramdisk_id":          aws.StringValue(instance.RamdiskId),
						"root_device_name":    aws.StringValue(instance.RootDeviceName),
						"root_device_type":    aws.StringValue(instance.RootDeviceType),
						"source_dest_check":   aws.BoolValue(instance.SourceDestCheck),
						"state":               aws.StringValue(instance.State.Name),
						"state_code":          aws.Int64Value(instance.State.Code),
						"subnet_id":           aws.StringValue(instance.SubnetId),
						"vpc_id":              aws.StringValue(instance.VpcId),
						"hours_running":       time.Since(*instance.LaunchTime).Hours(),
						"ebs_optimized":       aws.BoolValue(instance.EbsOptimized),
						"ena_support":         aws.BoolValue(instance.EnaSupport),
						"hypervisor":          aws.StringValue(instance.Hypervisor),
						"virtualization_type": aws.StringValue(instance.VirtualizationType),
						"tags":                tags,
					}

					// Add state reason if present
					if instance.StateReason != nil {
						details["state_reason"] = aws.StringValue(instance.StateReason.Message)
					}

					// Add EBS details if available
					if len(ebsDetails) > 0 {
						details["ebs_volumes"] = ebsDetails
					}

					// Calculate costs
					costEstimator := awslib.DefaultCostEstimator
					var costDetails map[string]interface{}
					if costEstimator != nil {
						costCalculations++

						// Calculate EBS volume costs first - these are always included
						var totalCosts *awslib.CostBreakdown
						if len(ebsDetails) > 0 {
							for _, ebs := range ebsDetails {
								volumeSize := ebs["SizeGB"].(int64)
								volumeType := ebs["VolumeType"].(string)
								hoursRunning := ebs["HoursRunning"].(float64)

								volumeCost, err := costEstimator.CalculateCost(awslib.ResourceCostConfig{
									ResourceType: "EBSVolumes",
									ResourceSize: volumeSize,
									Region:       opts.Region,
									CreationTime: time.Now().Add(-time.Duration(hoursRunning) * time.Hour),
									VolumeType:   volumeType,
								})
								if err != nil {
									logging.Error("Failed to calculate EBS volume costs", err, map[string]interface{}{
										"instance_id": aws.StringValue(instance.InstanceId),
										"volume_size": volumeSize,
										"volume_type": volumeType,
									})
									continue
								}

								if totalCosts == nil {
									totalCosts = volumeCost
								} else {
									totalCosts.HourlyRate += volumeCost.HourlyRate
									totalCosts.DailyRate += volumeCost.DailyRate
									totalCosts.MonthlyRate += volumeCost.MonthlyRate
									totalCosts.YearlyRate += volumeCost.YearlyRate
								}
							}
						}

						// Only calculate EC2 instance costs if the instance is running
						if aws.StringValue(instance.State.Name) == "running" {
							instanceCosts, err := costEstimator.CalculateCost(awslib.ResourceCostConfig{
								ResourceType: "EC2",
								ResourceSize: aws.StringValue(instance.InstanceType),
								Region:       opts.Region,
								CreationTime: *instance.LaunchTime,
							})
							if err != nil {
								logging.Error("Failed to calculate EC2 instance costs", err, map[string]interface{}{
									"instance_id": aws.StringValue(instance.InstanceId),
								})
							} else if instanceCosts != nil {
								if totalCosts == nil {
									totalCosts = instanceCosts
								} else {
									totalCosts.HourlyRate += instanceCosts.HourlyRate
									totalCosts.DailyRate += instanceCosts.DailyRate
									totalCosts.MonthlyRate += instanceCosts.MonthlyRate
									totalCosts.YearlyRate += instanceCosts.YearlyRate
								}
							}
						}

						if totalCosts != nil {
							costDetails = map[string]interface{}{
								"total": totalCosts,
							}
						}
					}

					// Create scan result
					result := awslib.ScanResult{
						ResourceType: s.Label(),
						ResourceID:   aws.StringValue(instance.InstanceId),
						ResourceName: name,
						Details:      details,
						Cost:         costDetails,
						Reason:       strings.Join(reasons, "\n"),
					}

					results = append(results, result)

					// Log individual result
					logging.Info("Found unused instance", map[string]interface{}{
						"instance_id": aws.StringValue(instance.InstanceId),
						"name":        name,
						"state":       aws.StringValue(instance.State.Name),
						"reasons":     reasons,
					})
				}
			}
		}
		return true // Continue pagination
	})

	if err != nil {
		logging.Error("Failed to describe instances", err, map[string]interface{}{
			"account_id": accountID,
			"region":     opts.Region,
		})
		return nil, fmt.Errorf("failed to describe instances: %w", err)
	}

	// Log scan completion with metrics
	scanDuration := time.Since(startTime)
	logging.Info("Completed EC2 instance scan", map[string]interface{}{
		"account_id":        accountID,
		"region":            opts.Region,
		"total_instances":   totalInstances,
		"unused_instances":  len(results),
		"cost_calculations": costCalculations,
		"duration_ms":       scanDuration.Milliseconds(),
	})

	return results, nil
}

func roundCost(cost float64) float64 {
	return math.Round(cost*100) / 100
}
