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
	"github.com/aws/aws-sdk-go/service/dynamodb"
)

// DynamoDBScanner scans for unused or underutilized DynamoDB tables
type DynamoDBScanner struct{}

func init() {
	awslib.DefaultRegistry.RegisterScanner(&DynamoDBScanner{})
}

// Name implements Scanner interface
func (s *DynamoDBScanner) Name() string {
	return "dynamodb"
}

// ArgumentName implements Scanner interface
func (s *DynamoDBScanner) ArgumentName() string {
	return "dynamodb"
}

// Label implements Scanner interface
func (s *DynamoDBScanner) Label() string {
	return "DynamoDB Tables"
}

// getTableMetrics retrieves CloudWatch metrics for a DynamoDB table
func (s *DynamoDBScanner) getTableMetrics(cwClient *cloudwatch.CloudWatch, tableName string, startTime, endTime time.Time) (map[string]float64, error) {
	metrics := []utils.MetricConfig{
		{
			Namespace:     "AWS/DynamoDB",
			ResourceID:    tableName,
			DimensionName: "TableName",
			MetricName:    "ConsumedReadCapacityUnits",
			Statistic:     "Sum",
			StartTime:     startTime.UTC(),
			EndTime:       endTime.UTC(),
		},
		{
			Namespace:     "AWS/DynamoDB",
			ResourceID:    tableName,
			DimensionName: "TableName",
			MetricName:    "ConsumedWriteCapacityUnits",
			Statistic:     "Sum",
			StartTime:     startTime.UTC(),
			EndTime:       endTime.UTC(),
		},
		{
			Namespace:     "AWS/DynamoDB",
			ResourceID:    tableName,
			DimensionName: "TableName",
			MetricName:    "ThrottledRequests",
			Statistic:     "Sum",
			StartTime:     startTime.UTC(),
			EndTime:       endTime.UTC(),
		},
	}

	results, err := utils.GetResourceMetricsData(cwClient, metrics)
	if err != nil {
		return nil, fmt.Errorf("failed to get metrics: %w", err)
	}

	// Map the metrics to our expected format
	mappedResults := map[string]float64{
		"read_throughput":  results["ConsumedReadCapacityUnits"],
		"write_throughput": results["ConsumedWriteCapacityUnits"],
		"throttled_events": results["ThrottledRequests"],
	}

	return mappedResults, nil
}

// determineUnusedReasons checks if a table is unused or underutilized
func (s *DynamoDBScanner) determineUnusedReasons(metrics map[string]float64, itemCount int64, tableSizeBytes int64, provisionedRead int64, provisionedWrite int64, opts awslib.ScanOptions) []string {
	var reasons []string

	// Check for no activity
	if metrics["read_throughput"] == 0 && metrics["write_throughput"] == 0 {
		if itemCount == 0 {
			reasons = append(reasons, fmt.Sprintf("Empty table with no read/write activity in the last %d days.", opts.DaysUnused))
		} else {
			reasons = append(reasons, fmt.Sprintf("Table has data but no read/write activity in the last %d days.", opts.DaysUnused))
		}
	}

	// Check for very low activity
	if itemCount > 0 && metrics["read_throughput"] < 1 && metrics["write_throughput"] < 1 {
		reasons = append(reasons, fmt.Sprintf("Very low activity (reads: %.2f/sec, writes: %.2f/sec) in the last %d days.",
			metrics["read_throughput"], metrics["write_throughput"], opts.DaysUnused))
	}

	// Check for low provisioned capacity utilization
	if provisionedRead > 0 && metrics["read_throughput"]/float64(provisionedRead) < 0.1 {
		reasons = append(reasons, fmt.Sprintf("Low read capacity utilization (%.2f%%) in the last %d days.",
			(metrics["read_throughput"]/float64(provisionedRead))*100, opts.DaysUnused))
	}
	if provisionedWrite > 0 && metrics["write_throughput"]/float64(provisionedWrite) < 0.1 {
		reasons = append(reasons, fmt.Sprintf("Low write capacity utilization (%.2f%%) in the last %d days.",
			(metrics["write_throughput"]/float64(provisionedWrite))*100, opts.DaysUnused))
	}

	// Check for throttling events
	if metrics["throttled_events"] > 0 {
		reasons = append(reasons, fmt.Sprintf("Table experienced %.0f throttled requests per hour on average.",
			metrics["throttled_events"]))
	}

	// Check for oversized tables with low activity
	tableSizeGB := float64(tableSizeBytes) / (1024 * 1024 * 1024)
	if tableSizeGB >= 1 && metrics["read_throughput"] < 10 && metrics["write_throughput"] < 10 {
		reasons = append(reasons, fmt.Sprintf("Large table (%.2f GB) with low activity.", tableSizeGB))
	}

	return reasons
}

