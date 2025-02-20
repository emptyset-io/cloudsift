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

// EBSVolumeScanner scans for EBS volumes
type EBSVolumeScanner struct{}

func init() {
	// Register this scanner with the default registry
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

	// Create EC2 service client
	svc := ec2.New(sess)
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
			costEstimator, err := awslib.NewCostEstimator("cache/costs.json")
			if err != nil {
				logging.Error("Failed to create cost estimator", err, nil)
			}

			var costs *awslib.CostBreakdown
			if costEstimator != nil {
				volumeSize := aws.Int64Value(volume.Size)
				volumeType := aws.StringValue(volume.VolumeType)
				hoursRunning := time.Since(*volume.CreateTime).Hours()

				costs, err = costEstimator.CalculateCost(awslib.ResourceCostConfig{
					ResourceType:  "EBSVolumes",
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
				}

				// Calculate lifetime cost
				if costs != nil {
					lifetime := roundCost(costs.HourlyRate * hoursRunning)
					costs.Lifetime = &lifetime
				}
			}

			// Collect all relevant details
			details := map[string]interface{}{
				"volume_id":           aws.StringValue(volume.VolumeId),
				"size":               aws.Int64Value(volume.Size),
				"state":              aws.StringValue(volume.State),
				"volume_type":        aws.StringValue(volume.VolumeType),
				"iops":               aws.Int64Value(volume.Iops),
				"throughput":         aws.Int64Value(volume.Throughput),
				"encrypted":          aws.BoolValue(volume.Encrypted),
				"kms_key_id":         aws.StringValue(volume.KmsKeyId),
				"outpost_arn":        aws.StringValue(volume.OutpostArn),
				"create_time":        volume.CreateTime.Format(time.RFC3339),
				"hours_running":      time.Since(*volume.CreateTime).Hours(),
				"multi_attach_enabled": aws.BoolValue(volume.MultiAttachEnabled),
				"fast_restored":       aws.BoolValue(volume.FastRestored),
				"snapshot_id":         aws.StringValue(volume.SnapshotId),
				"availability_zone":   aws.StringValue(volume.AvailabilityZone),
			}

			// Add attachments
			var attachments []map[string]interface{}
			for _, attachment := range volume.Attachments {
				attachmentDetails := map[string]interface{}{
					"attach_time":    aws.TimeValue(attachment.AttachTime).Format(time.RFC3339),
					"device":         aws.StringValue(attachment.Device),
					"instance_id":    aws.StringValue(attachment.InstanceId),
					"state":          aws.StringValue(attachment.State),
					"volume_id":      aws.StringValue(attachment.VolumeId),
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

			results = append(results, awslib.ScanResult{
				ResourceType: s.Label(),
				ResourceName: resourceName,
				ResourceID:   aws.StringValue(volume.VolumeId),
				Reason: fmt.Sprintf("%dGB volume, unused for %d days",
					aws.Int64Value(volume.Size),
					ageInDays),
				Tags:    tags,
				Details: details,
				Cost:    costDetails,
			})
		}
		return !lastPage
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
