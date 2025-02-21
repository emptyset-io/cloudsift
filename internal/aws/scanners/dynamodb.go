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
	if err := awslib.DefaultRegistry.RegisterScanner(&DynamoDBScanner{}); err != nil {
		panic(fmt.Sprintf("Failed to register DynamoDB scanner: %v", err))
	}
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

		// Safely get table values with nil checks
		var (
			itemCount      int64
			tableSizeBytes int64
			readCapacity   int64
			writeCapacity  int64
		)

		if tableDesc.Table != nil {
			if tableDesc.Table.ItemCount != nil {
				itemCount = aws.Int64Value(tableDesc.Table.ItemCount)
			}
			if tableDesc.Table.TableSizeBytes != nil {
				tableSizeBytes = aws.Int64Value(tableDesc.Table.TableSizeBytes)
			}
			if tableDesc.Table.ProvisionedThroughput != nil {
				if tableDesc.Table.ProvisionedThroughput.ReadCapacityUnits != nil {
					readCapacity = aws.Int64Value(tableDesc.Table.ProvisionedThroughput.ReadCapacityUnits)
				}
				if tableDesc.Table.ProvisionedThroughput.WriteCapacityUnits != nil {
					writeCapacity = aws.Int64Value(tableDesc.Table.ProvisionedThroughput.WriteCapacityUnits)
				}
			}
		}

		// Determine if table is unused/underutilized
		reasons := s.determineUnusedReasons(metrics, itemCount, tableSizeBytes, readCapacity, writeCapacity, opts)

		if len(reasons) > 0 {
			// Calculate cost if possible
			var monthlyCost float64
			var err error

			// Try to get cost, but don't fail if pricing info isn't available
			monthlyCost, err = utils.GetDynamoDBCost(tableSizeBytes, readCapacity, writeCapacity, opts.Region)
			if err != nil {
				logging.Error("Failed to calculate cost", err, map[string]interface{}{
					"table_name": *tableName,
				})
				// Continue with cost as 0 if we can't calculate it
				monthlyCost = 0
			}

			// Create details map with DynamoDB-specific fields
			details := map[string]interface{}{
				"TableName":          *tableName,
				"ItemCount":          itemCount,
				"TableSizeBytes":     tableSizeBytes,
				"TableSizeGB":        float64(tableSizeBytes) / (1024 * 1024 * 1024),
				"ReadCapacityUnits":  readCapacity,
				"WriteCapacityUnits": writeCapacity,
				"ReadThroughput":     metrics["read_throughput"],
				"WriteThroughput":    metrics["write_throughput"],
				"ThrottledEvents":    metrics["throttled_events"],
				"MonthlyCost":        monthlyCost,
			}

			// Create resource ARN
			resourceARN := fmt.Sprintf("arn:aws:dynamodb:%s:%s:table/%s",
				opts.Region, accountID, *tableName)

			results = append(results, awslib.ScanResult{
				ResourceType: s.Label(),
				ResourceName: *tableName,
				ResourceID:   resourceARN,
				Reason:      strings.Join(reasons, "\n"),
				Details:     details,
			})
		}
	}

	logging.Debug("Completed DynamoDB tables scan", map[string]interface{}{
		"total_tables": len(tableNames),
		"account_id":   accountID,
	})

	return results, nil
}
