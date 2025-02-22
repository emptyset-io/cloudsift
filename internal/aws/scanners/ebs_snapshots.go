package scanners

import (
	"context"
	"fmt"
	"sync"
	"time"

	awslib "cloudsift/internal/aws"
	"cloudsift/internal/aws/utils"
	"cloudsift/internal/logging"
	"cloudsift/internal/worker"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
)

// EBSSnapshotScanner scans for EBS snapshots
type EBSSnapshotScanner struct{}

func init() {
	awslib.DefaultRegistry.RegisterScanner(&EBSSnapshotScanner{})
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

	// Create EC2 service client
	svc := ec2.New(sess)
	input := &ec2.DescribeSnapshotsInput{
		OwnerIds: []*string{aws.String("self")},
	}

	var results awslib.ScanResults
	var resultsMutex sync.Mutex
	volumeSnapshots := make(map[string][]string)
	var volumeSnapshotsMutex sync.Mutex

	// Get the shared worker pool
	pool := worker.GetSharedPool()

	// Process snapshots in parallel
	var wg sync.WaitGroup
	err = svc.DescribeSnapshotsPages(input, func(page *ec2.DescribeSnapshotsOutput, lastPage bool) bool {
		for _, snapshot := range page.Snapshots {
			snapshot := snapshot // Create local copy for closure
			wg.Add(1)
			
			pool.Submit(func(ctx context.Context) error {
				defer wg.Done()

				// Calculate age of snapshot
				age := time.Since(*snapshot.StartTime)
				ageInDays := int(age.Hours() / 24)

				// Skip if snapshot is not old enough
				if ageInDays < opts.DaysUnused {
					return nil
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
					volumeSnapshotsMutex.Lock()
					volumeSnapshots[aws.StringValue(snapshot.VolumeId)] = append(volumeSnapshots[aws.StringValue(snapshot.VolumeId)], aws.StringValue(snapshot.SnapshotId))
					snapshotCount := len(volumeSnapshots[aws.StringValue(snapshot.VolumeId)])
					volumeSnapshotsMutex.Unlock()

					if snapshotCount > 1 {
						reasons = append(reasons, fmt.Sprintf("Multiple snapshots exist for volume %s. This snapshot has not been used in %d days.",
							aws.StringValue(snapshot.VolumeId), opts.DaysUnused))
					}
				}

				// Thread-safe append to results
				resultsMutex.Lock()
				results = append(results, awslib.ScanResult{
					ResourceType: s.Label(),
					ResourceName: resourceName,
					ResourceID:   aws.StringValue(snapshot.SnapshotId),
					Reason:      reasons[0],
					Tags:        tags,
					Details:     details,
					Cost:        costDetails,
				})
				resultsMutex.Unlock()

				return nil
			})
		}
		return true
	})

	if err != nil {
		return nil, fmt.Errorf("error describing snapshots: %w", err)
	}

	// Wait for all tasks to complete
	wg.Wait()

	return results, nil
}
