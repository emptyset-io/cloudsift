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
	"github.com/aws/aws-sdk-go/service/opensearchservice"
)

// OpenSearchScanner scans for unused or underutilized OpenSearch clusters
type OpenSearchScanner struct{}

func init() {
	awslib.DefaultRegistry.RegisterScanner(&OpenSearchScanner{})
}

// Name implements Scanner interface
func (s *OpenSearchScanner) Name() string {
	return "opensearch"
}

// ArgumentName implements Scanner interface
func (s *OpenSearchScanner) ArgumentName() string {
	return "opensearch"
}

// Label implements Scanner interface
func (s *OpenSearchScanner) Label() string {
	return "OpenSearch Clusters"
}

// getClusterMetrics retrieves CloudWatch metrics for an OpenSearch cluster
func (s *OpenSearchScanner) getClusterMetrics(cwClient *cloudwatch.CloudWatch, domainName string, startTime, endTime time.Time) (map[string]float64, error) {
	metrics := []utils.MetricConfig{
		{
			Namespace:     "AWS/ES",
			ResourceID:    domainName,
			DimensionName: "DomainName",
			MetricName:    "CPUUtilization",
			Statistic:     "Average",
			StartTime:     startTime.UTC(),
			EndTime:       endTime.UTC(),
		},
		{
			Namespace:     "AWS/ES",
			ResourceID:    domainName,
			DimensionName: "DomainName",
			MetricName:    "FreeStorageSpace",
			Statistic:     "Average",
			StartTime:     startTime.UTC(),
			EndTime:       endTime.UTC(),
		},
		{
			Namespace:     "AWS/ES",
			ResourceID:    domainName,
			DimensionName: "DomainName",
			MetricName:    "SearchRate",
			Statistic:     "Average",
			StartTime:     startTime.UTC(),
			EndTime:       endTime.UTC(),
		},
		{
			Namespace:     "AWS/ES",
			ResourceID:    domainName,
			DimensionName: "DomainName",
			MetricName:    "IndexingRate",
			Statistic:     "Average",
			StartTime:     startTime.UTC(),
			EndTime:       endTime.UTC(),
		},
		{
			Namespace:     "AWS/ES",
			ResourceID:    domainName,
			DimensionName: "DomainName",
			MetricName:    "DocumentCount",
			Statistic:     "Average",
			StartTime:     startTime.UTC(),
			EndTime:       endTime.UTC(),
		},
		{
			Namespace:     "AWS/ES",
			ResourceID:    domainName,
			DimensionName: "DomainName",
			MetricName:    "DeleteRate",
			Statistic:     "Average",
			StartTime:     startTime.UTC(),
			EndTime:       endTime.UTC(),
		},
		{
			Namespace:     "AWS/ES",
			ResourceID:    domainName,
			DimensionName: "DomainName",
			MetricName:    "JVMMemoryPressure",
			Statistic:     "Average",
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
		"cpu_utilization": results["CPUUtilization"],
		"free_storage":    results["FreeStorageSpace"],
		"search_rate":     results["SearchRate"],
		"index_rate":      results["IndexingRate"],
		"doc_count":       results["DocumentCount"],
		"delete_rate":     results["DeleteRate"],
		"jvm_memory":      results["JVMMemoryPressure"],
	}

	return mappedResults, nil
}

// determineUnusedReasons checks if a cluster is unused or underutilized
func (s *OpenSearchScanner) determineUnusedReasons(metrics map[string]float64, volumeSize int64, opts awslib.ScanOptions) []string {
	var reasons []string

	// Check for completely unused clusters
	if metrics["search_rate"] == 0 && metrics["index_rate"] == 0 && metrics["delete_rate"] == 0 {
		if metrics["doc_count"] == 0 {
			reasons = append(reasons, fmt.Sprintf("Cluster is empty with no search, index, or delete activity in the last %d days.", opts.DaysUnused))
		} else {
			reasons = append(reasons, fmt.Sprintf("Cluster has data but no search, index, or delete activity in the last %d days.", opts.DaysUnused))
		}
	}

	// Check for underutilized clusters
	if metrics["cpu_utilization"] < 10 {
		reasons = append(reasons, fmt.Sprintf("Very low CPU utilization (%.2f%%) in the last %d days.", metrics["cpu_utilization"], opts.DaysUnused))
	}

	// Check storage utilization
	storageUtilization := 100 * (1 - (metrics["free_storage"] / float64(volumeSize*1024*1024*1024)))
	if storageUtilization < 20 {
		reasons = append(reasons, fmt.Sprintf("Low storage utilization (%.2f%% used).", storageUtilization))
	}

	// Check JVM memory pressure
	if metrics["jvm_memory"] > 85 {
		reasons = append(reasons, fmt.Sprintf("High JVM memory pressure (%.2f%%).", metrics["jvm_memory"]))
	}

	// Check for low activity clusters
	avgSearchRate := metrics["search_rate"]
	avgIndexRate := metrics["index_rate"]
	if avgSearchRate < 1 && avgIndexRate < 1 && metrics["doc_count"] > 0 {
		reasons = append(reasons, fmt.Sprintf("Very low activity: %.2f searches/hour, %.2f indexes/hour with %.0f documents in the last %d days.", avgSearchRate, avgIndexRate, metrics["doc_count"], opts.DaysUnused))
	}

	return reasons
}

// Scan implements Scanner interface
func (s *OpenSearchScanner) Scan(opts awslib.ScanOptions) (awslib.ScanResults, error) {
	// Get regional session
	sess, err := awslib.GetSessionInRegion(opts.Session, opts.Region)
	if err != nil {
		logging.Error("Failed to create regional session", err, map[string]interface{}{
			"region": opts.Region,
		})
		return nil, fmt.Errorf("failed to create regional session: %w", err)
	}

	// Get current account ID
	accountID, err := utils.GetAccountID(sess)
	if err != nil {
		logging.Error("Failed to get caller identity", err, nil)
		return nil, fmt.Errorf("failed to get caller identity: %w", err)
	}

	// Create service clients
	esClient := opensearchservice.New(sess)
	cwClient := cloudwatch.New(sess)

	// Get all OpenSearch domains
	var results awslib.ScanResults
	endTime := time.Now().UTC()
	startTime := endTime.Add(-time.Duration(opts.DaysUnused) * 24 * time.Hour)

	// List all domains
	listOutput, err := esClient.ListDomainNames(&opensearchservice.ListDomainNamesInput{})
	if err != nil {
		logging.Error("Failed to list OpenSearch domains", err, nil)
		return nil, fmt.Errorf("failed to list OpenSearch domains: %w", err)
	}

	for _, domain := range listOutput.DomainNames {
		domainName := aws.StringValue(domain.DomainName)

		logging.Debug("Analyzing OpenSearch domain", map[string]interface{}{
			"domain_name": domainName,
		})

		// Get domain details
		describeOutput, err := esClient.DescribeDomain(&opensearchservice.DescribeDomainInput{
			DomainName: aws.String(domainName),
		})
		if err != nil {
			logging.Error("Failed to describe domain", err, map[string]interface{}{
				"domain_name": domainName,
			})
			continue
		}

		status := describeOutput.DomainStatus

		// Get cluster metrics
		metrics, err := s.getClusterMetrics(cwClient, domainName, startTime, endTime)
		if err != nil {
			logging.Error("Failed to get cluster metrics", err, map[string]interface{}{
				"domain_name": domainName,
			})
			continue
		}

		// Get cluster configuration
		instanceType := aws.StringValue(status.ClusterConfig.InstanceType)
		instanceCount := aws.Int64Value(status.ClusterConfig.InstanceCount)
		volumeType := aws.StringValue(status.EBSOptions.VolumeType)
		volumeSize := aws.Int64Value(status.EBSOptions.VolumeSize)

		// Determine if cluster is unused/underutilized
		reasons := s.determineUnusedReasons(metrics, volumeSize, opts)

		if len(reasons) > 0 {
			// Calculate cost
			// costConfig := awslib.ResourceCostConfig{
			// 	ResourceType:  "OpenSearch",
			// 	ResourceSize:  instanceType,
			// 	Region:        opts.Region,
			// 	CreationTime:  time.Now(), // OpenSearch API doesn't provide creation time
			// 	VolumeType:    volumeType,
			// 	StorageSize:   volumeSize,
			// 	InstanceCount: instanceCount,
			// }

			// cost, err := awslib.DefaultCostEstimator.CalculateCost(costConfig)
			// if err != nil {
			// 	logging.Error("Failed to calculate cost", err, map[string]interface{}{
			// 		"domain_name": domainName,
			// 	})
			// }

			details := map[string]interface{}{
				"InstanceType":   instanceType,
				"InstanceCount":  instanceCount,
				"VolumeType":     volumeType,
				"VolumeSizeGB":   volumeSize,
				"CPUUtilization": metrics["cpu_utilization"],
				"SearchRate":     metrics["search_rate"],
				"IndexRate":      metrics["index_rate"],
				"DocumentCount":  metrics["doc_count"],
				"JVMMemory":      metrics["jvm_memory"],
				"AccountId":      accountID,
				"Region":         opts.Region,
			}

			// if cost != nil {
			// 	details["Cost"] = cost
			// }

			result := awslib.ScanResult{
				ResourceType: s.Label(),
				ResourceName: domainName,
				ResourceID:   aws.StringValue(status.ARN),
				Reason:       strings.Join(reasons, "\n"),
				Details:      details,
			}

			results = append(results, result)
		}
	}

	return results, nil
}
