package scanners

import (
	"fmt"
	"time"

	awslib "cloudsift/internal/aws"
	"cloudsift/internal/logging"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/sts"
)

// EBSSnapshotScanner scans for EBS snapshots
type EBSSnapshotScanner struct{}

func init() {
	// Register this scanner with the default registry
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
	sess, err := awslib.GetSession(opts.Region)
	if err != nil {
		logging.Error("Failed to create AWS session", err, map[string]interface{}{
			"region": opts.Region,
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

	// Scan for snapshots
	svc := ec2.New(sess)
	input := &ec2.DescribeSnapshotsInput{
		OwnerIds: []*string{aws.String("self")},
	}

	var results awslib.ScanResults
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

			// Calculate costs
			costEstimator, err := awslib.NewCostEstimator("cache/costs.json")
			if err != nil {
				logging.Error("Failed to create cost estimator", err, map[string]interface{}{
					"account_id":    accountID,
					"region":        opts.Region,
					"resource_name": resourceName,
					"resource_id":   aws.StringValue(snapshot.SnapshotId),
				})
			}

			var costs *awslib.CostBreakdown
			if costEstimator != nil {
				costs, err = costEstimator.CalculateCost(awslib.ResourceCostConfig{
					ResourceType: "EBSSnapshots",
					ResourceSize: aws.Int64Value(snapshot.VolumeSize),
					Region:       opts.Region,
					CreationTime: *snapshot.StartTime,
				})
				if err != nil {
					logging.Error("Failed to calculate costs", err, map[string]interface{}{
						"account_id":    accountID,
						"region":        opts.Region,
						"resource_name": resourceName,
						"resource_id":   aws.StringValue(snapshot.SnapshotId),
						"resource_type": "EBSSnapshots",
					})
				}
			}

			// Collect all relevant details
			details := map[string]interface{}{
				"snapshot_id":            aws.StringValue(snapshot.SnapshotId),
				"volume_id":              aws.StringValue(snapshot.VolumeId),
				"state":                  aws.StringValue(snapshot.State),
				"start_time":             snapshot.StartTime.Format("2006-01-02T15:04:05Z07:00"),
				"age_days":               ageInDays,
				"volume_size":            aws.Int64Value(snapshot.VolumeSize),
				"encrypted":              aws.BoolValue(snapshot.Encrypted),
				"description":            aws.StringValue(snapshot.Description),
				"kms_key_id":             aws.StringValue(snapshot.KmsKeyId),
				"data_encryption_key_id": aws.StringValue(snapshot.DataEncryptionKeyId),
				"progress":               aws.StringValue(snapshot.Progress),
			}

			if costs != nil {
				details["costs"] = costs
			}

			// Log that we found a result
			logging.Debug("Found unused EBS snapshot", map[string]interface{}{
				"account_id":    accountID,
				"region":        opts.Region,
				"resource_name": resourceName,
				"resource_id":   aws.StringValue(snapshot.SnapshotId),
			})

			results = append(results, awslib.ScanResult{
				ResourceType: s.Label(),
				ResourceName: resourceName,
				ResourceID:   aws.StringValue(snapshot.SnapshotId),
				Reason: fmt.Sprintf("%dGB snapshot from %s, unused for %d days",
					aws.Int64Value(snapshot.VolumeSize),
					snapshot.StartTime.Format("2006-01-02"),
					ageInDays),
				Tags:    tags,
				Details: details,
			})
		}
		return !lastPage
	})

	if err != nil {
		logging.Error("Failed to describe snapshots", err, map[string]interface{}{
			"account_id": accountID,
			"region":     opts.Region,
		})
		return nil, fmt.Errorf("failed to describe snapshots in %s: %w", opts.Region, err)
	}

	return results, nil
}
