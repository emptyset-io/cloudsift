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
	"github.com/aws/aws-sdk-go/service/s3"
)

// S3Scanner scans for unused S3 buckets
type S3Scanner struct{}

func init() {
	if err := awslib.DefaultRegistry.RegisterScanner(&S3Scanner{}); err != nil {
		panic(fmt.Sprintf("Failed to register S3 scanner: %v", err))
	}
}

// Name implements Scanner interface
func (s *S3Scanner) Name() string {
	return "s3"
}

// ArgumentName implements Scanner interface
func (s *S3Scanner) ArgumentName() string {
	return "s3"
}

// Label implements Scanner interface
func (s *S3Scanner) Label() string {
	return "S3 Buckets"
}

// getBucketObjectCount gets the current number of objects in an S3 bucket
func (s *S3Scanner) getBucketObjectCount(s3Client *s3.S3, bucketName string) int64 {
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
	}

	output, err := s3Client.ListObjectsV2(input)
	if err != nil {
		logging.Error("Failed to list objects", err, map[string]interface{}{
			"bucket": bucketName,
		})
		return 0
	}

	return aws.Int64Value(output.KeyCount)
}

// getBucketMetrics retrieves CloudWatch metrics for an S3 bucket
func (s *S3Scanner) getBucketMetrics(cwClient *cloudwatch.CloudWatch, bucketName string, startTime, endTime time.Time) (map[string]float64, error) {
	metricsMap := map[string]string{
		"object_count":    "NumberOfObjects",
		"bucket_size":     "BucketSizeBytes",
		"get_requests":    "GetRequests",
		"put_requests":    "PutRequests",
		"delete_requests": "DeleteRequests",
	}

	results := make(map[string]float64)
	for key, metricName := range metricsMap {
		config := utils.MetricConfig{
			Namespace:     "AWS/S3",
			ResourceID:    bucketName,
			DimensionName: "BucketName",
			MetricName:    metricName,
			Statistic:     "Average",
			StartTime:     startTime.UTC(),
			EndTime:       endTime.UTC(),
		}

		value, err := utils.GetResourceMetrics(cwClient, config)
		if err != nil {
			return nil, fmt.Errorf("failed to get metric %s: %w", metricName, err)
		}

		results[key] = value
	}

	return results, nil
}

// determineUnusedReasons checks if a bucket is unused
func (s *S3Scanner) determineUnusedReasons(currentObjectCount int64, metrics map[string]float64, daysUnused int) []string {
	var reasons []string

	if currentObjectCount == 0 {
		reasons = append(reasons, "Bucket is empty.")
	}

	if metrics["get_requests"] == 0 && metrics["put_requests"] == 0 && metrics["delete_requests"] == 0 {
		if currentObjectCount > 0 {
			reasons = append(reasons, fmt.Sprintf("Bucket has objects but no activity in the last %d days.", daysUnused))
		} else {
			reasons = append(reasons, fmt.Sprintf("Empty bucket with no activity in the last %d days.", daysUnused))
		}
	}

	if metrics["put_requests"] == 0 && metrics["delete_requests"] == 0 && currentObjectCount > 0 {
		if metrics["get_requests"] == 0 {
			reasons = append(reasons, fmt.Sprintf("Static content with no access in the last %d days.", daysUnused))
		} else if metrics["get_requests"] < 1 {
			reasons = append(reasons, fmt.Sprintf("Static content with very low access (%.2f requests/hour) in the last %d days.",
				metrics["get_requests"], daysUnused))
		}
	}

	return reasons
}

