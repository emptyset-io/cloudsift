package scanners

import (
	"fmt"
	"time"

	awslib "cloudsift/internal/aws"
	"cloudsift/internal/aws/utils"
	"cloudsift/internal/logging"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
)

// EBSSnapshotScanner scans for EBS snapshots
type EBSSnapshotScanner struct{}

func init() {
	if err := awslib.DefaultRegistry.RegisterScanner(&EBSSnapshotScanner{}); err != nil {
		panic(fmt.Sprintf("Failed to register EBS snapshot scanner: %v", err))
	}
}

// Name implements Scanner interface
func (s *EBSSnapshotScanner) Name() string {
	return "ebs-snapshots"
}

// ArgumentName implements Scanner interface
func (s *EBSSnapshotScanner) ArgumentName() string {
	return "ebs-snapshots"
}

// Label implements Scanner interface
func (s *EBSSnapshotScanner) Label() string {
	return "EBS Snapshots"
}

// Scan implements Scanner interface
func (s *EBSSnapshotScanner) Scan(opts awslib.ScanOptions) (awslib.ScanResults, error) {
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
	svc := ec2.New(sess)
	input := &ec2.DescribeSnapshotsInput{
		OwnerIds: []*string{aws.String("self")},
	}

	var results awslib.ScanResults
	volumeSnapshots := make(map[string][]string)

	err = svc.DescribeSnapshotsPages(input, func(page *ec2.DescribeSnapshotsOutput, lastPage bool) bool {
		for _, snapshot := range page.Snapshots {
			// Calculate age of snapshot
			age := time.Since(*snapshot.StartTime)
			ageInDays := int(age.Hours() / 24)

			// Skip if snapshot is not old enough
			if ageInDays < opts.DaysUnused {
				continue
			}

			// Convert AWS tags to map
			tags := make(map[string]string)
			for _, tag := range snapshot.Tags {
				tags[aws.StringValue(tag.Key)] = aws.StringValue(tag.Value)
			}

			// Get resource name from tags or use description/snapshot ID
			resourceName := aws.StringValue(snapshot.Description)
			if resourceName == "" {
				resourceName = aws.StringValue(snapshot.SnapshotId)
			}
			if name, ok := tags["Name"]; ok {
				resourceName = name
			}

			// Get volume information to determine volume type
			var volumeType string
			if aws.StringValue(snapshot.VolumeId) != "" {
				volumeInput := &ec2.DescribeVolumesInput{
					VolumeIds: []*string{snapshot.VolumeId},
				}
				volumeOutput, err := svc.DescribeVolumes(volumeInput)
				if err == nil && len(volumeOutput.Volumes) > 0 {
					volumeType = aws.StringValue(volumeOutput.Volumes[0].VolumeType)
				}
			}
			if volumeType == "" {
				volumeType = "gp2" // Default to gp2 if we can't determine the volume type
			}

			// Calculate costs
			costEstimator := awslib.DefaultCostEstimator

			var costs *awslib.CostBreakdown
			if costEstimator != nil {
				snapshotSize := aws.Int64Value(snapshot.VolumeSize)
				hoursRunning := time.Since(*snapshot.StartTime).Hours()

				costs, err = costEstimator.CalculateCost(awslib.ResourceCostConfig{
					ResourceType: "EBSSnapshots",
					ResourceSize: snapshotSize,
					Region:       opts.Region,
					CreationTime: *snapshot.StartTime,
					VolumeType:   volumeType,
				})
				if err != nil {
					logging.Error("Failed to calculate costs", err, map[string]interface{}{
						"account_id":    accountID,
						"region":        opts.Region,
						"resource_name": resourceName,
						"resource_id":   aws.StringValue(snapshot.SnapshotId),
					})
				}

				// Calculate lifetime cost
				if costs != nil {
					lifetime := roundCost(costs.HourlyRate * hoursRunning)
					costs.Lifetime = &lifetime
				}
			}

			// Collect all relevant details
			details := map[string]interface{}{
				"snapshot_id":            aws.StringValue(snapshot.SnapshotId),
				"volume_id":              aws.StringValue(snapshot.VolumeId),
				"state":                  aws.StringValue(snapshot.State),
				"state_message":          aws.StringValue(snapshot.StateMessage),
				"start_time":             snapshot.StartTime.Format(time.RFC3339),
				"progress":               aws.StringValue(snapshot.Progress),
				"owner_id":               aws.StringValue(snapshot.OwnerId),
				"description":            aws.StringValue(snapshot.Description),
				"volume_size":            aws.Int64Value(snapshot.VolumeSize),
				"owner_alias":            aws.StringValue(snapshot.OwnerAlias),
				"encrypted":              aws.BoolValue(snapshot.Encrypted),
				"kms_key_id":             aws.StringValue(snapshot.KmsKeyId),
				"data_encryption_key_id": aws.StringValue(snapshot.DataEncryptionKeyId),
				"hours_running":          time.Since(*snapshot.StartTime).Hours(),
				"volume_type":            volumeType,
				"account_id":             accountID,
				"region":                 opts.Region,
			}

			// Add storage tier info if available
			if snapshot.StorageTier != nil {
				details["storage_tier"] = map[string]interface{}{
					"tier": aws.StringValue(snapshot.StorageTier),
				}
			}

			// Build cost details
			var costDetails map[string]interface{}
			if costs != nil {
				costDetails = map[string]interface{}{
					"total": costs,
				}
			}

			// Log that we found a result
			logging.Debug("Found unused EBS snapshot", map[string]interface{}{
				"account_id":    accountID,
				"region":        opts.Region,
				"resource_name": resourceName,
				"resource_id":   aws.StringValue(snapshot.SnapshotId),
			})

			reasons := []string{}
			// Check for old snapshots
			if age.Hours()/24 > float64(opts.DaysUnused) {
				reasons = append(reasons, fmt.Sprintf("Snapshot is older than %d days (age: %.0f days).",
					opts.DaysUnused, age.Hours()/24))
			}

			// Check for snapshots of deleted volumes
			if aws.StringValue(snapshot.VolumeId) == "" {
				reasons = append(reasons, fmt.Sprintf("Source volume was deleted. Snapshot has not been used in %d days.",
					opts.DaysUnused))
			}

			// Check for multiple snapshots of the same volume
			if aws.StringValue(snapshot.VolumeId) != "" {
				volumeSnapshots[aws.StringValue(snapshot.VolumeId)] = append(volumeSnapshots[aws.StringValue(snapshot.VolumeId)], aws.StringValue(snapshot.SnapshotId))
				if len(volumeSnapshots[aws.StringValue(snapshot.VolumeId)]) > 1 {
					reasons = append(reasons, fmt.Sprintf("Multiple snapshots exist for volume %s. This snapshot has not been used in %d days.",
						aws.StringValue(snapshot.VolumeId), opts.DaysUnused))
				}
			}

			if len(reasons) > 0 {
				results = append(results, awslib.ScanResult{
					ResourceType: s.Label(),
					ResourceName: resourceName,
					ResourceID:   aws.StringValue(snapshot.SnapshotId),
					Reason:      reasons[0],
					Tags:        tags,
					Details:     details,
					Cost:        costDetails,
				})
			}
		}
		return true
	})

	if err != nil {
		logging.Error("Failed to describe snapshots", err, nil)
		return nil, fmt.Errorf("failed to describe snapshots: %w", err)
	}

	return results, nil
}
