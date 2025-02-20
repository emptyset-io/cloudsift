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
	return "ebs-volumes"
}

// Scan implements Scanner interface
func (s *EBSVolumeScanner) Scan(opts awslib.ScanOptions) (awslib.ScanResults, error) {
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

	// Scan for volumes
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
				logging.Error("Failed to create cost estimator", err, map[string]interface{}{
					"account_id":    accountID,
					"region":        opts.Region,
					"resource_name": resourceName,
					"resource_id":   aws.StringValue(volume.VolumeId),
				})
			}

			var costs *awslib.CostBreakdown
			if costEstimator != nil {
				costs, err = costEstimator.CalculateCost(awslib.ResourceCostConfig{
					ResourceType: "EBSVolumes",
					ResourceSize: aws.Int64Value(volume.Size),
					Region:       opts.Region,
					CreationTime: *volume.CreateTime,
				})
				if err != nil {
					logging.Error("Failed to calculate costs", err, map[string]interface{}{
						"account_id":    accountID,
						"region":        opts.Region,
						"resource_name": resourceName,
						"resource_id":   aws.StringValue(volume.VolumeId),
						"resource_type": "EBSVolumes",
					})
				}
			}

			// Collect all relevant details
			details := map[string]interface{}{
				"volume_id":   aws.StringValue(volume.VolumeId),
				"state":       aws.StringValue(volume.State),
				"create_time": volume.CreateTime.Format("2006-01-02T15:04:05Z07:00"),
				"age_days":    ageInDays,
				"size":        aws.Int64Value(volume.Size),
				"encrypted":   aws.BoolValue(volume.Encrypted),
				"type":        aws.StringValue(volume.VolumeType),
				"iops":        aws.Int64Value(volume.Iops),
				"kms_key_id":  aws.StringValue(volume.KmsKeyId),
			}

			if costs != nil {
				details["costs"] = costs
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
