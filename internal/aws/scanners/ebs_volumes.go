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

			// Check current attachment status and history
			isCurrentlyAttached := len(volume.Attachments) > 0
			var lastAttachTime *time.Time
			var lastDetachTime *time.Time
			hasAttachmentHistory := false

			// Get last attach time from current attachments
			for _, attachment := range volume.Attachments {
				if attachment.AttachTime != nil {
					hasAttachmentHistory = true
					if lastAttachTime == nil || attachment.AttachTime.After(*lastAttachTime) {
						lastAttachTime = attachment.AttachTime
					}
				}
			}

			// Get volume status history
			statusInput := &ec2.DescribeVolumeStatusInput{
				VolumeIds: []*string{volume.VolumeId},
				Filters: []*ec2.Filter{
					{
						Name:   aws.String("event-type"),
						Values: []*string{aws.String("attaching"), aws.String("detaching")},
					},
				},
			}
			statusResp, err := svc.DescribeVolumeStatus(statusInput)
			if err == nil && len(statusResp.VolumeStatuses) > 0 {
				status := statusResp.VolumeStatuses[0]
				if status.Events != nil {
					for _, event := range status.Events {
						eventType := aws.StringValue(event.EventType)
						if eventType == "attaching" && event.NotBefore != nil {
							hasAttachmentHistory = true
							if lastAttachTime == nil || event.NotBefore.After(*lastAttachTime) {
								lastAttachTime = event.NotBefore
							}
						} else if eventType == "detaching" && event.NotAfter != nil {
							hasAttachmentHistory = true
							if lastDetachTime == nil || event.NotAfter.After(*lastDetachTime) {
								lastDetachTime = event.NotAfter
							}
						}
					}
				}
			}

			// Skip if currently attached
			if isCurrentlyAttached {
				continue
			}

			// If we have no attachment history and volume is old, it's likely never been attached
			// Otherwise use the last detach time to determine unused period
			var lastUsedTime *time.Time
			if !hasAttachmentHistory {
				// For volumes that have never been attached, use creation time
				lastUsedTime = volume.CreateTime
			} else if lastDetachTime != nil {
				lastUsedTime = lastDetachTime
			} else {
				// If we have attachment history but no detach time, something's wrong
				// Be conservative and skip this volume
				continue
			}

			unusedDuration := time.Since(*lastUsedTime)
			unusedDays := int(unusedDuration.Hours() / 24)
			if unusedDays < opts.DaysUnused {
				continue
			}

			ageString := awslib.FormatTimeDifference(time.Now(), lastUsedTime)

			// Convert AWS tags to map
			tags := make(map[string]string)
			for _, tag := range volume.Tags {
				tags[aws.StringValue(tag.Key)] = aws.StringValue(tag.Value)
			}

			details := map[string]interface{}{
				// Resource identifiers
				"account_id":    accountID,
				"region":        opts.Region,
				"volume_id":     aws.StringValue(volume.VolumeId),
				"snapshot_id":   aws.StringValue(volume.SnapshotId),
				"tags":         tags,
				"reason":       fmt.Sprintf("Volume has not been used in %s", ageString),
				// Volume configuration
				"volume_type":          aws.StringValue(volume.VolumeType),
				"size_gb":             aws.Int64Value(volume.Size),
				"iops":                aws.Int64Value(volume.Iops),
				"throughput":          aws.Int64Value(volume.Throughput),
				"encrypted":           aws.BoolValue(volume.Encrypted),
				"kms_key_id":          aws.StringValue(volume.KmsKeyId),
				"multi_attach_enabled": aws.BoolValue(volume.MultiAttachEnabled),

				// Location info
				"availability_zone": aws.StringValue(volume.AvailabilityZone),
				"outpost_arn":      aws.StringValue(volume.OutpostArn),

				// Status and timing
				"state":              aws.StringValue(volume.State),
				"created":            volume.CreateTime.Format(time.RFC3339),
				"age_days":           unusedDays,
				"attachment_history": map[string]interface{}{
					"currently_attached": isCurrentlyAttached,
					"has_history":       hasAttachmentHistory,
				},
				"fast_restored": aws.BoolValue(volume.FastRestored),
			}

			// Add status details if available
			if len(statusResp.VolumeStatuses) > 0 {
				status := statusResp.VolumeStatuses[0]
				volumeStatus := map[string]interface{}{
					"status":                aws.StringValue(status.VolumeStatus.Status),
					"details":               status.VolumeStatus.Details,
					"availability_zone":     aws.StringValue(status.AvailabilityZone),
				}

				if status.Events != nil {
					var events []map[string]interface{}
					for _, event := range status.Events {
						eventMap := map[string]interface{}{
							"event_type":    aws.StringValue(event.EventType),
							"description":   aws.StringValue(event.Description),
							"event_id":      aws.StringValue(event.EventId),
						}
						if event.NotBefore != nil {
							eventMap["not_before"] = event.NotBefore.Format(time.RFC3339)
						}
						if event.NotAfter != nil {
							eventMap["not_after"] = event.NotAfter.Format(time.RFC3339)
						}
						events = append(events, eventMap)
					}
					volumeStatus["events"] = events
				}

				if status.Actions != nil {
					var actions []map[string]interface{}
					for _, action := range status.Actions {
						actionMap := map[string]interface{}{
							"code":        aws.StringValue(action.Code),
							"description": aws.StringValue(action.Description),
							"event_type":  aws.StringValue(action.EventType),
							"event_id":    aws.StringValue(action.EventId),
						}
						actions = append(actions, actionMap)
					}
					volumeStatus["actions"] = actions
				}

				details["volume_status"] = volumeStatus
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

			// Check if volume is truly unused based on all criteria
			isUnused := true
			var unusedReasons []string

			// Add attachment status to reasons
			unusedReasons = append(unusedReasons, fmt.Sprintf("Volume has not been used in %s", ageString))

			// Check metrics for activity with thresholds
			const minActivityThreshold = 1.0 // Minimum ops/day to consider active
			if metrics != nil {
				if readOps, ok := metrics["ReadOps"]; ok {
					avgReadOpsPerDay := readOps / float64(daysUnused)
					if avgReadOpsPerDay >= minActivityThreshold {
						isUnused = false
					} else {
						unusedReasons = append(unusedReasons, fmt.Sprintf("Very low read activity (%.2f ops/day) in the last %d days.",
							avgReadOpsPerDay, daysUnused))
					}
				}
				if writeOps, ok := metrics["WriteOps"]; ok {
					avgWriteOpsPerDay := writeOps / float64(daysUnused)
					if avgWriteOpsPerDay >= minActivityThreshold {
						isUnused = false
					} else {
						unusedReasons = append(unusedReasons, fmt.Sprintf("Very low write activity (%.2f ops/day) in the last %d days.",
							avgWriteOpsPerDay, daysUnused))
					}
				}
				if idleTime, ok := metrics["IdleTime"]; ok {
					if idleTime < 95.0 { // Less than 95% idle means active
						isUnused = false
					} else {
						unusedReasons = append(unusedReasons, fmt.Sprintf("Volume has been idle %.1f%% of the time in the last %d days.",
							idleTime, daysUnused))
					}
				}
			}

			// Skip if volume is not unused
			if !isUnused {
				continue
			}

			// Get resource name from tags or use volume ID
			resourceName := aws.StringValue(volume.VolumeId)
			if name, ok := tags["Name"]; ok {
				resourceName = name
			}

			// Calculate costs only for unused volumes
			var costs *awslib.CostBreakdown
			var costDetails map[string]interface{}
			costEstimator := awslib.DefaultCostEstimator
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
					costDetails = map[string]interface{}{
						"total": costs,
					}
				}
			}

			// Collect all relevant details
			attachmentHistory := map[string]interface{}{
				"currently_attached": isCurrentlyAttached,
				"has_history":       hasAttachmentHistory,
			}
			if lastAttachTime != nil {
				attachmentHistory["last_attach_time"] = lastAttachTime.Format(time.RFC3339)
			}
			if lastDetachTime != nil {
				attachmentHistory["last_detach_time"] = lastDetachTime.Format(time.RFC3339)
			}
			attachmentHistory["days_unused"] = unusedDays

			// Get current attachments info if any
			var attachments []map[string]interface{}
			for _, att := range volume.Attachments {
				attachment := map[string]interface{}{
					"instance_id":  aws.StringValue(att.InstanceId),
					"device":       aws.StringValue(att.Device),
					"state":        aws.StringValue(att.State),
					"attach_time":  att.AttachTime.Format(time.RFC3339),
					"delete_on_termination": aws.BoolValue(att.DeleteOnTermination),
				}
				attachments = append(attachments, attachment)
			}
			if len(attachments) > 0 {
				attachmentHistory["current_attachments"] = attachments
			}

			details["attachment_history"] = attachmentHistory

			// Build reasons
			result := awslib.ScanResult{
				ResourceType: s.Label(),
				ResourceID:   aws.StringValue(volume.VolumeId),
				ResourceName: resourceName,
				Details:      details,
				Cost:         costDetails,
				Reason:       strings.Join(unusedReasons, "\n"),
			}

			results = append(results, result)

			// Log individual result
			logging.Info("Found unused EBS volume", map[string]interface{}{
				"account_id":    accountID,
				"region":        opts.Region,
				"resource_name": resourceName,
				"resource_id":   aws.StringValue(volume.VolumeId),
				"age_days":      unusedDays,
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
