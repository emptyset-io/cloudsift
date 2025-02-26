package scanners

import (
	"fmt"
	"strings"
	"time"

	awslib "cloudsift/internal/aws"
	"cloudsift/internal/aws/utils"
	"cloudsift/internal/logging"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudwatch"
	"github.com/aws/aws-sdk-go/service/ec2"
)

// EBSVolumeScanner scans for EBS volumes
type EBSVolumeScanner struct{}

func init() {
	awslib.DefaultRegistry.RegisterScanner(&EBSVolumeScanner{})
}

// Name implements Scanner interface
func (s *EBSVolumeScanner) Name() string {
	return "ebs-volumes"
}

// ArgumentName implements Scanner interface
func (s *EBSVolumeScanner) ArgumentName() string {
	return "ebs-volumes"
}

// Label implements Scanner interface
func (s *EBSVolumeScanner) Label() string {
	return "EBS Volumes"
}

// Scan implements Scanner interface
func (s *EBSVolumeScanner) Scan(opts awslib.ScanOptions) (awslib.ScanResults, error) {
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

	// Create service clients
	clients := utils.CreateServiceClients(sess)
	svc := ec2.New(sess) // Keep direct EC2 client for backward compatibility

	// Initialize metrics
	var totalVolumes int
	var costCalculations int
	startTime := time.Now()

	// Log scan start
	logging.Info("Starting EBS volume scan", map[string]interface{}{
		"account_id": accountID,
		"region":     opts.Region,
	})

	input := &ec2.DescribeVolumesInput{
		MaxResults: nil, // Ensure we don't limit results per page
	}

	var results awslib.ScanResults
	err = svc.DescribeVolumesPages(input, func(page *ec2.DescribeVolumesOutput, lastPage bool) bool {
		// Log page processing
		logging.Debug("Processing volume page", map[string]interface{}{
			"account_id":   accountID,
			"region":       opts.Region,
			"page_size":    len(page.Volumes),
			"is_last_page": lastPage,
		})

		for _, volume := range page.Volumes {
			totalVolumes++

			// Calculate age of volume
			age := time.Since(*volume.CreateTime)
			ageInDays := int(age.Hours() / 24)

			// Skip if volume is not old enough
			if ageInDays < opts.DaysUnused {
				continue
			}

			// Skip if volume is attached
			if len(volume.Attachments) > 0 {
				continue
			}

			// Convert AWS tags to map
			tags := make(map[string]string)
			for _, tag := range volume.Tags {
				tags[aws.StringValue(tag.Key)] = aws.StringValue(tag.Value)
			}

			// Get resource name from tags or use volume ID
			resourceName := aws.StringValue(volume.VolumeId)
			if name, ok := tags["Name"]; ok {
				resourceName = name
			}

			// Calculate costs with error handling and metrics
			costEstimator := awslib.DefaultCostEstimator
			var costs *awslib.CostBreakdown
			if costEstimator != nil {
				costCalculations++
				volumeSize := aws.Int64Value(volume.Size)
				volumeType := aws.StringValue(volume.VolumeType)
				hoursRunning := time.Since(*volume.CreateTime).Hours()

				costs, err = costEstimator.CalculateCost(awslib.ResourceCostConfig{
					ResourceType: "EBSVolumes",
					ResourceSize: volumeSize,
					Region:       opts.Region,
					CreationTime: *volume.CreateTime,
					VolumeType:   volumeType,
				})
				if err != nil {
					logging.Error("Failed to calculate costs", err, map[string]interface{}{
						"account_id":    accountID,
						"region":        opts.Region,
						"resource_name": resourceName,
						"resource_id":   aws.StringValue(volume.VolumeId),
					})
					// Continue processing even if cost calculation fails
				}

				// Calculate lifetime cost
				if costs != nil {
					lifetime := float64(int(costs.HourlyRate*hoursRunning*100+0.5)) / 100
					costs.Lifetime = &lifetime
					hours := float64(int(hoursRunning*100+0.5)) / 100
					costs.HoursRunning = &hours
				}
			}

			// Collect all relevant details
			details := map[string]interface{}{
				"volume_id":            aws.StringValue(volume.VolumeId),
				"size":                 aws.Int64Value(volume.Size),
				"state":                aws.StringValue(volume.State),
				"volume_type":          aws.StringValue(volume.VolumeType),
				"iops":                 aws.Int64Value(volume.Iops),
				"throughput":           aws.Int64Value(volume.Throughput),
				"encrypted":            aws.BoolValue(volume.Encrypted),
				"kms_key_id":           aws.StringValue(volume.KmsKeyId),
				"outpost_arn":          aws.StringValue(volume.OutpostArn),
				"create_time":          volume.CreateTime.Format(time.RFC3339),
				"hours_running":        time.Since(*volume.CreateTime).Hours(),
				"multi_attach_enabled": aws.BoolValue(volume.MultiAttachEnabled),
				"fast_restored":        aws.BoolValue(volume.FastRestored),
				"snapshot_id":          aws.StringValue(volume.SnapshotId),
				"availability_zone":    aws.StringValue(volume.AvailabilityZone),
				"account_id":           accountID,
				"region":               opts.Region,
				"tags":                 tags,
			}

			// Add attachments
			var attachments []map[string]interface{}
			for _, attachment := range volume.Attachments {
				attachmentDetails := map[string]interface{}{
					"attach_time":           aws.TimeValue(attachment.AttachTime).Format(time.RFC3339),
					"device":                aws.StringValue(attachment.Device),
					"instance_id":           aws.StringValue(attachment.InstanceId),
					"state":                 aws.StringValue(attachment.State),
					"volume_id":             aws.StringValue(attachment.VolumeId),
					"delete_on_termination": aws.BoolValue(attachment.DeleteOnTermination),
				}
				attachments = append(attachments, attachmentDetails)
			}
			if len(attachments) > 0 {
				details["attachments"] = attachments
			}

			// Build cost details
			var costDetails map[string]interface{}
			if costs != nil {
				costDetails = map[string]interface{}{
					"total": costs,
				}
			}

			// Get volume metrics with error handling
			volumeID := aws.StringValue(volume.VolumeId)
			endTime := time.Now().UTC().Truncate(time.Minute)
			daysUnused := awslib.Max(1, opts.DaysUnused)
			metricStartTime := endTime.Add(-time.Duration(daysUnused) * 24 * time.Hour)
			metrics, err := s.getVolumeMetrics(clients.CloudWatch, volumeID, metricStartTime, endTime)
			if err != nil {
				logging.Error("Failed to get volume metrics", err, map[string]interface{}{
					"volume_id": volumeID,
					"startTime": metricStartTime.Format(time.RFC3339),
					"endTime":   endTime.Format(time.RFC3339),
				})
				// Continue processing even if metrics collection fails
			}

			// Build reasons
			var reasons []string
			if aws.StringValue(volume.State) == "available" {
				reasons = append(reasons, fmt.Sprintf("Volume is not attached to any instance for %d days.", daysUnused))
			}

			// Add metrics-based reasons if available
			if metrics != nil {
				if readOps, ok := metrics["ReadOps"]; ok && readOps == 0 {
					reasons = append(reasons, fmt.Sprintf("No read operations in the last %d days.", daysUnused))
				}
				if writeOps, ok := metrics["WriteOps"]; ok && writeOps == 0 {
					reasons = append(reasons, fmt.Sprintf("No write operations in the last %d days.", daysUnused))
				}
			}

			// Create scan result
			result := awslib.ScanResult{
				ResourceType: s.Label(),
				ResourceID:   aws.StringValue(volume.VolumeId),
				ResourceName: resourceName,
				Details:      details,
				Cost:         costDetails,
				Reason:       strings.Join(reasons, "\n"),
			}

			results = append(results, result)

			// Log individual result
			logging.Info("Found unused EBS volume", map[string]interface{}{
				"account_id":    accountID,
				"region":        opts.Region,
				"resource_name": resourceName,
				"resource_id":   aws.StringValue(volume.VolumeId),
				"age_days":      ageInDays,
			})
		}
		return true // Continue pagination
	})

	if err != nil {
		logging.Error("Failed to describe volumes", err, map[string]interface{}{
			"account_id": accountID,
			"region":     opts.Region,
		})
		return nil, fmt.Errorf("failed to describe volumes: %w", err)
	}

	// Log scan completion with metrics
	scanDuration := time.Since(startTime)
	logging.Info("Completed EBS volume scan", map[string]interface{}{
		"account_id":        accountID,
		"region":            opts.Region,
		"total_volumes":     totalVolumes,
		"unused_volumes":    len(results),
		"cost_calculations": costCalculations,
		"duration_ms":       scanDuration.Milliseconds(),
	})

	return results, nil
}

