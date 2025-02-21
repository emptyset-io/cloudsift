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
	"github.com/aws/aws-sdk-go/service/elb"
	"github.com/aws/aws-sdk-go/service/elbv2"
)

const (
	daysThreshold = 30 // Days to look back for metrics
)

// ELBScanner scans for unused Elastic Load Balancers
type ELBScanner struct{}

func init() {
	if err := awslib.DefaultRegistry.RegisterScanner(&ELBScanner{}); err != nil {
		panic(fmt.Sprintf("Failed to register ELB scanner: %v", err))
	}
}

// Name implements Scanner interface
func (s *ELBScanner) Name() string {
	return "elb"
}

// ArgumentName implements Scanner interface
func (s *ELBScanner) ArgumentName() string {
	return "load-balancers"
}

// Label implements Scanner interface
func (s *ELBScanner) Label() string {
	return "Load Balancers"
}

// getLoadBalancerName gets the name from tags or ARN
func (s *ELBScanner) getLoadBalancerName(elbClient *elbv2.ELBV2, lb *elbv2.LoadBalancer) string {
	// First try to get name from tags
	input := &elbv2.DescribeTagsInput{
		ResourceArns: []*string{lb.LoadBalancerArn},
	}

	tags, err := elbClient.DescribeTags(input)
	if err == nil {
		for _, tagDesc := range tags.TagDescriptions {
			for _, tag := range tagDesc.Tags {
				if aws.StringValue(tag.Key) == "Name" {
					return aws.StringValue(tag.Value)
				}
			}
		}
	}

	// Fall back to LoadBalancer name from ARN
	return aws.StringValue(lb.LoadBalancerName)
}

// hasAttachedResources checks if the load balancer has any attached resources
func (s *ELBScanner) hasAttachedResources(elbClient *elbv2.ELBV2, classicClient *elb.ELB, lb interface{}) (bool, error) {
	switch v := lb.(type) {
	case *elbv2.LoadBalancer:
		// Application and Network Load Balancers
		input := &elbv2.DescribeTargetGroupsInput{
			LoadBalancerArn: v.LoadBalancerArn,
		}

		targetGroups, err := elbClient.DescribeTargetGroups(input)
		if err != nil {
			return false, fmt.Errorf("failed to describe target groups: %w", err)
		}

		// Check each target group for registered targets
		for _, tg := range targetGroups.TargetGroups {
			targetsInput := &elbv2.DescribeTargetHealthInput{
				TargetGroupArn: tg.TargetGroupArn,
			}

			targetHealth, err := elbClient.DescribeTargetHealth(targetsInput)
			if err != nil {
				return false, fmt.Errorf("failed to describe target health: %w", err)
			}

			// If there are any targets, the load balancer is in use
			if len(targetHealth.TargetHealthDescriptions) > 0 {
				return true, nil
			}
		}

	case *elb.LoadBalancerDescription:
		// Classic Load Balancer
		input := &elb.DescribeInstanceHealthInput{
			LoadBalancerName: v.LoadBalancerName,
		}

		instances, err := classicClient.DescribeInstanceHealth(input)
		if err != nil {
			return false, fmt.Errorf("failed to describe instance health: %w", err)
		}

		// If there are any instances, the load balancer is in use
		return len(instances.InstanceStates) > 0, nil
	}

	// No resources attached
	return false, nil
}

