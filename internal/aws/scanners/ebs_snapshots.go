package scanners

import (
	"fmt"
	"time"

	awslib "cloudsift/internal/aws"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
)

// EBSSnapshotScanner scans for EBS snapshots
type EBSSnapshotScanner struct{}

func init() {
	// Register this scanner with the default registry
	awslib.DefaultRegistry.RegisterScanner(&EBSSnapshotScanner{})
}

// Name implements Scanner interface
func (s *EBSSnapshotScanner) Name() string {
	return "EBS Snapshots"
}

// ArgumentName implements Scanner interface
func (s *EBSSnapshotScanner) ArgumentName() string {
	return "ebs-snapshots"
}

// Label implements Scanner interface
func (s *EBSSnapshotScanner) Label() string {
	return "EBSSnapshots"
}

// Scan implements Scanner interface
func (s *EBSSnapshotScanner) Scan(opts awslib.ScanOptions) (awslib.ScanResults, error) {
	sess, err := awslib.GetSession(opts.Region)
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS session: %w", err)
	}

	// Scan for snapshots
	svc := ec2.New(sess)
	input := &ec2.DescribeSnapshotsInput{
		OwnerIds: []*string{aws.String("self")},
	}

	var results awslib.ScanResults
	err = svc.DescribeSnapshotsPages(input, func(page *ec2.DescribeSnapshotsOutput, lastPage bool) bool {
		for _, snapshot := range page.Snapshots {
			// Convert AWS tags to map
			tags := make(map[string]string)
			for _, tag := range snapshot.Tags {
				tags[aws.StringValue(tag.Key)] = aws.StringValue(tag.Value)
			}

			// Calculate age of snapshot
			age := time.Since(*snapshot.StartTime)
			ageInDays := int(age.Hours() / 24)

			// Only include snapshots older than the threshold
			if ageInDays <= opts.DaysUnused {
				continue
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
				"owner_id":               aws.StringValue(snapshot.OwnerId),
				"progress":               aws.StringValue(snapshot.Progress),
			}

			results = append(results, awslib.ScanResult{
				ResourceType: s.Label(),
				ResourceName: aws.StringValue(snapshot.Description),
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
		return nil, fmt.Errorf("failed to describe snapshots in %s: %w", opts.Region, err)
	}

	return results, nil
}
