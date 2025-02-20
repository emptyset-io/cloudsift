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
	if err := awslib.DefaultRegistry.RegisterScanner(&EBSVolumeScanner{}); err != nil {
		panic(fmt.Sprintf("Failed to register EBS volume scanner: %v", err))
	}
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

	// Create service clients
	clients := utils.CreateServiceClients(sess)
	svc := ec2.New(sess) // Keep direct EC2 client for backward compatibility

	input := &ec2.DescribeVolumesInput{}

	var results awslib.ScanResults
	err = svc.DescribeVolumesPages(input, func(page *ec2.DescribeVolumesOutput, lastPage bool) bool {
		for _, volume := range page.Volumes {
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

			// Calculate costs
			costEstimator := awslib.DefaultCostEstimator

			var costs *awslib.CostBreakdown
			if costEstimator != nil {
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
					continue
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

			// Log that we found a result
			logging.Debug("Found unused EBS volume", map[string]interface{}{
				"account_id":    accountID,
				"region":        opts.Region,
				"resource_name": resourceName,
				"resource_id":   aws.StringValue(volume.VolumeId),
			})

			// Get volume metrics
			volumeID := aws.StringValue(volume.VolumeId)
			endTime := time.Now().UTC().Truncate(time.Minute)
			// Ensure at least 24 hours between start and end time
			daysUnused := awslib.Max(1, opts.DaysUnused)
			startTime := endTime.Add(-time.Duration(daysUnused) * 24 * time.Hour)
			metrics, err := s.getVolumeMetrics(clients.CloudWatch, volumeID, startTime, endTime)
			if err != nil {
				logging.Error("Failed to get volume metrics", err, map[string]interface{}{
					"volume_id": volumeID,
					"startTime": startTime.Format(time.RFC3339),
					"endTime":   endTime.Format(time.RFC3339),
				})
				continue
			}

			var reasons []string
			// Check for detached volumes
			if aws.StringValue(volume.State) == "available" {
				reasons = append(reasons, fmt.Sprintf("Volume is not attached to any instance for %d days.", daysUnused))
			}

			// Check for low I/O activity
			if metrics["read_ops"] == 0 && metrics["write_ops"] == 0 {
				reasons = append(reasons, fmt.Sprintf("No read/write operations in the last %d days.", daysUnused))
			} else if metrics["read_ops"] < 1 && metrics["write_ops"] < 1 {
				reasons = append(reasons, fmt.Sprintf("Very low I/O activity (reads: %.2f/hour, writes: %.2f/hour) in the last %d days.",
					metrics["read_ops"], metrics["write_ops"], daysUnused))
			}

			// Check for low utilization
			if metrics["idle_time"] > 90 {
				reasons = append(reasons, fmt.Sprintf("Volume is idle %.2f%% of the time in the last %d days.",
					metrics["idle_time"], daysUnused))
			}

			if len(reasons) > 0 {
				// Create details map with specific key format for ScanResult
				details := map[string]interface{}{
					"State":      aws.StringValue(volume.State),
					"VolumeType": aws.StringValue(volume.VolumeType),
					"Size":       aws.Int64Value(volume.Size),
					"IOPS":       aws.Int64Value(volume.Iops),
					"AccountId":  accountID,
					"Region":     opts.Region,
				}

				results = append(results, awslib.ScanResult{
					ResourceType: s.Label(),
					ResourceName: resourceName,
					ResourceID:   volumeID,
					Reason:       strings.Join(reasons, "\n"),
					Tags:         tags,
					Details:      details,
					Cost:         costDetails,
				})
			}
		}
		return true
	})

	if err != nil {
		logging.Error("Failed to describe volumes", err, map[string]interface{}{
			"account_id": accountID,
			"region":     opts.Region,
		})
		return nil, fmt.Errorf("failed to describe volumes in %s: %w", opts.Region, err)
	}

	return results, nil
}

func (s *EBSVolumeScanner) getVolumeMetrics(cwClient *cloudwatch.CloudWatch, volumeID string, startTime time.Time, endTime time.Time) (map[string]float64, error) {
	metrics := make(map[string]float64)
	metricConfigs := []utils.MetricConfig{
		{
			Namespace:     "AWS/EBS",
			ResourceID:    volumeID,
			DimensionName: "VolumeId",
			MetricName:    "VolumeReadOps",
			Statistic:     "Average",
			StartTime:     startTime,
			EndTime:       endTime,
		},
		{
			Namespace:     "AWS/EBS",
			ResourceID:    volumeID,
			DimensionName: "VolumeId",
			MetricName:    "VolumeWriteOps",
			Statistic:     "Average",
			StartTime:     startTime,
			EndTime:       endTime,
		},
		{
			Namespace:     "AWS/EBS",
			ResourceID:    volumeID,
			DimensionName: "VolumeId",
			MetricName:    "VolumeIdleTime",
			Statistic:     "Average",
			StartTime:     startTime,
			EndTime:       endTime,
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