// getLoadBalancerMetrics gets CloudWatch metrics for the load balancer
func (s *ELBScanner) getLoadBalancerMetrics(cwClient *cloudwatch.CloudWatch, lb interface{}) (map[string]interface{}, error) {
	endTime := time.Now()
	startTime := endTime.Add(-time.Duration(daysThreshold) * 24 * time.Hour)

	// Determine metrics based on LB type
	var namespace, requestMetric, bytesMetric, dimensionName, dimensionValue string

	switch v := lb.(type) {
	case *elbv2.LoadBalancer:
		lbARN := aws.StringValue(v.LoadBalancerArn)
		if strings.Contains(lbARN, "app/") {
			namespace = "AWS/ApplicationELB"
			requestMetric = "RequestCount"
			bytesMetric = "ProcessedBytes"
			dimensionName = "LoadBalancer"
			// For ALB, we need to extract just the name part of the ARN
			parts := strings.Split(lbARN, "/")
			if len(parts) >= 3 {
				dimensionValue = parts[len(parts)-2] + "/" + parts[len(parts)-1]
			} else {
				return nil, fmt.Errorf("invalid ALB ARN format: %s", lbARN)
			}
		} else if strings.Contains(lbARN, "net/") {
			namespace = "AWS/NetworkELB"
			requestMetric = "ActiveFlowCount"
			bytesMetric = "ProcessedBytes"
			dimensionName = "LoadBalancer"
			// For NLB, we need to extract just the name part of the ARN
			parts := strings.Split(lbARN, "/")
			if len(parts) >= 3 {
				dimensionValue = parts[len(parts)-2] + "/" + parts[len(parts)-1]
			} else {
				return nil, fmt.Errorf("invalid NLB ARN format: %s", lbARN)
			}
		}
	case *elb.LoadBalancerDescription:
		namespace = "AWS/ELB"
		requestMetric = "RequestCount"
		bytesMetric = "ProcessedBytes"
		dimensionName = "LoadBalancerName"
		dimensionValue = aws.StringValue(v.LoadBalancerName)
	default:
		return nil, fmt.Errorf("unknown load balancer type")
	}

	// Validate required fields
	if namespace == "" || requestMetric == "" || dimensionName == "" || dimensionValue == "" {
		return nil, fmt.Errorf("missing required metric configuration: namespace=%s, metric=%s, dimension=%s:%s",
			namespace, requestMetric, dimensionName, dimensionValue)
	}

	logging.Debug("Getting load balancer metrics", map[string]interface{}{
		"namespace":       namespace,
		"request_metric":  requestMetric,
		"bytes_metric":    bytesMetric,
		"dimension_name":  dimensionName,
		"dimension_value": dimensionValue,
		"start_time":      startTime,
		"end_time":        endTime,
	})

	// Get request metrics
	requestInput := &cloudwatch.GetMetricStatisticsInput{
		Namespace:  aws.String(namespace),
		MetricName: aws.String(requestMetric),
		StartTime:  aws.Time(startTime),
		EndTime:    aws.Time(endTime),
		Period:     aws.Int64(3600), // 1 hour
		Statistics: []*string{aws.String("Sum")},
		Dimensions: []*cloudwatch.Dimension{
			{
				Name:  aws.String(dimensionName),
				Value: aws.String(dimensionValue),
			},
		},
	}

	requestData, err := cwClient.GetMetricStatistics(requestInput)
	if err != nil {
		return nil, fmt.Errorf("failed to get request metrics: %w", err)
	}

	// Get bytes processed metrics
	bytesInput := &cloudwatch.GetMetricStatisticsInput{
		Namespace:  aws.String(namespace),
		MetricName: aws.String(bytesMetric),
		StartTime:  aws.Time(startTime),
		EndTime:    aws.Time(endTime),
		Period:     aws.Int64(3600), // 1 hour
		Statistics: []*string{aws.String("Sum")},
		Dimensions: []*cloudwatch.Dimension{
			{
				Name:  aws.String(dimensionName),
				Value: aws.String(dimensionValue),
			},
		},
	}

	bytesData, err := cwClient.GetMetricStatistics(bytesInput)
	if err != nil {
		return nil, fmt.Errorf("failed to get bytes metrics: %w", err)
	}

	// Calculate total requests and bytes
	var totalRequests, totalBytes float64
	var requestPoints []float64

	for _, point := range requestData.Datapoints {
		if point.Sum != nil {
			totalRequests += aws.Float64Value(point.Sum)
			requestPoints = append(requestPoints, aws.Float64Value(point.Sum))
		}
	}

	for _, point := range bytesData.Datapoints {
		if point.Sum != nil {
			totalBytes += aws.Float64Value(point.Sum)
		}
	}

	// Calculate average requests per hour
	avgRequestsPerHour := 0.0
	if len(requestPoints) > 0 {
		avgRequestsPerHour = totalRequests / float64(len(requestPoints))
	}

	return map[string]interface{}{
		"total_requests":        totalRequests,
		"total_bytes":           totalBytes,
		"avg_requests_per_hour": avgRequestsPerHour,
		"datapoints_count":      len(requestData.Datapoints),
	}, nil
}