func (s *EBSVolumeScanner) getVolumeMetrics(cwClient *cloudwatch.CloudWatch, volumeID string, startTime time.Time, endTime time.Time) (map[string]float64, error) {
	metrics := make(map[string]float64)
	period := int64(86400) // 1 day
	metricConfigs := []utils.MetricConfig{
		{
			Namespace:     "AWS/EBS",
			ResourceID:    volumeID,
			DimensionName: "VolumeId",
			MetricName:    "VolumeReadOps",
			Statistic:     "Average",
			StartTime:     startTime,
			EndTime:       endTime,
			Period:        period,
		},
		{
			Namespace:     "AWS/EBS",
			ResourceID:    volumeID,
			DimensionName: "VolumeId",
			MetricName:    "VolumeWriteOps",
			Statistic:     "Average",
			StartTime:     startTime,
			EndTime:       endTime,
			Period:        period,
		},
		{
			Namespace:     "AWS/EBS",
			ResourceID:    volumeID,
			DimensionName: "VolumeId",
			MetricName:    "VolumeIdleTime",
			Statistic:     "Average",
			StartTime:     startTime,
			EndTime:       endTime,
			Period:        period,
		},
	}

	for _, config := range metricConfigs {
		value, err := utils.GetResourceMetrics(cwClient, config)
		if err != nil {
			return nil, fmt.Errorf("failed to get metric %s: %w", config.MetricName, err)
		}

		// Map AWS metric names to our internal names
		metricKey := strings.ToLower(strings.TrimPrefix(config.MetricName, "Volume"))
		metrics[metricKey] = value
	}

	return metrics, nil
}
