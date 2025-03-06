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
	"github.com/aws/aws-sdk-go/service/rds"
)

// RDSScanner scans for unused RDS instances
type RDSScanner struct{}

func init() {
	awslib.DefaultRegistry.RegisterScanner(&RDSScanner{})
}

// ArgumentName implements Scanner interface
func (s *RDSScanner) ArgumentName() string {
	return "rds"
}

// Label implements Scanner interface
func (s *RDSScanner) Label() string {
	return "RDS Instances"
}

// Scan implements Scanner interface
func (s *RDSScanner) Scan(opts awslib.ScanOptions) (awslib.ScanResults, error) {
	// Get regional session
	sess, err := awslib.GetSessionInRegion(opts.Session, opts.Region)
	if err != nil {
		logging.Error("Failed to create regional session", err, map[string]interface{}{
			"region": opts.Region,
		})
		return nil, fmt.Errorf("failed to create regional session: %w", err)
	}

	// Create service clients
	clients := utils.CreateServiceClients(sess)
	rdsClient := rds.New(sess)

	// Get all RDS instances
	var instances []*rds.DBInstance
	err = rdsClient.DescribeDBInstancesPages(&rds.DescribeDBInstancesInput{},
		func(page *rds.DescribeDBInstancesOutput, lastPage bool) bool {
			instances = append(instances, page.DBInstances...)
			return !lastPage
		})
	if err != nil {
		logging.Error("Failed to describe RDS instances", err, nil)
		return nil, fmt.Errorf("failed to describe RDS instances: %w", err)
	}

	var results awslib.ScanResults
	endTime := time.Now().UTC()
	startTime := endTime.Add(-time.Duration(opts.DaysUnused) * 24 * time.Hour)

	for _, instance := range instances {
		instanceID := aws.StringValue(instance.DBInstanceIdentifier)
		logging.Debug("Analyzing RDS instance", map[string]interface{}{
			"instance_id": instanceID,
		})

		// Calculate hours running
		hoursRunning := endTime.Sub(aws.TimeValue(instance.InstanceCreateTime)).Hours()

		// Analyze instance usage
		reasons, err := s.analyzeInstanceUsage(clients.CloudWatch, instance, startTime, endTime)
		if err != nil {
			logging.Error("Failed to analyze instance usage", err, map[string]interface{}{
				"instance_id": instanceID,
			})
			continue
		}

		if len(reasons) > 0 {
			// Create details map with comprehensive RDS instance information
			details := map[string]interface{}{
				"InstanceClass":      aws.StringValue(instance.DBInstanceClass),
				"Engine":             aws.StringValue(instance.Engine),
				"EngineVersion":      aws.StringValue(instance.EngineVersion),
				"CreationTime":       aws.TimeValue(instance.InstanceCreateTime),
				"HoursRunning":       hoursRunning,
				"opts.AccountID":     opts.AccountID,
				"Region":             opts.Region,
				"Status":             aws.StringValue(instance.DBInstanceStatus),
				"StorageType":        aws.StringValue(instance.StorageType),
				"AllocatedStorage":   aws.Int64Value(instance.AllocatedStorage),
				"MultiAZ":            aws.BoolValue(instance.MultiAZ),
				"PubliclyAccessible": aws.BoolValue(instance.PubliclyAccessible),
			}

			// Add optional instance details if present
			if instance.Endpoint != nil {
				details["Endpoint"] = aws.StringValue(instance.Endpoint.Address)
				details["Port"] = aws.Int64Value(instance.Endpoint.Port)
			}
			if instance.PerformanceInsightsEnabled != nil && aws.BoolValue(instance.PerformanceInsightsEnabled) {
				details["PerformanceInsightsEnabled"] = true
				if instance.PerformanceInsightsRetentionPeriod != nil {
					details["PerformanceInsightsRetention"] = aws.Int64Value(instance.PerformanceInsightsRetentionPeriod)
				}
			}
			if instance.StorageEncrypted != nil {
				details["StorageEncrypted"] = aws.BoolValue(instance.StorageEncrypted)
				if instance.KmsKeyId != nil {
					details["KmsKeyId"] = aws.StringValue(instance.KmsKeyId)
				}
			}

			result := awslib.ScanResult{
				ResourceType: s.Label(),
				ResourceName: instanceID,
				ResourceID:   aws.StringValue(instance.DBInstanceArn),
				Reason:       strings.Join(reasons, ", "),
				Details:      details,
			}

			// Calculate cost
			costConfig := awslib.ResourceCostConfig{
				ResourceType: "RDS",
				ResourceSize: aws.StringValue(instance.DBInstanceClass),
				Region:       opts.Region,
				CreationTime: aws.TimeValue(instance.InstanceCreateTime),
				StorageSize:  aws.Int64Value(instance.AllocatedStorage),
				VolumeType:   aws.StringValue(instance.StorageType),
				MultiAZ:      aws.BoolValue(instance.MultiAZ),
				Engine:       aws.StringValue(instance.Engine),
			}

			if cost, err := awslib.DefaultCostEstimator.CalculateCost(costConfig); err != nil {
				logging.Error("Failed to calculate cost", err, map[string]interface{}{
					"instance_id": instanceID,
				})
			} else if cost != nil {
				result.Cost = map[string]interface{}{
					"total": cost,
				}
			}

			results = append(results, result)
		}
	}

	return results, nil
}