// isUnusedLoadBalancer determines if a load balancer is unused
func (s *ELBScanner) isUnusedLoadBalancer(elbClient *elbv2.ELBV2, classicClient *elb.ELB, lb interface{}, metrics map[string]interface{}) (bool, string) {
	// First check if there are any attached resources
	hasResources, err := s.hasAttachedResources(elbClient, classicClient, lb)
	if err != nil {
		logging.Error("Failed to check attached resources", err, map[string]interface{}{
			"lb_arn": aws.StringValue(lb.(*elbv2.LoadBalancer).LoadBalancerArn),
		})
	} else if !hasResources {
		return true, "No resources attached"
	}

	totalRequests := metrics["total_requests"].(float64)
	totalBytes := metrics["total_bytes"].(float64)

	if totalRequests == 0 && totalBytes == 0 {
		return true, fmt.Sprintf("No traffic recorded during the threshold period of %d days", daysThreshold)
	}

	return false, ""
}

// calculateELBCosts calculates costs for a load balancer using fixed hourly rates
func (s *ELBScanner) calculateELBCosts(lbType string) *awslib.CostBreakdown {
	var hourlyRate float64

	switch lbType {
	case "application":
		hourlyRate = 0.0225 // $0.0225 per ALB-hour
	case "network":
		hourlyRate = 0.0225 // $0.0225 per NLB-hour
	case "classic":
		hourlyRate = 0.025 // $0.025 per CLB-hour
	default:
		return &awslib.CostBreakdown{} // Return zero costs for unknown types
	}

	// Calculate other rates
	dailyRate := hourlyRate * 24
	monthlyRate := dailyRate * 30 // Approximate month
	yearlyRate := monthlyRate * 12

	return &awslib.CostBreakdown{
		HourlyRate:  hourlyRate,
		DailyRate:   dailyRate,
		MonthlyRate: monthlyRate,
		YearlyRate:  yearlyRate,
	}
}