// Scan implements Scanner interface
func (s *DynamoDBScanner) Scan(opts awslib.ScanOptions) (awslib.ScanResults, error) {
	// Get regional session
	sess, err := awslib.GetSessionInRegion(opts.Session, opts.Region)
	if err != nil {
		logging.Error("Failed to create regional session", err, map[string]interface{}{
			"region": opts.Region,
		})
		return nil, fmt.Errorf("failed to create regional session: %w", err)
	}

	// Create service clients
	dynamodbClient := dynamodb.New(sess)
	cwClient := cloudwatch.New(sess)

	// Get all DynamoDB tables
	var tableNames []*string
	err = dynamodbClient.ListTablesPages(&dynamodb.ListTablesInput{},
		func(page *dynamodb.ListTablesOutput, lastPage bool) bool {
			tableNames = append(tableNames, page.TableNames...)
			return !lastPage
		})
	if err != nil {
		logging.Error("Failed to list DynamoDB tables", err, nil)
		return nil, fmt.Errorf("failed to list DynamoDB tables: %w", err)
	}

	var results awslib.ScanResults
	endTime := time.Now().UTC()
	startTime := endTime.Add(-time.Duration(opts.DaysUnused) * 24 * time.Hour)

	for _, tableName := range tableNames {
		logging.Debug("Analyzing DynamoDB table", map[string]interface{}{
			"table_name": *tableName,
		})

		// Get table details
		tableDesc, err := dynamodbClient.DescribeTable(&dynamodb.DescribeTableInput{
			TableName: tableName,
		})
		if err != nil {
			logging.Error("Failed to describe table", err, map[string]interface{}{
				"table_name": *tableName,
			})
			continue
		}

		// Get table metrics
		metrics, err := s.getTableMetrics(cwClient, *tableName, startTime, endTime)
		if err != nil {
			logging.Error("Failed to get table metrics", err, map[string]interface{}{
				"table_name": *tableName,
			})
			continue
		}

		// Determine if table is unused/underutilized
		reasons := s.determineUnusedReasons(metrics,
			aws.Int64Value(tableDesc.Table.ItemCount),
			aws.Int64Value(tableDesc.Table.TableSizeBytes),
			aws.Int64Value(tableDesc.Table.ProvisionedThroughput.ReadCapacityUnits),
			aws.Int64Value(tableDesc.Table.ProvisionedThroughput.WriteCapacityUnits),
			opts)

		if len(reasons) > 0 {
			details := map[string]interface{}{
				"ItemCount":       aws.Int64Value(tableDesc.Table.ItemCount),
				"TableSizeBytes":  aws.Int64Value(tableDesc.Table.TableSizeBytes),
				"ReadThroughput":  metrics["read_throughput"],
				"WriteThroughput": metrics["write_throughput"],
				"ThrottledEvents": metrics["throttled_events"],
				"account_id":      opts.AccountID,
				"region":          opts.Region,
			}

			// Add billing mode if available
			if tableDesc.Table.BillingModeSummary != nil {
				details["BillingMode"] = aws.StringValue(tableDesc.Table.BillingModeSummary.BillingMode)
			} else {
				details["BillingMode"] = "PROVISIONED" // Default billing mode
			}

			// Add provisioned throughput if available
			if tableDesc.Table.ProvisionedThroughput != nil {
				details["ProvisionedRead"] = aws.Int64Value(tableDesc.Table.ProvisionedThroughput.ReadCapacityUnits)
				details["ProvisionedWrite"] = aws.Int64Value(tableDesc.Table.ProvisionedThroughput.WriteCapacityUnits)
			} else {
				details["ProvisionedRead"] = 0
				details["ProvisionedWrite"] = 0
			}

			result := awslib.ScanResult{
				ResourceType: s.Label(),
				ResourceName: *tableName,
				ResourceID:   *tableName,
				Reason:       strings.Join(reasons, "\n"),
				Details:      details,
			}

			results = append(results, result)
		}
	}

	return results, nil
}
