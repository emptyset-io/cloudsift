package scanners

import (
	"fmt"
	"time"

	awslib "cloudsift/internal/aws"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
)

// EBSVolumeScanner scans for unused EBS volumes
type EBSVolumeScanner struct{}

func init() {
	// Register this scanner with the default registry
	awslib.DefaultRegistry.RegisterScanner(&EBSVolumeScanner{})
}

// Name implements Scanner interface
func (s *EBSVolumeScanner) Name() string {
	return "EBS Volumes"
}

// ArgumentName implements Scanner interface
func (s *EBSVolumeScanner) ArgumentName() string {
	return "ebs-volumes"
}

// Label implements Scanner interface
func (s *EBSVolumeScanner) Label() string {
	return "EBSVolumes"
}

// Scan implements Scanner interface
func (s *EBSVolumeScanner) Scan(opts awslib.ScanOptions) (awslib.ScanResults, error) {
	sess, err := awslib.GetSession(opts.Region)
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS session: %w", err)
	}

	// Scan for volumes
	svc := ec2.New(sess)
	input := &ec2.DescribeVolumesInput{}

	var results awslib.ScanResults
	err = svc.DescribeVolumesPages(input, func(page *ec2.DescribeVolumesOutput, lastPage bool) bool {
		for _, volume := range page.Volumes {
			// Calculate age of volume
			age := time.Since(*volume.CreateTime)
			ageInDays := int(age.Hours() / 24)

			// Skip if volume is attached or not old enough
			if len(volume.Attachments) > 0 || ageInDays < opts.DaysUnused {
				continue
			}

			// Convert AWS tags to map
			tags := make(map[string]string)
			for _, tag := range volume.Tags {
				tags[aws.StringValue(tag.Key)] = aws.StringValue(tag.Value)
			}

			// Get volume name from tags or use volume ID
			volumeName := aws.StringValue(volume.VolumeId)
			if name, ok := tags["Name"]; ok {
				volumeName = name
			}

			// Calculate costs
			costEstimator, err := awslib.NewCostEstimator("cache/costs.json")
			if err != nil {
				// Log error but continue without costs
				fmt.Printf("Failed to create cost estimator: %v\n", err)
			}

			var costs *awslib.CostBreakdown
			if costEstimator != nil {
				costs, err = costEstimator.CalculateCost(awslib.ResourceCostConfig{
					ResourceType:  "EBSVolumes",
					ResourceSize:  aws.Int64Value(volume.Size),
					Region:       opts.Region,
					CreationTime: *volume.CreateTime,
				})
				if err != nil {
					fmt.Printf("Failed to calculate costs for volume %s: %v\n", volumeName, err)
				}
			}

			// Collect all relevant details
			details := map[string]interface{}{
				"volume_id":    aws.StringValue(volume.VolumeId),
				"state":        aws.StringValue(volume.State),
				"create_time":  volume.CreateTime.Format("2006-01-02T15:04:05Z07:00"),
				"age_days":     ageInDays,
				"size":         aws.Int64Value(volume.Size),
				"volume_type":  aws.StringValue(volume.VolumeType),
				"encrypted":    aws.BoolValue(volume.Encrypted),
				"iops":         aws.Int64Value(volume.Iops),
				"kms_key_id":   aws.StringValue(volume.KmsKeyId),
				"snapshot_id":  aws.StringValue(volume.SnapshotId),
				"availability_zone": aws.StringValue(volume.AvailabilityZone),
				"multi_attach_enabled": aws.BoolValue(volume.MultiAttachEnabled),
			}

			if costs != nil {
				details["costs"] = costs
			}

			results = append(results, awslib.ScanResult{
				ResourceType: s.Label(),
				ResourceName: volumeName,
				ResourceID:   aws.StringValue(volume.VolumeId),
				Reason: fmt.Sprintf("%dGB volume created on %s, unused for %d days",
					aws.Int64Value(volume.Size),
					volume.CreateTime.Format("2006-01-02"),
					ageInDays),
				Tags:    tags,
				Details: details,
			})
		}
		return !lastPage
	})

	if err != nil {
		return nil, fmt.Errorf("failed to describe volumes in %s: %w", opts.Region, err)
	}

	return results, nil
}