// Scan implements Scanner interface
func (s *ELBScanner) Scan(opts awslib.ScanOptions) (awslib.ScanResults, error) {
	// Create base session
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
	elbv2Client := elbv2.New(sess)
	elbClassicClient := elb.New(sess)
	cwClient := cloudwatch.New(sess)

	var results awslib.ScanResults

	// Scan Application and Network Load Balancers
	var loadBalancers []*elbv2.LoadBalancer
	input := &elbv2.DescribeLoadBalancersInput{}

	err = elbv2Client.DescribeLoadBalancersPages(input,
		func(page *elbv2.DescribeLoadBalancersOutput, lastPage bool) bool {
			loadBalancers = append(loadBalancers, page.LoadBalancers...)
			return !lastPage
		})

	if err != nil {
		logging.Error("Failed to describe load balancers", err, nil)
		return nil, fmt.Errorf("failed to describe load balancers: %w", err)
	}

	// Scan each ALB/NLB
	for _, lb := range loadBalancers {
		lbName := s.getLoadBalancerName(elbv2Client, lb)
		lbARN := aws.StringValue(lb.LoadBalancerArn)

		logging.Debug("Scanning load balancer", map[string]interface{}{
			"name": lbName,
			"arn":  lbARN,
		})

		// Get metrics
		metrics, err := s.getLoadBalancerMetrics(cwClient, lb)
		if err != nil {
			logging.Error("Failed to get load balancer metrics", err, map[string]interface{}{
				"name": lbName,
				"arn":  lbARN,
			})
			continue
		}

		// Check if unused
		unused, reason := s.isUnusedLoadBalancer(elbv2Client, elbClassicClient, lb, metrics)
		if !unused {
			continue
		}

		// Get all tags
		tagsInput := &elbv2.DescribeTagsInput{
			ResourceArns: []*string{lb.LoadBalancerArn},
		}
		tags := make(map[string]string)

		if tagDesc, err := elbv2Client.DescribeTags(tagsInput); err == nil {
			for _, tagSet := range tagDesc.TagDescriptions {
				for _, tag := range tagSet.Tags {
					tags[aws.StringValue(tag.Key)] = aws.StringValue(tag.Value)
				}
			}
		}

		// Determine LB type for cost calculation
		var lbType string
		if strings.Contains(lbARN, "app/") {
			lbType = "application"
		} else if strings.Contains(lbARN, "net/") {
			lbType = "network"
		}

		// Calculate costs using fixed rates instead of pricing API
		costs := s.calculateELBCosts(lbType)

		// Set lifetime cost to N/A since we can't determine creation time
		var lifetime float64 = 0
		costs.Lifetime = &lifetime
		var hours float64 = 0
		costs.HoursRunning = &hours

		// Create result
		result := awslib.ScanResult{
			ResourceType: s.Label(),
			ResourceName: lbName,
			ResourceID:   lbARN,
			Reason:       reason,
			Details: map[string]interface{}{
				"account_id":            accountID,
				"region":                opts.Region,
				"type":                  lbType,
				"scheme":                aws.StringValue(lb.Scheme),
				"vpc_id":                aws.StringValue(lb.VpcId),
				"state":                 aws.StringValue(lb.State.Code),
				"total_requests":        metrics["total_requests"],
				"total_bytes":           metrics["total_bytes"],
				"avg_requests_per_hour": metrics["avg_requests_per_hour"],
				"datapoints_count":      metrics["datapoints_count"],
			},
			Tags: tags,
			Cost: map[string]interface{}{
				"total": costs,
			},
		}

		results = append(results, result)
	}

	// Scan Classic Load Balancers
	var classicLoadBalancers []*elb.LoadBalancerDescription
	classicInput := &elb.DescribeLoadBalancersInput{}

	err = elbClassicClient.DescribeLoadBalancersPages(classicInput,
		func(page *elb.DescribeLoadBalancersOutput, lastPage bool) bool {
			classicLoadBalancers = append(classicLoadBalancers, page.LoadBalancerDescriptions...)
			return !lastPage
		})

	if err != nil {
		logging.Error("Failed to describe classic load balancers", err, nil)
		return nil, fmt.Errorf("failed to describe classic load balancers: %w", err)
	}

	// Scan each Classic ELB
	for _, lb := range classicLoadBalancers {
		lbName := aws.StringValue(lb.LoadBalancerName)

		logging.Debug("Scanning classic load balancer", map[string]interface{}{
			"name": lbName,
		})

		// Get metrics
		metrics, err := s.getLoadBalancerMetrics(cwClient, lb)
		if err != nil {
			logging.Error("Failed to get load balancer metrics", err, map[string]interface{}{
				"name": lbName,
			})
			continue
		}

		// Check if unused
		unused, reason := s.isUnusedLoadBalancer(elbv2Client, elbClassicClient, lb, metrics)
		if !unused {
			continue
		}

		// Get all tags
		tagsInput := &elb.DescribeTagsInput{
			LoadBalancerNames: []*string{lb.LoadBalancerName},
		}
		tags := make(map[string]string)

		if tagDesc, err := elbClassicClient.DescribeTags(tagsInput); err == nil {
			for _, tagSet := range tagDesc.TagDescriptions {
				for _, tag := range tagSet.Tags {
					tags[aws.StringValue(tag.Key)] = aws.StringValue(tag.Value)
				}
			}
		}

		// Calculate costs using fixed rates instead of pricing API
		costs := s.calculateELBCosts("classic")

		// Set lifetime cost to N/A since we can't determine creation time
		var lifetime float64 = 0
		costs.Lifetime = &lifetime
		var hours float64 = 0
		costs.HoursRunning = &hours

		// Create result
		result := awslib.ScanResult{
			ResourceType: s.Label(),
			ResourceName: lbName,
			ResourceID:   lbName,
			Reason:       reason,
			Details: map[string]interface{}{
				"account_id":            accountID,
				"region":                opts.Region,
				"type":                  "classic",
				"scheme":                aws.StringValue(lb.Scheme),
				"vpc_id":                aws.StringValue(lb.VPCId),
				"total_requests":        metrics["total_requests"],
				"total_bytes":           metrics["total_bytes"],
				"avg_requests_per_hour": metrics["avg_requests_per_hour"],
				"datapoints_count":      metrics["datapoints_count"],
			},
			Tags: tags,
			Cost: map[string]interface{}{
				"total": costs,
			},
		}

		results = append(results, result)
	}

	return results, nil
}