// Scan implements Scanner interface
func (s *S3Scanner) Scan(opts awslib.ScanOptions) (awslib.ScanResults, error) {
	sess, err := awslib.GetSession(opts.Role, opts.Region)
	if err != nil {
		logging.Error("Failed to create AWS session", err, map[string]interface{}{
			"region": opts.Region,
			"role":   opts.Role,
		})
		return nil, fmt.Errorf("failed to create AWS session: %w", err)
	}

	accountID, err := utils.GetAccountID(sess)
	if err != nil {
		logging.Error("Failed to get caller identity", err, nil)
		return nil, fmt.Errorf("failed to get caller identity: %w", err)
	}

	clients := utils.CreateServiceClients(sess)
	s3Client := s3.New(sess)

	var results awslib.ScanResults
	endTime := time.Now().UTC().Truncate(time.Minute)
	daysUnused := awslib.Max(1, opts.DaysUnused)
	startTime := endTime.Add(-time.Duration(daysUnused) * 24 * time.Hour)

	listOutput, err := s3Client.ListBuckets(&s3.ListBucketsInput{})
	if err != nil {
		logging.Error("Failed to list S3 buckets", err, nil)
		return nil, fmt.Errorf("failed to list S3 buckets: %w", err)
	}

	for _, bucket := range listOutput.Buckets {
		bucketName := aws.StringValue(bucket.Name)
		creationDate := aws.TimeValue(bucket.CreationDate)

		locationOutput, err := s3Client.GetBucketLocation(&s3.GetBucketLocationInput{
			Bucket: aws.String(bucketName),
		})
		if err != nil {
			logging.Error("Failed to get bucket location", err, map[string]interface{}{
				"bucket": bucketName,
			})
			continue
		}

		bucketRegion := aws.StringValue(locationOutput.LocationConstraint)
		if bucketRegion == "" {
			bucketRegion = "us-east-1"
		}

		if bucketRegion != opts.Region {
			logging.Debug("Skipping bucket in different region", map[string]interface{}{
				"bucket":        bucketName,
				"bucket_region": bucketRegion,
				"target_region": opts.Region,
			})
			continue
		}

		logging.Debug("Analyzing S3 bucket", map[string]interface{}{
			"bucket": bucketName,
			"region": bucketRegion,
		})

		currentObjectCount := s.getBucketObjectCount(s3Client, bucketName)

		metrics, err := s.getBucketMetrics(clients.CloudWatch, bucketName, startTime, endTime)
		if err != nil {
			logging.Error("Failed to get bucket metrics", err, map[string]interface{}{
				"bucket":     bucketName,
				"startTime": startTime.Format(time.RFC3339),
				"endTime":   endTime.Format(time.RFC3339),
			})
			continue
		}

		// Try to get bucket versioning status
		versioning, err := s3Client.GetBucketVersioning(&s3.GetBucketVersioningInput{
			Bucket: aws.String(bucketName),
		})
		if err != nil {
			logging.Error("Failed to get bucket versioning", err, map[string]interface{}{
				"bucket": bucketName,
			})
		}

		// Try to get bucket encryption
		encryption, err := s3Client.GetBucketEncryption(&s3.GetBucketEncryptionInput{
			Bucket: aws.String(bucketName),
		})
		if err != nil {
			logging.Debug("Failed to get bucket encryption", map[string]interface{}{
				"bucket": bucketName,
				"error":  err.Error(),
			})
		}

		reasons := s.determineUnusedReasons(currentObjectCount, metrics, daysUnused)

		if len(reasons) > 0 {
			details := map[string]interface{}{
				"ObjectCount":     currentObjectCount,
				"Region":         bucketRegion,
				"BucketSizeBytes": metrics["bucket_size"],
				"GetRequests":     metrics["get_requests"],
				"PutRequests":     metrics["put_requests"],
				"DeleteRequests":  metrics["delete_requests"],
				"AccountId":       accountID,
				"CreationDate":    creationDate.Format(time.RFC3339),
			}

			// Add versioning status if available
			if versioning != nil {
				details["Versioning"] = aws.StringValue(versioning.Status)
			}

			// Add encryption information if available
			if encryption != nil && encryption.ServerSideEncryptionConfiguration != nil {
				rules := encryption.ServerSideEncryptionConfiguration.Rules
				if len(rules) > 0 && rules[0].ApplyServerSideEncryptionByDefault != nil {
					details["EncryptionType"] = aws.StringValue(rules[0].ApplyServerSideEncryptionByDefault.SSEAlgorithm)
					if rules[0].ApplyServerSideEncryptionByDefault.KMSMasterKeyID != nil {
						details["KMSKeyId"] = aws.StringValue(rules[0].ApplyServerSideEncryptionByDefault.KMSMasterKeyID)
					}
				}
			}

			result := awslib.ScanResult{
				ResourceType: s.Label(),
				ResourceName: bucketName,
				ResourceID:   fmt.Sprintf("arn:aws:s3:::%s", bucketName),
				Reason:       strings.Join(reasons, "\n"),
				Details:      details,
			}

			results = append(results, result)
		}
	}

	return results, nil
}
