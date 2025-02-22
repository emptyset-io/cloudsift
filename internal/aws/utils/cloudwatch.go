package utils

import (
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudwatch"
)

// MetricConfig represents configuration for retrieving CloudWatch metrics
type MetricConfig struct {
	Namespace     string
	ResourceID    string
	DimensionName string
	MetricName    string
	Statistic     string
	StartTime     time.Time
	EndTime       time.Time
	Period        int64
}

// GetResourceMetrics retrieves CloudWatch metrics for a resource using GetMetricStatistics
func GetResourceMetrics(cwClient *cloudwatch.CloudWatch, config MetricConfig) (float64, error) {
	input := &cloudwatch.GetMetricStatisticsInput{
		Namespace:  aws.String(config.Namespace),
		MetricName: aws.String(config.MetricName),
		StartTime:  aws.Time(config.StartTime),
		EndTime:    aws.Time(config.EndTime),
		Period:     aws.Int64(config.Period),
		Statistics: []*string{
			aws.String(config.Statistic),
		},
		Dimensions: []*cloudwatch.Dimension{
			{
				Name:  aws.String(config.DimensionName),
				Value: aws.String(config.ResourceID),
			},
		},
	}

	output, err := cwClient.GetMetricStatistics(input)
	if err != nil {
		return 0, fmt.Errorf("failed to get metric statistics: %w", err)
	}

	if len(output.Datapoints) == 0 {
		return 0, nil
	}

	// Return the average of all datapoints
	var sum float64
	for _, dp := range output.Datapoints {
		if config.Statistic == "Average" {
			sum += *dp.Average
		} else if config.Statistic == "Sum" {
			sum += *dp.Sum
		} else if config.Statistic == "Maximum" {
			sum += *dp.Maximum
		} else if config.Statistic == "Minimum" {
			sum += *dp.Minimum
		}
	}
	return sum / float64(len(output.Datapoints)), nil
}

// GetResourceMetricsData retrieves multiple metrics for a resource using GetMetricData
func GetResourceMetricsData(cwClient *cloudwatch.CloudWatch, configs []MetricConfig) (map[string]float64, error) {
	queries := make([]*cloudwatch.MetricDataQuery, len(configs))
	for i, config := range configs {
		queries[i] = &cloudwatch.MetricDataQuery{
			Id: aws.String(fmt.Sprintf("m%d", i)),
			MetricStat: &cloudwatch.MetricStat{
				Metric: &cloudwatch.Metric{
					Namespace:  aws.String(config.Namespace),
					MetricName: aws.String(config.MetricName),
					Dimensions: []*cloudwatch.Dimension{
						{
							Name:  aws.String(config.DimensionName),
							Value: aws.String(config.ResourceID),
						},
					},
				},
				Period: aws.Int64(3600),
				Stat:   aws.String(config.Statistic),
			},
		}
	}

	input := &cloudwatch.GetMetricDataInput{
		MetricDataQueries: queries,
		StartTime:         aws.Time(configs[0].StartTime),
		EndTime:           aws.Time(configs[0].EndTime),
	}

	output, err := cwClient.GetMetricData(input)
	if err != nil {
		return nil, fmt.Errorf("failed to get metric data: %w", err)
	}

	results := make(map[string]float64)
	for i, metricResult := range output.MetricDataResults {
		if len(metricResult.Values) > 0 {
			var sum float64
			for _, value := range metricResult.Values {
				sum += *value
			}
			results[configs[i].MetricName] = sum / float64(len(metricResult.Values))
		} else {
			results[configs[i].MetricName] = 0
		}
	}

	return results, nil
}
