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

					if costEstimator != nil {
						// Calculate instance costs
						hoursRunning := time.Since(*instance.LaunchTime).Hours()
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
						} else if instanceCosts != nil {
							// Calculate lifetime cost for the instance
							lifetime := roundCost(instanceCosts.HourlyRate * hoursRunning)
							instanceCosts.Lifetime = &lifetime
							instanceCosts.HoursRunning = &hoursRunning
						}

						// Calculate EBS volume costs
						if len(ebsDetails) > 0 {
							ebsCosts = &awslib.CostBreakdown{}
							for _, ebs := range ebsDetails {
								volumeSize := ebs["SizeGB"].(int64)
								volumeType := ebs["VolumeType"].(string)
								hoursRunning := ebs["HoursRunning"].(float64)

								volumeCost, err := costEstimator.CalculateCost(awslib.ResourceCostConfig{
									ResourceType:  "EBSVolumes",
									ResourceSize: volumeSize,
									Region:       opts.Region,
									CreationTime: time.Now().Add(-time.Duration(hoursRunning) * time.Hour),
									VolumeType:   volumeType,
								})
								if err != nil {
									logging.Error("Failed to calculate EBS costs", err, map[string]interface{}{
										"account_id":    accountID,
										"region":        opts.Region,
										"resource_name": resourceName,
										"resource_id":   aws.StringValue(instance.InstanceId),
									})
									continue
								}

								// Calculate lifetime cost for this volume
								lifetime := roundCost(volumeCost.HourlyRate * hoursRunning)
								volumeCost.Lifetime = &lifetime
								volumeCost.HoursRunning = &hoursRunning

								if ebsCosts == nil {
									ebsCosts = volumeCost
								} else {
									ebsCosts.HourlyRate += volumeCost.HourlyRate
									ebsCosts.DailyRate += volumeCost.DailyRate
									ebsCosts.MonthlyRate += volumeCost.MonthlyRate
									ebsCosts.YearlyRate += volumeCost.YearlyRate
									if volumeCost.Lifetime != nil {
										if ebsCosts.Lifetime == nil {
											ebsCosts.Lifetime = volumeCost.Lifetime
										} else {
											lifetime := *ebsCosts.Lifetime + *volumeCost.Lifetime
											ebsCosts.Lifetime = &lifetime
										}
									}
								}
							}
						}

						// Calculate total costs by combining instance and EBS costs
						totalCosts = &awslib.CostBreakdown{}
						// Always include EBS costs
						if ebsCosts != nil {
							totalCosts.HourlyRate = ebsCosts.HourlyRate
							totalCosts.DailyRate = ebsCosts.DailyRate
							totalCosts.MonthlyRate = ebsCosts.MonthlyRate
							totalCosts.YearlyRate = ebsCosts.YearlyRate
							totalCosts.Lifetime = ebsCosts.Lifetime
							totalCosts.HoursRunning = ebsCosts.HoursRunning
						}

						// Only include instance costs if the instance is not stopped
						if instanceState != "stopped" && instanceCosts != nil {
							totalCosts.HourlyRate += instanceCosts.HourlyRate
							totalCosts.DailyRate += instanceCosts.DailyRate
							totalCosts.MonthlyRate += instanceCosts.MonthlyRate
							totalCosts.YearlyRate += instanceCosts.YearlyRate
							if instanceCosts.Lifetime != nil {
								if totalCosts.Lifetime == nil {
									totalCosts.Lifetime = instanceCosts.Lifetime
								} else {
									lifetime := *totalCosts.Lifetime + *instanceCosts.Lifetime
									totalCosts.Lifetime = &lifetime
								}
							}
							if instanceCosts.HoursRunning != nil {
								if totalCosts.HoursRunning == nil {
									totalCosts.HoursRunning = instanceCosts.HoursRunning
								} else {
									// Use the max hours running between instance and EBS
									if *instanceCosts.HoursRunning > *totalCosts.HoursRunning {
										totalCosts.HoursRunning = instanceCosts.HoursRunning
									}
								}
							}
						}

						// Debug cost information
						logging.Debug("Cost breakdown for instance", map[string]interface{}{
							"instance_id":     aws.StringValue(instance.InstanceId),
							"state":           instanceState,
							"instance_costs":  instanceCosts,
							"ebs_costs":       ebsCosts,
							"total_costs":     totalCosts,
						})
					}

					// Build result details with all available attributes
					details := map[string]interface{}{
						"architecture":          aws.StringValue(instance.Architecture),
						"ami_id":               aws.StringValue(instance.ImageId),
						"instance_id":          aws.StringValue(instance.InstanceId),
						"instance_type":        aws.StringValue(instance.InstanceType),
						"kernel_id":            aws.StringValue(instance.KernelId),
						"key_name":             aws.StringValue(instance.KeyName),
						"launch_time":          instance.LaunchTime.Format(time.RFC3339),
						"platform":             aws.StringValue(instance.Platform),
						"private_dns_name":     aws.StringValue(instance.PrivateDnsName),
						"private_ip_address":   aws.StringValue(instance.PrivateIpAddress),
						"public_dns_name":      aws.StringValue(instance.PublicDnsName),
						"public_ip_address":    aws.StringValue(instance.PublicIpAddress),
						"ramdisk_id":          aws.StringValue(instance.RamdiskId),
						"root_device_name":     aws.StringValue(instance.RootDeviceName),
						"root_device_type":     aws.StringValue(instance.RootDeviceType),
						"source_dest_check":    aws.BoolValue(instance.SourceDestCheck),
						"state":               aws.StringValue(instance.State.Name),
						"state_code":          aws.Int64Value(instance.State.Code),
						"state_reason":        aws.StringValue(instance.StateReason.Message),
						"subnet_id":           aws.StringValue(instance.SubnetId),
						"vpc_id":              aws.StringValue(instance.VpcId),
						"hours_running":       time.Since(*instance.LaunchTime).Hours(),
						"ebs_optimized":       aws.BoolValue(instance.EbsOptimized),
						"ena_support":         aws.BoolValue(instance.EnaSupport),
						"hypervisor":          aws.StringValue(instance.Hypervisor),
						"virtualization_type": aws.StringValue(instance.VirtualizationType),
						"monitoring_state":    aws.StringValue(instance.Monitoring.State),
						"placement": map[string]interface{}{
							"availability_zone": aws.StringValue(instance.Placement.AvailabilityZone),
							"affinity":         aws.StringValue(instance.Placement.Affinity),
							"group_name":       aws.StringValue(instance.Placement.GroupName),
							"host_id":          aws.StringValue(instance.Placement.HostId),
							"tenancy":          aws.StringValue(instance.Placement.Tenancy),
						},
					}

					// Add network interfaces
					var networkInterfaces []map[string]interface{}
					for _, ni := range instance.NetworkInterfaces {
						niDetails := map[string]interface{}{
							"network_interface_id": aws.StringValue(ni.NetworkInterfaceId),
							"description":         aws.StringValue(ni.Description),
							"status":             aws.StringValue(ni.Status),
							"mac_address":        aws.StringValue(ni.MacAddress),
							"private_ip_address": aws.StringValue(ni.PrivateIpAddress),
							"private_dns_name":   aws.StringValue(ni.PrivateDnsName),
							"source_dest_check":  aws.BoolValue(ni.SourceDestCheck),
							"subnet_id":          aws.StringValue(ni.SubnetId),
							"vpc_id":             aws.StringValue(ni.VpcId),
						}
						networkInterfaces = append(networkInterfaces, niDetails)
					}
					details["network_interfaces"] = networkInterfaces

					// Add security groups
					var securityGroups []map[string]interface{}
					for _, sg := range instance.SecurityGroups {
						sgDetails := map[string]interface{}{
							"group_id":   aws.StringValue(sg.GroupId),
							"group_name": aws.StringValue(sg.GroupName),
						}
						securityGroups = append(securityGroups, sgDetails)
					}
					details["security_groups"] = securityGroups

					// Add block device mappings
					var blockDevices []map[string]interface{}
					for _, bd := range instance.BlockDeviceMappings {
						bdDetails := map[string]interface{}{
							"device_name": aws.StringValue(bd.DeviceName),
						}
						if bd.Ebs != nil {
							bdDetails["ebs"] = map[string]interface{}{
								"attach_time":          aws.TimeValue(bd.Ebs.AttachTime).Format(time.RFC3339),
								"delete_on_termination": aws.BoolValue(bd.Ebs.DeleteOnTermination),
								"status":               aws.StringValue(bd.Ebs.Status),
								"volume_id":            aws.StringValue(bd.Ebs.VolumeId),
							}
						}
						blockDevices = append(blockDevices, bdDetails)
					}
					details["block_device_mappings"] = blockDevices

					if len(ebsDetails) > 0 {
						details["ebs_volumes"] = ebsDetails
					}

					// Add costs to result
					costs := map[string]interface{}{
						"total": totalCosts,
					}
					if instanceState != "stopped" && instanceCosts != nil {
						costs["instance"] = instanceCosts
					}
					if ebsCosts != nil {
						costs["ebs"] = ebsCosts
					}

					result := awslib.ScanResult{
						ResourceType: s.Label(),
						ResourceName: resourceName,
						ResourceID:   aws.StringValue(instance.InstanceId),
						Details:     details,
						Tags:       tags,
						Cost:       costs,
					}

					if len(reasons) > 0 {
						result.Reason = strings.Join(reasons, ", ")
					}

					results = append(results, result)
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

func roundCost(cost float64) float64 {
	return math.Round(cost*100) / 100
}
