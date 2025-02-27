package scanners

import (
	"fmt"
	"math"
	"strings"
	"time"

	awslib "cloudsift/internal/aws"
	"cloudsift/internal/logging"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudwatch"
	"github.com/aws/aws-sdk-go/service/elb"
	"github.com/aws/aws-sdk-go/service/elbv2"
)

const (
	MetricDatapointThreshold  = 10
	RequestDeviationThreshold = 0.1
)

// ELBScanner scans for unused Elastic Load Balancers
type ELBScanner struct{}

func init() {
	awslib.DefaultRegistry.RegisterScanner(&ELBScanner{})
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

func getLoadBalancerShortName(arn string) string {
	parts := strings.Split(arn, ":")
	if len(parts) >= 6 {
		return parts[5] // This gets the "app/my-alb/1234567890" part
	} else {
		return arn
	}
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
func (s *ELBScanner) getLoadBalancerMetrics(cwClient *cloudwatch.CloudWatch, lb interface{}, opts awslib.ScanOptions) (map[string]interface{}, error) {
	endTime := time.Now()
	startTime := endTime.Add(-time.Duration(opts.DaysUnused) * 24 * time.Hour)

	// Determine metrics based on LB type
	var namespace, requestMetric, bytesMetric, dimensionName, dimensionValue string
	var period int64 = 86400 // 1 day

	switch v := lb.(type) {
	case *elbv2.LoadBalancer:
		namespace = "AWS/ApplicationELB"
		requestMetric = "RequestCount"
		bytesMetric = "ProcessedBytes"
		dimensionName = "LoadBalancer"
		dimensionValue = getLoadBalancerShortName(aws.StringValue(v.LoadBalancerArn))

		// For Gateway LB, use different namespace and 1 day period
		if aws.StringValue(v.Type) == "gateway" {
			namespace = "AWS/GatewayELB"
			period = 86400 // 1 day
		}
	case *elb.LoadBalancerDescription:
		namespace = "AWS/ELB"
		requestMetric = "RequestCount"
		bytesMetric = "ProcessedBytes"
		dimensionName = "LoadBalancerName"
		dimensionValue = aws.StringValue(v.LoadBalancerName)
	default:
		return nil, fmt.Errorf("unknown load balancer type: %T", lb)
	}

	// Get request count metrics
	requestData, err := cwClient.GetMetricStatistics(&cloudwatch.GetMetricStatisticsInput{
		Namespace:  aws.String(namespace),
		MetricName: aws.String(requestMetric),
		Dimensions: []*cloudwatch.Dimension{
			{
				Name:  aws.String(dimensionName),
				Value: aws.String(dimensionValue),
			},
		},
		StartTime: aws.Time(startTime),
		EndTime:   aws.Time(endTime),
		Period:    aws.Int64(period),
		Statistics: []*string{
			aws.String("Sum"),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get request metrics: %w", err)
	}

	// Get bytes processed metrics
	bytesData, err := cwClient.GetMetricStatistics(&cloudwatch.GetMetricStatisticsInput{
		Namespace:  aws.String(namespace),
		MetricName: aws.String(bytesMetric),
		Dimensions: []*cloudwatch.Dimension{
			{
				Name:  aws.String(dimensionName),
				Value: aws.String(dimensionValue),
			},
		},
		StartTime: aws.Time(startTime),
		EndTime:   aws.Time(endTime),
		Period:    aws.Int64(period),
		Statistics: []*string{
			aws.String("Sum"),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get bytes metrics: %w", err)
	}

	// Calculate total requests and bytes
	var totalRequests, totalBytes float64
	var requestCounts []float64

	for _, point := range requestData.Datapoints {
		totalRequests += aws.Float64Value(point.Sum)
		requestCounts = append(requestCounts, aws.Float64Value(point.Sum))
	}

	for _, point := range bytesData.Datapoints {
		totalBytes += aws.Float64Value(point.Sum)
	}

	// Calculate request deviation
	var requestDeviation float64
	if len(requestCounts) >= 2 {
		mean := totalRequests / float64(len(requestCounts))
		var sumSquares float64
		for _, point := range requestCounts {
			diff := point - mean
			sumSquares += diff * diff
		}
		variance := sumSquares / float64(len(requestCounts))
		requestDeviation = math.Sqrt(variance)
	}

	return map[string]interface{}{
		"TotalRequests":    totalRequests,
		"TotalBytesSent":   totalBytes,
		"RequestDeviation": requestDeviation,
		"ProcessedGB":      totalBytes / 1024 / 1024 / 1024, // Convert bytes to GB
		"DatapointCount":   float64(len(requestCounts)),
	}, nil
}

// isUnusedLoadBalancer determines if a load balancer is unused based on metrics and attached resources
func (s *ELBScanner) isUnusedLoadBalancer(elbClient *elbv2.ELBV2, classicClient *elb.ELB, lb interface{}, metrics map[string]interface{}, opts awslib.ScanOptions) (bool, string) {
	// First check if there are any attached resources
	hasResources, err := s.hasAttachedResources(elbClient, classicClient, lb)
	if err != nil {
		logging.Error("Failed to check attached resources", err, map[string]interface{}{
			"lb_arn": aws.StringValue(lb.(*elbv2.LoadBalancer).LoadBalancerArn),
		})
	} else if !hasResources {
		return true, "No resources attached"
	}

	// Check if we have enough datapoints
	if metrics["DatapointCount"].(float64) < MetricDatapointThreshold {
		return false, ""
	}

	totalRequests := metrics["TotalRequests"].(float64)
	totalBytes := metrics["TotalBytesSent"].(float64)
	requestDeviation := metrics["RequestDeviation"].(float64)

	if totalRequests == 0 && totalBytes == 0 {
		return true, fmt.Sprintf("No traffic recorded during the threshold period of %d days", opts.DaysUnused)
	}

	if requestDeviation < RequestDeviationThreshold {
		return true, fmt.Sprintf("Very low traffic variation (%.2f) over %d days", requestDeviation, opts.DaysUnused)
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
	case "gateway":
		hourlyRate = 0.0225 // $0.0225 per GWLB-hour
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
	// Get regional session
	sess, err := awslib.GetSessionInRegion(opts.Session, opts.Region)
	if err != nil {
		logging.Error("Failed to create regional session", err, map[string]interface{}{
			"region": opts.Region,
		})
		return nil, fmt.Errorf("failed to create regional session: %w", err)
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
		metrics, err := s.getLoadBalancerMetrics(cwClient, lb, opts)
		if err != nil {
			logging.Error("Failed to get load balancer metrics", err, map[string]interface{}{
				"name": lbName,
				"arn":  lbARN,
			})
			continue
		}

		// Check if unused based on metrics and resources
		isUnused, reason := s.isUnusedLoadBalancer(elbv2Client, elbClassicClient, lb, metrics, opts)
		if !isUnused {
			continue
		}

		// Build result details
		details := map[string]interface{}{
			// Common fields
			"type":            aws.StringValue(lb.Type),
			"arn":             aws.StringValue(lb.LoadBalancerArn),
			"name":            aws.StringValue(lb.LoadBalancerName),
			"scheme":          aws.StringValue(lb.Scheme),
			"vpc_id":          aws.StringValue(lb.VpcId),
			"state":           aws.StringValue(lb.State.Code),
			"state_reason":    aws.StringValue(lb.State.Reason),
			"created_time":    lb.CreatedTime.Format(time.RFC3339),
			"ip_address_type": aws.StringValue(lb.IpAddressType),
			"security_groups": aws.StringValueSlice(lb.SecurityGroups),

			// Availability zones as simple maps
			"availability_zones": func() []map[string]string {
				zones := make([]map[string]string, len(lb.AvailabilityZones))
				for i, az := range lb.AvailabilityZones {
					zones[i] = map[string]string{
						"zone_name": aws.StringValue(az.ZoneName),
						"subnet_id": aws.StringValue(az.SubnetId),
					}
				}
				return zones
			}(),

			// Metric data
			"total_requests":    metrics["TotalRequests"].(float64),
			"total_bytes":       metrics["TotalBytesSent"].(float64),
			"request_deviation": metrics["RequestDeviation"].(float64),
			"processed_gb":      metrics["ProcessedGB"].(float64),
			"datapoint_count":   metrics["DatapointCount"].(float64),
		}

		// Add tags
		tags := make(map[string]string)
		tagsInput := &elbv2.DescribeTagsInput{
			ResourceArns: []*string{lb.LoadBalancerArn},
		}
		if tagDesc, err := elbv2Client.DescribeTags(tagsInput); err == nil {
			for _, tagSet := range tagDesc.TagDescriptions {
				for _, tag := range tagSet.Tags {
					tags[aws.StringValue(tag.Key)] = aws.StringValue(tag.Value)
				}
			}
		}

		results = append(results, awslib.ScanResult{
			ResourceType: s.Label(),
			ResourceName: lbName,
			ResourceID:   aws.StringValue(lb.LoadBalancerArn),
			Reason:       reason,
			Tags:         tags,
			Details:      details,
			Cost: map[string]interface{}{
				"total": s.calculateELBCosts(aws.StringValue(lb.Type)),
			},
		})
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
		metrics, err := s.getLoadBalancerMetrics(cwClient, lb, opts)
		if err != nil {
			logging.Error("Failed to get load balancer metrics", err, map[string]interface{}{
				"name": lbName,
			})
			continue
		}

		// Check if unused based on metrics and resources
		isUnused, reason := s.isUnusedLoadBalancer(elbv2Client, elbClassicClient, lb, metrics, opts)
		if !isUnused {
			continue
		}

		// Build result details for classic ELB
		details := map[string]interface{}{
			// Common fields
			"type":            "classic",
			"name":            aws.StringValue(lb.LoadBalancerName),
			"dns_name":        aws.StringValue(lb.DNSName),
			"scheme":          aws.StringValue(lb.Scheme),
			"vpc_id":          aws.StringValue(lb.VPCId),
			"created_time":    lb.CreatedTime.Format(time.RFC3339),
			"security_groups": aws.StringValueSlice(lb.SecurityGroups),

			// Source security group as flat fields
			"source_security_group_name":  aws.StringValue(lb.SourceSecurityGroup.GroupName),
			"source_security_group_owner": aws.StringValue(lb.SourceSecurityGroup.OwnerAlias),

			// Simple string arrays
			"availability_zones": aws.StringValueSlice(lb.AvailabilityZones),
			"subnets":            aws.StringValueSlice(lb.Subnets),
			"instance_ids": func() []string {
				ids := make([]string, len(lb.Instances))
				for i, inst := range lb.Instances {
					ids[i] = aws.StringValue(inst.InstanceId)
				}
				return ids
			}(),

			// Health check as flat fields
			"health_check_target":              aws.StringValue(lb.HealthCheck.Target),
			"health_check_interval":            aws.Int64Value(lb.HealthCheck.Interval),
			"health_check_timeout":             aws.Int64Value(lb.HealthCheck.Timeout),
			"health_check_unhealthy_threshold": aws.Int64Value(lb.HealthCheck.UnhealthyThreshold),
			"health_check_healthy_threshold":   aws.Int64Value(lb.HealthCheck.HealthyThreshold),

			// Backend servers as simple maps
			"backend_servers": func() []map[string]interface{} {
				backends := make([]map[string]interface{}, len(lb.BackendServerDescriptions))
				for i, backend := range lb.BackendServerDescriptions {
					backends[i] = map[string]interface{}{
						"instance_port": aws.Int64Value(backend.InstancePort),
						"policy_names":  aws.StringValueSlice(backend.PolicyNames),
					}
				}
				return backends
			}(),

			// Listeners as simple maps
			"listeners": func() []map[string]interface{} {
				listeners := make([]map[string]interface{}, len(lb.ListenerDescriptions))
				for i, listener := range lb.ListenerDescriptions {
					listeners[i] = map[string]interface{}{
						"protocol":           aws.StringValue(listener.Listener.Protocol),
						"load_balancer_port": aws.Int64Value(listener.Listener.LoadBalancerPort),
						"instance_protocol":  aws.StringValue(listener.Listener.InstanceProtocol),
						"instance_port":      aws.Int64Value(listener.Listener.InstancePort),
						"ssl_certificate_id": aws.StringValue(listener.Listener.SSLCertificateId),
						"policy_names":       aws.StringValueSlice(listener.PolicyNames),
					}
				}
				return listeners
			}(),

			// Metric data
			"total_requests":    metrics["TotalRequests"].(float64),
			"total_bytes":       metrics["TotalBytesSent"].(float64),
			"request_deviation": metrics["RequestDeviation"].(float64),
			"processed_gb":      metrics["ProcessedGB"].(float64),
			"datapoint_count":   metrics["DatapointCount"].(float64),
		}

		// Add tags
		tags := make(map[string]string)
		tagsInput := &elb.DescribeTagsInput{
			LoadBalancerNames: []*string{lb.LoadBalancerName},
		}
		if tagDesc, err := elbClassicClient.DescribeTags(tagsInput); err == nil {
			for _, tagSet := range tagDesc.TagDescriptions {
				for _, tag := range tagSet.Tags {
					tags[aws.StringValue(tag.Key)] = aws.StringValue(tag.Value)
				}
			}
		}

		results = append(results, awslib.ScanResult{
			ResourceType: s.Label(),
			ResourceName: lbName,
			ResourceID:   aws.StringValue(lb.LoadBalancerName),
			Reason:       reason,
			Tags:         tags,
			Details:      details,
			Cost: map[string]interface{}{
				"total": s.calculateELBCosts("classic"),
			},
		})
	}

	return results, nil
}