// analyzeInstanceUsage checks if an instance is underutilized
func (s *RDSScanner) analyzeInstanceUsage(cwClient *cloudwatch.CloudWatch, instance *rds.DBInstance, startTime, endTime time.Time) ([]string, error) {
	instanceID := aws.StringValue(instance.DBInstanceIdentifier)
	var reasons []string

	// Define metrics to collect
	metrics := []struct {
		name      string
		stat      string
		threshold float64
		message   string
	}{
		{"CPUUtilization", "Average", 5, "Very low CPU utilization (%.2f%%) in the last %d days."},
		{"DatabaseConnections", "Maximum", 0, "No active database connections"},
		{"ReadIOPS", "Sum", 0, ""},
		{"WriteIOPS", "Sum", 0, ""},
	}

	// Collect all metrics using utils
	metricResults := make(map[string][]float64)
	for _, metric := range metrics {
		config := []utils.MetricConfig{
			{
				Namespace:     "AWS/RDS",
				ResourceID:    instanceID,
				DimensionName: "DBInstanceIdentifier",
				MetricName:    metric.name,
				Statistic:     metric.stat,
				StartTime:     startTime.UTC(),
				EndTime:       endTime.UTC(),
			},
		}

		values, err := utils.GetResourceMetricsData(cwClient, config)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch %s metrics: %w", metric.name, err)
		}

		// GetResourceMetricsData returns a map[string]float64, convert to slice
		if metricValue, ok := values[metric.name]; ok {
			metricResults[metric.name] = []float64{metricValue}
		} else {
			metricResults[metric.name] = []float64{} // Empty slice if no data found
		}
	}

	// Analyze metrics
	cpuAvg := calculateAverage(metricResults["CPUUtilization"])
	connMax := calculateMax(metricResults["DatabaseConnections"])
	readSum := calculateSum(metricResults["ReadIOPS"])
	writeSum := calculateSum(metricResults["WriteIOPS"])

	// Check for low utilization patterns
	if connMax == 0 {
		reasons = append(reasons, "No active database connections")
	}

	if cpuAvg < 5 {
		reasons = append(reasons, fmt.Sprintf("Very low CPU utilization (%.2f%%) in the last %d days.",
			cpuAvg, int(endTime.Sub(startTime).Hours()/24)))
	}

	if readSum+writeSum == 0 {
		reasons = append(reasons, fmt.Sprintf("Very low I/O activity (reads: %.2f IOPS, writes: %.2f IOPS) in the last %d days.",
			readSum, writeSum, int(endTime.Sub(startTime).Hours()/24)))
	}

	// Check for stopped instances
	if aws.StringValue(instance.DBInstanceStatus) == "stopped" {
		reasons = append(reasons, fmt.Sprintf("Instance has been stopped for %d days.",
			int(endTime.Sub(startTime).Hours()/24)))
	}

	return reasons, nil
}

// Helper functions for metric calculations
func calculateAverage(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func calculateMax(values []float64) float64 {
	max := 0.0
	for _, v := range values {
		if v > max {
			max = v
		}
	}
	return max
}

func calculateSum(values []float64) float64 {
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum
}
