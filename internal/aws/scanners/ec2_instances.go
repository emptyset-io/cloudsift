package scanners

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	awslib "cloudsift/internal/aws"
	"cloudsift/internal/aws/utils"
	"cloudsift/internal/logging"
	"cloudsift/internal/worker"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudwatch"
	"github.com/aws/aws-sdk-go/service/ec2"

	"context"
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
	var resultsMutex sync.Mutex
	endTime := time.Now().UTC()
	metricStartTime := endTime.Add(-time.Duration(opts.DaysUnused) * 24 * time.Hour)

	input := &ec2.DescribeInstancesInput{
		MaxResults: aws.Int64(1000), // Use maximum page size for efficiency
	}

	// Get shared worker pool
	pool := worker.GetSharedPool()

	// Create a channel to collect tasks
	var tasks []worker.Task

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
				// Create a copy of instance for the closure
				instanceCopy := instance
				
				task := func(ctx context.Context) error {
					totalInstances++

					// Skip terminated instances
					if aws.StringValue(instanceCopy.State.Name) == "terminated" {
						logging.Debug("Skipping terminated instance", map[string]interface{}{
							"instance_id": aws.StringValue(instanceCopy.InstanceId),
						})
						return nil
					}

					// Get instance name from tags
					name := aws.StringValue(instanceCopy.InstanceId)
					for _, tag := range instanceCopy.Tags {
						if aws.StringValue(tag.Key) == "Name" {
							name = aws.StringValue(tag.Value)
							break
						}
					}

					// Convert AWS tags to map
					tags := make(map[string]string)
					for _, tag := range instanceCopy.Tags {
						tags[aws.StringValue(tag.Key)] = aws.StringValue(tag.Value)
					}

					// Get EBS details for the instance
					ebsDetails, err := s.getEBSVolumes(ec2Client, instanceCopy, time.Since(*instanceCopy.LaunchTime).Hours())
					if err != nil {
						logging.Warn("Failed to get EBS details for instance", map[string]interface{}{
							"instance_id": aws.StringValue(instanceCopy.InstanceId),
							"error":       err.Error(),
						})
					}

					// Check if instance is unused based on state
					var reasons []string
					if aws.StringValue(instanceCopy.State.Name) == "stopped" {
						logging.Info("Found stopped instance", map[string]interface{}{
							"instance_id": aws.StringValue(instanceCopy.InstanceId),
							"name":        name,
						})
						reasons = append(reasons, "Instance is stopped")
					} else if aws.StringValue(instanceCopy.State.Name) != "running" {
						reasons = append(reasons, fmt.Sprintf("Non-running state: %s", aws.StringValue(instanceCopy.State.Name)))
					} else {
						// Analyze running instances using launch time
						usageReasons, err := s.analyzeInstanceUsage(clients.CloudWatch, instanceCopy, metricStartTime, endTime, opts.DaysUnused)
						if err != nil {
							logging.Error("Failed to analyze instance usage", err, map[string]interface{}{
								"instance_id": aws.StringValue(instanceCopy.InstanceId),
							})
						} else {
							reasons = append(reasons, usageReasons...)
						}
					}

					// If we found reasons the instance is unused, add it to results
					if len(reasons) > 0 {
						// Build result details with all available attributes
						details := map[string]interface{}{
							"architecture":        aws.StringValue(instanceCopy.Architecture),
							"ami_id":              aws.StringValue(instanceCopy.ImageId),
							"instance_id":         aws.StringValue(instanceCopy.InstanceId),
							"instance_type":       aws.StringValue(instanceCopy.InstanceType),
							"kernel_id":           aws.StringValue(instanceCopy.KernelId),
							"key_name":            aws.StringValue(instanceCopy.KeyName),
							"launch_time":         instanceCopy.LaunchTime.Format(time.RFC3339),
							"platform":            aws.StringValue(instanceCopy.Platform),
							"private_dns_name":    aws.StringValue(instanceCopy.PrivateDnsName),
							"private_ip_address":  aws.StringValue(instanceCopy.PrivateIpAddress),
							"public_dns_name":     aws.StringValue(instanceCopy.PublicDnsName),
							"public_ip_address":   aws.StringValue(instanceCopy.PublicIpAddress),
							"ramdisk_id":          aws.StringValue(instanceCopy.RamdiskId),
							"root_device_name":    aws.StringValue(instanceCopy.RootDeviceName),
							"root_device_type":    aws.StringValue(instanceCopy.RootDeviceType),
							"source_dest_check":   aws.BoolValue(instanceCopy.SourceDestCheck),
							"state":               aws.StringValue(instanceCopy.State.Name),
							"state_code":          aws.Int64Value(instanceCopy.State.Code),
							"subnet_id":           aws.StringValue(instanceCopy.SubnetId),
							"vpc_id":              aws.StringValue(instanceCopy.VpcId),
							"hours_running":       time.Since(*instanceCopy.LaunchTime).Hours(),
							"ebs_optimized":       aws.BoolValue(instanceCopy.EbsOptimized),
							"ena_support":         aws.BoolValue(instanceCopy.EnaSupport),
							"hypervisor":          aws.StringValue(instanceCopy.Hypervisor),
							"virtualization_type": aws.StringValue(instanceCopy.VirtualizationType),
							"tags":                tags,
						}

						// Add state reason if present
						if instanceCopy.StateReason != nil {
							details["state_reason"] = aws.StringValue(instanceCopy.StateReason.Message)
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
											"instance_id": aws.StringValue(instanceCopy.InstanceId),
											"volume_size": volumeSize,
											"volume_type": volumeType,
										})
										continue
									}

									if volumeCost != nil {
										lifetime := float64(int(volumeCost.HourlyRate*hoursRunning*100+0.5)) / 100
										volumeCost.Lifetime = &lifetime
										hours := float64(int(hoursRunning*100+0.5)) / 100
										volumeCost.HoursRunning = &hours
									}

									if totalCosts == nil {
										totalCosts = volumeCost
									} else {
										totalCosts.HourlyRate += volumeCost.HourlyRate
										totalCosts.DailyRate += volumeCost.DailyRate
										totalCosts.MonthlyRate += volumeCost.MonthlyRate
										totalCosts.YearlyRate += volumeCost.YearlyRate
										if volumeCost.Lifetime != nil {
											if totalCosts.Lifetime == nil {
												totalCosts.Lifetime = volumeCost.Lifetime
											} else {
												newLifetime := *totalCosts.Lifetime + *volumeCost.Lifetime
												totalCosts.Lifetime = &newLifetime
											}
										}
									}
								}
							}

							// Only calculate EC2 instance costs if the instance is running
							if aws.StringValue(instanceCopy.State.Name) == "running" {
								hoursRunning := time.Since(*instanceCopy.LaunchTime).Hours()
								instanceCosts, err := costEstimator.CalculateCost(awslib.ResourceCostConfig{
									ResourceType: "EC2",
									ResourceSize: aws.StringValue(instanceCopy.InstanceType),
									Region:       opts.Region,
									CreationTime: *instanceCopy.LaunchTime,
								})
								if err != nil {
									logging.Error("Failed to calculate EC2 instance costs", err, map[string]interface{}{
										"instance_id": aws.StringValue(instanceCopy.InstanceId),
									})
								} else if instanceCosts != nil {
									lifetime := float64(int(instanceCosts.HourlyRate*hoursRunning*100+0.5)) / 100
									instanceCosts.Lifetime = &lifetime
									hours := float64(int(hoursRunning*100+0.5)) / 100
									instanceCosts.HoursRunning = &hours

									if totalCosts == nil {
										totalCosts = instanceCosts
									} else {
										totalCosts.HourlyRate += instanceCosts.HourlyRate
										totalCosts.DailyRate += instanceCosts.DailyRate
										totalCosts.MonthlyRate += instanceCosts.MonthlyRate
										totalCosts.YearlyRate += instanceCosts.YearlyRate
										if instanceCosts.Lifetime != nil {
											if totalCosts.Lifetime == nil {
												totalCosts.Lifetime = instanceCosts.Lifetime
											} else {
												newLifetime := *totalCosts.Lifetime + *instanceCosts.Lifetime
												totalCosts.Lifetime = &newLifetime
											}
										}
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
							ResourceID:   aws.StringValue(instanceCopy.InstanceId),
							ResourceName: name,
							Details:      details,
							Cost:         costDetails,
							Reason:       strings.Join(reasons, "\n"),
						}

						// Thread-safe append to results
						resultsMutex.Lock()
						results = append(results, result)
						resultsMutex.Unlock()

						// Log individual result
						logging.Info("Found unused instance", map[string]interface{}{
							"instance_id": aws.StringValue(instanceCopy.InstanceId),
							"name":        name,
							"state":       aws.StringValue(instanceCopy.State.Name),
							"reasons":     reasons,
						})
					}
					return nil
				}
				tasks = append(tasks, task)
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

	// Execute all tasks in parallel
	pool.ExecuteTasks(tasks)

	// Log scan completion with metrics
	scanDuration := time.Since(startTime)
	logging.Info("Completed EC2 instance scan", map[string]interface{}{
		"account_id":        accountID,
		"region":           opts.Region,
		"total_instances":  totalInstances,
		"unused_instances": len(results),
		"cost_calculations": costCalculations,
		"duration_ms":      scanDuration.Milliseconds(),
	})

	return results, nil
}

func roundCost(cost float64) float64 {
	return math.Round(cost*100) / 100
}
