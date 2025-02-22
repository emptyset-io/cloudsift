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

// S3Scanner scans S3 buckets for usage
type S3Scanner struct{}

func init() {
	awslib.DefaultRegistry.RegisterScanner(&S3Scanner{})
}

// Name implements Scanner interface
func (s *S3Scanner) Name() string {
	return "s3"
}

// ArgumentName implements Scanner interface
func (s *S3Scanner) ArgumentName() string {
	return "s3-buckets"
}

// Label implements Scanner interface
func (s *S3Scanner) Label() string {
	return "S3 Buckets"
}

// getBucketMetrics fetches CloudWatch metrics for a bucket
func (s *S3Scanner) getBucketMetrics(cwClient *cloudwatch.CloudWatch, bucketName string, startTime, endTime time.Time) (map[string]float64, error) {
	metrics := []struct {
		name      string
		namespace string
		metric    string
		stat      string
	}{
		{
			name:      "bucket_size",
			namespace: "AWS/S3",
			metric:    "BucketSizeBytes",
			stat:      "Average",
		},
		{
			name:      "get_requests",
			namespace: "AWS/S3",
			metric:    "GetRequests",
			stat:      "Sum",
		},
		{
			name:      "put_requests",
			namespace: "AWS/S3",
			metric:    "PutRequests",
			stat:      "Sum",
		},
		{
			name:      "delete_requests",
			namespace: "AWS/S3",
			metric:    "DeleteRequests",
			stat:      "Sum",
		},
	}

	results := make(map[string]float64)
	for _, metric := range metrics {
		input := &cloudwatch.GetMetricStatisticsInput{
			Namespace:  aws.String(metric.namespace),
			MetricName: aws.String(metric.metric),
			StartTime:  aws.Time(startTime),
			EndTime:    aws.Time(endTime),
			Period:     aws.Int64(86400), // 1 day
			Statistics: []*string{
				aws.String(metric.stat),
			},
			Dimensions: []*cloudwatch.Dimension{
				{
					Name:  aws.String("BucketName"),
					Value: aws.String(bucketName),
				},
			},
		}

		output, err := cwClient.GetMetricStatistics(input)
		if err != nil {
			return nil, fmt.Errorf("failed to get metric %s: %w", metric.name, err)
		}

		// Get the most recent datapoint
		var value float64
		if len(output.Datapoints) > 0 {
			// Find the most recent datapoint
			var latestPoint *cloudwatch.Datapoint
			for _, point := range output.Datapoints {
				if latestPoint == nil || point.Timestamp.After(*latestPoint.Timestamp) {
					latestPoint = point
				}
			}

			switch metric.stat {
			case "Average":
				value = aws.Float64Value(latestPoint.Average)
			case "Sum":
				value = aws.Float64Value(latestPoint.Sum)
			}
		}

		results[metric.name] = value
	}

	return results, nil
}

// Scan implements Scanner interface
func (s *S3Scanner) Scan(opts awslib.ScanOptions) (awslib.ScanResults, error) {
	// Get regional session
	sess, err := awslib.GetSessionInRegion(opts.Session, opts.Region)
	if err != nil {
		logging.Error("Failed to create regional session", err, map[string]interface{}{
			"region": opts.Region,
		})
		return nil, fmt.Errorf("failed to create regional session: %w", err)
	}

	// Create service clients
	clients := struct {
		S3         *s3.S3
		CloudWatch *cloudwatch.CloudWatch
	}{
		S3:         s3.New(sess),
		CloudWatch: cloudwatch.New(sess),
	}

	// List buckets in the current region
	input := &s3.ListBucketsInput{}
	output, err := clients.S3.ListBuckets(input)
	if err != nil {
		logging.Error("Failed to list S3 buckets", err, nil)
		return nil, fmt.Errorf("failed to list S3 buckets: %w", err)
	}

	// Get account ID for cost calculations
	accountID, err := utils.GetAccountID(sess)
	if err != nil {
		logging.Error("Failed to get AWS account ID", err, nil)
		return nil, fmt.Errorf("failed to get AWS account ID: %w", err)
	}

	// Time range for metrics (last 30 days)
	endTime := time.Now()
	startTime := endTime.AddDate(0, 0, -30)

	var results awslib.ScanResults

	// Process each bucket
	for _, bucket := range output.Buckets {
		bucketName := aws.StringValue(bucket.Name)

		// Get bucket location
		locInput := &s3.GetBucketLocationInput{
			Bucket: bucket.Name,
		}
		locOutput, err := clients.S3.GetBucketLocation(locInput)
		if err != nil {
			logging.Error("Failed to get bucket location", err, map[string]interface{}{
				"bucket": bucketName,
			})
			continue
		}

		// Convert empty location (US East-1) to its proper name
		bucketRegion := aws.StringValue(locOutput.LocationConstraint)
		if bucketRegion == "" {
			bucketRegion = "us-east-1"
		}

		// Skip if bucket is not in the target region
		if bucketRegion != opts.Region {
			continue
		}

		// Get object count
		objectCount := int64(0)
		err = clients.S3.ListObjectsV2Pages(&s3.ListObjectsV2Input{
			Bucket: aws.String(bucketName),
		}, func(page *s3.ListObjectsV2Output, lastPage bool) bool {
			objectCount += aws.Int64Value(page.KeyCount)
			return !lastPage
		})
		if err != nil {
			logging.Error("Failed to get bucket object count", err, map[string]interface{}{
				"bucket": bucketName,
			})
			continue
		}

		// Get metrics for the bucket
		metrics, err := s.getBucketMetrics(clients.CloudWatch, bucketName, startTime, endTime)
		if err != nil {
			logging.Error("Failed to get bucket metrics", err, map[string]interface{}{
				"bucket":    bucketName,
				"startTime": startTime.Format(time.RFC3339),
				"endTime":   endTime.Format(time.RFC3339),
			})
			continue
		}

		// Check for inactivity
		var reasons []string
		if metrics["get_requests"] == 0 && metrics["put_requests"] == 0 && metrics["delete_requests"] == 0 {
			reasons = append(reasons, fmt.Sprintf("No GET, PUT, or DELETE requests in the last %d days", opts.DaysUnused))
		}

		// Get bucket tags
		tagsInput := &s3.GetBucketTaggingInput{
			Bucket: bucket.Name,
		}
		tags := make(map[string]string)
		if tagsOutput, err := clients.S3.GetBucketTagging(tagsInput); err == nil {
			for _, tag := range tagsOutput.TagSet {
				tags[aws.StringValue(tag.Key)] = aws.StringValue(tag.Value)
			}
		}

		if len(reasons) > 0 {
			details := map[string]interface{}{
				"ObjectCount":     objectCount,
				"Region":          bucketRegion,
				"BucketSizeBytes": metrics["bucket_size"],
				"GetRequests":     metrics["get_requests"],
				"PutRequests":     metrics["put_requests"],
				"DeleteRequests":  metrics["delete_requests"],
				"AccountId":       accountID,
			}

			result := awslib.ScanResult{
				ResourceType: s.Label(),
				ResourceName: bucketName,
				ResourceID:   bucketName,
				Reason:       strings.Join(reasons, "\n"),
				Tags:         tags,
				Details:      details,
			}

			results = append(results, result)
		}
	}

	return results, nil
}
