package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ResourceDetails struct {
	InstanceType          string                 `json:"InstanceType,omitempty"`
	State                 string                 `json:"State,omitempty"`
	LaunchTime            string                 `json:"LaunchTime,omitempty"`
	Tags                  []map[string]string    `json:"Tags,omitempty"`
	SecurityGroups        []map[string]string    `json:"SecurityGroups,omitempty"`
	NetworkInterfaces     []map[string]string    `json:"NetworkInterfaces,omitempty"`
	DBInstanceClass       string                 `json:"DBInstanceClass,omitempty"`
	Engine                string                 `json:"Engine,omitempty"`
	EngineVersion         string                 `json:"EngineVersion,omitempty"`
	DBInstanceIdentifier  string                 `json:"DBInstanceIdentifier,omitempty"`
	DBInstanceStatus      string                 `json:"DBInstanceStatus,omitempty"`
	MasterUsername        string                 `json:"MasterUsername,omitempty"`
	Endpoint              map[string]interface{} `json:"Endpoint,omitempty"`
	AllocatedStorage      int                    `json:"AllocatedStorage,omitempty"`
	BackupRetentionPeriod int                    `json:"BackupRetentionPeriod,omitempty"`
	PreferredBackupWindow string                 `json:"PreferredBackupWindow,omitempty"`
	VpcSecurityGroups     []map[string]string    `json:"VpcSecurityGroups,omitempty"`
	MultiAZ               bool                   `json:"MultiAZ,omitempty"`
	PubliclyAccessible    bool                   `json:"PubliclyAccessible,omitempty"`
	StorageEncrypted      bool                   `json:"StorageEncrypted,omitempty"`
	PerformanceInsights   map[string]interface{} `json:"PerformanceInsights,omitempty"`
	Size                  int                    `json:"Size,omitempty"`
	VolumeType            string                 `json:"VolumeType,omitempty"`
	VolumeId              string                 `json:"VolumeId,omitempty"`
	Encrypted             bool                   `json:"Encrypted,omitempty"`
	Iops                  int                    `json:"Iops,omitempty"`
	MultiAttach           bool                   `json:"MultiAttach,omitempty"`
	VolumeSize            int                    `json:"VolumeSize,omitempty"`
	StartTime             string                 `json:"StartTime,omitempty"`
	ProvisionedThroughput map[string]int         `json:"ProvisionedThroughput,omitempty"`
	TableStatus           string                 `json:"TableStatus,omitempty"`
	TableName             string                 `json:"TableName,omitempty"`
	CreationDateTime      string                 `json:"CreationDateTime,omitempty"`
	TableSizeBytes        int                    `json:"TableSizeBytes,omitempty"`
	ItemCount             int                    `json:"ItemCount,omitempty"`
	StreamEnabled         bool                   `json:"StreamEnabled,omitempty"`
	CreateDate            string                 `json:"CreateDate,omitempty"`
	PasswordLastUsed      string                 `json:"PasswordLastUsed,omitempty"`
	Groups                int                    `json:"Groups,omitempty"`
	LastUsedDate          string                 `json:"LastUsedDate,omitempty"`
	AttachedPolicies      int                    `json:"AttachedPolicies,omitempty"`
	PublicIp              string                 `json:"PublicIp,omitempty"`
	AllocationId          string                 `json:"AllocationId,omitempty"`
	Domain                string                 `json:"Domain,omitempty"`
	ClusterConfig         map[string]interface{} `json:"ClusterConfig,omitempty"`
}

type ResourceCosts struct {
	Hourly   float64 `json:"hourly"`
	Daily    float64 `json:"daily"`
	Monthly  float64 `json:"monthly"`
	Yearly   float64 `json:"yearly"`
	Lifetime float64 `json:"lifetime"`
}

type Resource struct {
	ID                      string          `json:"id"`
	Name                    string          `json:"name"`
	Type                    string          `json:"type"`
	AccountID               string          `json:"account_id"`
	AccountName             string          `json:"account_name"`
	Region                  string          `json:"region"`
	LastUsed                string          `json:"last_used"`
	EstimatedMonthlySavings float64         `json:"estimated_monthly_savings"`
	Reasons                 []string        `json:"reasons"`
	Details                 ResourceDetails `json:"details"`
	Costs                   ResourceCosts   `json:"costs"`
}

type ScanMetrics struct {
	CompletedAt        string         `json:"CompletedAt"`
	TotalResources     int            `json:"TotalResources"`
	ResourcesByType    map[string]int `json:"ResourcesByType"`
	TotalCost          float64        `json:"TotalCost"`
	TotalScans         int            `json:"TotalScans"`
	CompletedScans     int            `json:"CompletedScans"`
	FailedScans        int            `json:"FailedScans"`
	TasksPerSecond     float64        `json:"TasksPerSecond"`
	AvgExecutionTimeMs float64        `json:"AvgExecutionTimeMs"`
	WorkerUtilization  float64        `json:"WorkerUtilization"`
	PeakWorkers        int            `json:"PeakWorkers"`
	MaxWorkers         int            `json:"MaxWorkers"`
	TotalRunTime       string         `json:"TotalRunTime"`
}

type CostBreakdown struct {
	Type     string  `json:"type"`
	Hourly   float64 `json:"hourly"`
	Daily    float64 `json:"daily"`
	Monthly  float64 `json:"monthly"`
	Yearly   float64 `json:"yearly"`
	Lifetime float64 `json:"lifetime"`
}

type ScanData struct {
	ScanMetrics        ScanMetrics         `json:"ScanMetrics"`
	AccountsAndRegions map[string][]string `json:"AccountsAndRegions"`
	AccountNames       map[string]string   `json:"AccountNames"`
	UnusedResources    []Resource          `json:"UnusedResources"`
	CostBreakdown      []CostBreakdown     `json:"CostBreakdown"`
	TotalCosts         ResourceCosts       `json:"TotalCosts"`
}

func generateResourceName(resourceType string) string {
	switch resourceType {
	case "EC2 Instance":
		return "app-server"
	case "RDS Instance":
		engines := []string{"mysql", "postgres", "aurora-postgresql"}
		return fmt.Sprintf("%s-db", engines[rand.Intn(len(engines))])
	case "DynamoDB Table":
		return "user-data"
	case "EBS Volume":
		return "data-volume"
	case "EBS Snapshot":
		return "backup"
	case "Elastic IP":
		return "static-ip"
	case "ELB":
		return "load-balancer"
	case "IAM Role":
		return "service-role"
	case "IAM User":
		return "system-user"
	case "OpenSearch Domain":
		return "search-cluster"
	default:
		return "resource"
	}
}

func generateResourceReasons(resourceType string) []string {
	cpuUtil := rand.Float64()*4 + 1
	memoryUtil := rand.Float64()*7 + 1
	networkUtil := rand.Float64()*1.9 + 0.1

	reasons := make([]string, 0)
	switch resourceType {
	case "EC2 Instance":
		reasons = append(reasons,
			fmt.Sprintf("Low CPU utilization (average %.2f%%) in the last 180 days", cpuUtil),
			fmt.Sprintf("Low memory utilization (average %.2f%%)", memoryUtil),
			fmt.Sprintf("Minimal network traffic (%.2f KB/s average)", networkUtil),
			"No active SSH sessions in past 90 days",
			"Instance has been running continuously without maintenance window")
	case "RDS Instance":
		connections := rand.Intn(6)
		storageUsed := rand.Float64()*15 + 5
		reasons = append(reasons,
			fmt.Sprintf("Low connection count (average %d active connections/day)", connections),
			fmt.Sprintf("Low storage utilization (%.1f%% used)", storageUsed),
			"Minimal query activity in the last 180 days",
			"No database parameter changes in past 90 days",
			"Backup retention period longer than necessary")
	case "ELB":
		reasons = append(reasons,
			fmt.Sprintf("Low request count (average %.1f requests/minute)", networkUtil),
			"No healthy backend instances",
			"SSL certificate expiring soon",
			"Cross-zone load balancing disabled",
			"Access logs disabled")
	case "EBS Volume":
		iops := rand.Float64()*10 + 1
		reasons = append(reasons,
			fmt.Sprintf("Low I/O activity (average %.1f IOPS)", iops),
			"Volume not attached to any instance",
			"Volume type may be over-provisioned",
			"Snapshot schedule not configured",
			"Volume encryption not enabled")
	case "EBS Snapshot":
		ageInDays := rand.Intn(180) + 180
		reasons = append(reasons,
			fmt.Sprintf("Snapshot is %d days old", ageInDays),
			"Source volume has been deleted",
			"Multiple redundant snapshots exist",
			"No tags present",
			"Created from terminated instance")
	case "DynamoDB Table":
		readCapacity := rand.Float64()*1.9 + 0.1
		writeCapacity := rand.Float64()*0.9 + 0.1
		reasons = append(reasons,
			fmt.Sprintf("Low read capacity utilization (%.2f%% of provisioned)", readCapacity),
			fmt.Sprintf("Low write capacity utilization (%.2f%% of provisioned)", writeCapacity),
			"No table updates in past month",
			"Auto-scaling not configured",
			"Backup older than retention policy")
	case "IAM User":
		reasons = append(reasons,
			"No console or API activity in past 180 days",
			"Access keys not rotated in past year",
			"MFA not enabled",
			"Attached policies provide excessive permissions",
			"Direct policy attachments instead of group-based")
	case "IAM Role":
		reasons = append(reasons,
			"No service activity in past 90 days",
			"Overly permissive trust relationship",
			"Unused service permissions",
			"Policy allows full administrative access",
			"No boundary policy configured")
	case "Elastic IP":
		reasons = append(reasons,
			"Not associated with any running instance",
			"Associated instance in stopped state",
			"No DNS records pointing to this IP",
			"In unused region",
			"No tags present")
	case "OpenSearch Domain":
		reasons = append(reasons,
			fmt.Sprintf("Low search traffic (%.2f requests/second)", networkUtil),
			fmt.Sprintf("Low CPU utilization (average %.2f%%)", cpuUtil),
			"Instance type may be over-provisioned",
			"Unused index replicas",
			"Snapshot retention longer than necessary")
	}

	// Randomly select 3-5 reasons
	numReasons := rand.Intn(3) + 3
	if len(reasons) > numReasons {
		reasons = reasons[:numReasons]
	}
	return reasons
}

func generateResourceDetails(resourceType string) ResourceDetails {
	details := ResourceDetails{}
	now := time.Now()

	switch resourceType {
	case "EC2 Instance":
		instanceTypes := []string{"t3.micro", "t3.small", "t3.medium", "t3.large", "r5.xlarge"}
		details.InstanceType = instanceTypes[rand.Intn(len(instanceTypes))]
		details.State = []string{"running", "stopped"}[rand.Intn(2)]
		details.LaunchTime = now.AddDate(0, 0, -rand.Intn(365)).Format("2006-01-02T15:04:05")
		details.Tags = []map[string]string{
			{"Key": "Name", "Value": fmt.Sprintf("%s-01", generateResourceName(resourceType))},
			{"Key": "Environment", "Value": []string{"dev", "stg", "prod"}[rand.Intn(3)]},
			{"Key": "Team", "Value": []string{"platform", "backend", "frontend"}[rand.Intn(3)]},
		}
		details.SecurityGroups = []map[string]string{
			{"GroupId": "sg-0123456789abcdef0", "GroupName": "default"},
			{"GroupId": "sg-0123456789abcdef1", "GroupName": "web-servers"},
		}
		details.NetworkInterfaces = []map[string]string{
			{"NetworkInterfaceId": "eni-0123456789abcdef0", "PrivateIpAddress": "172.31.16.100"},
		}

	case "RDS Instance":
		instanceClasses := []string{"db.t3.micro", "db.t3.small", "db.t3.medium", "db.r5.large"}
		details.DBInstanceClass = instanceClasses[rand.Intn(len(instanceClasses))]
		details.Engine = []string{"mysql", "postgres", "aurora"}[rand.Intn(3)]
		details.EngineVersion = "8.0.28"
		details.DBInstanceStatus = "available"
		details.MasterUsername = "admin"
		details.AllocatedStorage = rand.Intn(951) + 50    // 50-1000
		details.BackupRetentionPeriod = rand.Intn(31) + 5 // 5-35 days
		details.PreferredBackupWindow = "03:00-04:00"
		details.MultiAZ = rand.Intn(2) == 1
		details.PubliclyAccessible = rand.Intn(2) == 1
		details.StorageEncrypted = rand.Intn(2) == 1
		details.VpcSecurityGroups = []map[string]string{
			{"GroupId": "sg-0123456789abcdef2", "GroupName": "rds-security-group"},
		}
		details.Endpoint = map[string]interface{}{
			"Address": fmt.Sprintf("mydb.123456789012.%s.rds.amazonaws.com", "us-west-2"),
			"Port":    3306,
		}

	case "ELB":
		details.State = "active"
		details.Tags = []map[string]string{
			{"Key": "Name", "Value": generateResourceName(resourceType)},
			{"Key": "Environment", "Value": []string{"dev", "stg", "prod"}[rand.Intn(3)]},
		}
		details.SecurityGroups = []map[string]string{
			{"GroupId": "sg-0123456789abcdef3", "GroupName": "elb-security-group"},
		}

	case "EBS Volume":
		volumeTypes := []string{"gp2", "gp3"}
		details.VolumeType = volumeTypes[rand.Intn(len(volumeTypes))]
		details.Size = rand.Intn(101) + 10 // 10-1000 GB
		details.VolumeId = fmt.Sprintf("vol-%08x", rand.Int31())
		details.State = []string{"available", "in-use"}[rand.Intn(2)]
		details.Encrypted = rand.Intn(2) == 1
		details.Iops = rand.Intn(16001) + 4000 // 4000-20000
		details.MultiAttach = rand.Intn(2) == 1
		details.Tags = []map[string]string{
			{"Key": "Name", "Value": generateResourceName(resourceType)},
		}

	case "EBS Snapshot":
		details.VolumeId = fmt.Sprintf("vol-%08x", rand.Int31())
		details.Size = rand.Intn(101) + 10 // 100-1000 GB
		details.State = "completed"
		details.Encrypted = rand.Intn(2) == 1
		details.CreateDate = now.AddDate(0, 0, -rand.Intn(365)).Format("2006-01-02T15:04:05")

	case "DynamoDB Table":
		details.TableName = generateResourceName(resourceType)
		details.TableStatus = "ACTIVE"
		details.CreationDateTime = now.AddDate(0, 0, -rand.Intn(365)).Format("2006-01-02T15:04:05")
		details.TableSizeBytes = rand.Intn(10000000) + 1000000 // 1MB-11MB
		details.ItemCount = rand.Intn(10000) + 1000
		details.StreamEnabled = rand.Intn(2) == 1
		details.ProvisionedThroughput = map[string]int{
			"ReadCapacityUnits":  rand.Intn(91) + 10, // 10-100
			"WriteCapacityUnits": rand.Intn(46) + 5,  // 5-50
		}

	case "IAM User":
		details.CreateDate = now.AddDate(0, 0, -rand.Intn(730)).Format("2006-01-02T15:04:05") // Up to 2 years old
		details.PasswordLastUsed = now.AddDate(0, 0, -rand.Intn(365)).Format("2006-01-02T15:04:05")
		details.Groups = rand.Intn(4) + 1
		details.AttachedPolicies = rand.Intn(6) + 1

	case "IAM Role":
		details.CreateDate = now.AddDate(0, 0, -rand.Intn(730)).Format("2006-01-02T15:04:05")
		details.LastUsedDate = now.AddDate(0, 0, -rand.Intn(365)).Format("2006-01-02T15:04:05")
		details.AttachedPolicies = rand.Intn(6) + 1

	case "Elastic IP":
		details.PublicIp = fmt.Sprintf("54.%d.%d.%d", rand.Intn(256), rand.Intn(256), rand.Intn(256))
		details.AllocationId = fmt.Sprintf("eipalloc-%08x", rand.Int31())
		details.Domain = "vpc"

	case "OpenSearch Domain":
		details.Engine = "OpenSearch"
		details.EngineVersion = "2.5"
		details.ClusterConfig = map[string]interface{}{
			"InstanceType":           "t3.small.search",
			"InstanceCount":          2,
			"DedicatedMasterEnabled": false,
			"ZoneAwarenessEnabled":   true,
			"WarmEnabled":            false,
		}
		details.StorageEncrypted = true
		details.Tags = []map[string]string{
			{"Key": "Name", "Value": generateResourceName(resourceType)},
			{"Key": "Environment", "Value": []string{"dev", "stg", "prod"}[rand.Intn(3)]},
		}
	}

	return details
}

func calculateResourceCosts(resourceType string, details ResourceDetails) ResourceCosts {
	costs := ResourceCosts{}

	switch resourceType {
	case "EC2 Instance":
		hourlyRates := map[string]float64{
			"t3.micro":   0.0104,
			"t3.small":   0.0208,
			"t3.medium":  0.0416,
			"t3.large":   0.0832,
			"r5.xlarge":  0.252,
			"r5.2xlarge": 0.504,
			"r5.4xlarge": 1.008,
		}
		costs.Hourly = hourlyRates[details.InstanceType]
	case "RDS Instance":
		hourlyRates := map[string]float64{
			"db.t3.micro":  0.017,
			"db.t3.small":  0.034,
			"db.t3.medium": 0.068,
			"db.r5.large":  0.29,
		}
		costs.Hourly = hourlyRates[details.DBInstanceClass]
		if details.MultiAZ {
			costs.Hourly *= 2
		}
	case "ELB":
		costs.Hourly = 0.0225 // Base cost for Application Load Balancer
	case "EBS Volume":
		volumeRates := map[string]float64{
			"gp2": 0.10,
			"gp3": 0.08,
		}
		costs.Hourly = volumeRates[details.VolumeType] * float64(details.Size) / 730
	case "EBS Snapshot":
		costs.Hourly = float64(details.Size) * 0.05 / 730 // $0.05 per GB-month
	case "DynamoDB Table":
		readCost := float64(details.ProvisionedThroughput["ReadCapacityUnits"]) * 0.00013
		writeCost := float64(details.ProvisionedThroughput["WriteCapacityUnits"]) * 0.00065
		costs.Hourly = readCost + writeCost
	case "IAM User", "IAM Role":
		costs.Hourly = 0 // No direct cost
	case "Elastic IP":
		costs.Hourly = 0.005 // Cost when not attached to running instance
	case "OpenSearch Domain":
		costs.Hourly = 0.0138 // Base cost for t3.small.search
	}

	costs.Daily = costs.Hourly * 24
	costs.Monthly = costs.Daily * 30.44
	costs.Yearly = costs.Monthly * 12
	costs.Lifetime = costs.Yearly * float64(rand.Intn(3)+1) // 1-3 year lifetime

	return costs
}

func generateRandomRegions() []string {
	regions := []string{
		"us-east-1", "us-east-2", "us-west-1", "us-west-2",
		"eu-central-1", "eu-west-1", "ap-southeast-1",
	}
	numRegions := rand.Intn(5) + 3 // 3-7 regions
	selectedRegions := make([]string, numRegions)
	for i := 0; i < numRegions; i++ {
		selectedRegions[i] = regions[i]
	}
	return selectedRegions
}

func generateSampleData() ScanData {
	data := ScanData{
		AccountsAndRegions: map[string][]string{
			"123456789012": generateRandomRegions(),
			"234567890123": generateRandomRegions(),
			"345678901234": generateRandomRegions(),
		},
		AccountNames: map[string]string{
			"123456789012": "Production",
			"234567890123": "Staging",
			"345678901234": "Development",
		},
		UnusedResources: make([]Resource, 0),
		ScanMetrics: ScanMetrics{
			ResourcesByType: make(map[string]int),
		},
	}

	resourceTypes := []string{
		"EC2 Instance", "RDS Instance", "ELB", "EBS Volume",
		"EBS Snapshot", "DynamoDB Table", "IAM User", "IAM Role",
		"Elastic IP", "OpenSearch Domain",
	}

	for _, resourceType := range resourceTypes {
		numResources := rand.Intn(11) + 5 // 5-15 resources
		for i := 0; i < numResources; i++ {
			accountIDs := []string{"123456789012", "234567890123", "345678901234"}
			accountID := accountIDs[rand.Intn(len(accountIDs))]
			region := data.AccountsAndRegions[accountID][rand.Intn(len(data.AccountsAndRegions[accountID]))]
			details := generateResourceDetails(resourceType)

			resource := Resource{
				ID:                      fmt.Sprintf("%s-%08d", strings.ToLower(strings.ReplaceAll(resourceType, " ", "-")), rand.Intn(90000000)+10000000),
				Name:                    fmt.Sprintf("%s-%02d", generateResourceName(resourceType), i+1),
				Type:                    resourceType,
				AccountID:               accountID,
				AccountName:             data.AccountNames[accountID],
				Region:                  region,
				LastUsed:                time.Now().AddDate(0, 0, -rand.Intn(151)-30).Format("2006-01-02T15:04:05"), // 30-180 days ago
				EstimatedMonthlySavings: rand.Float64()*900 + 100,                                                   // 100-1000
				Reasons:                 generateResourceReasons(resourceType),
				Details:                 details,
				Costs:                   calculateResourceCosts(resourceType, details),
			}

			data.UnusedResources = append(data.UnusedResources, resource)
			data.ScanMetrics.ResourcesByType[resourceType]++
		}
	}

	data.ScanMetrics.TotalResources = len(data.UnusedResources)
	metrics := calculateScanMetrics(data.ScanMetrics.TotalResources)
	data.ScanMetrics = metrics

	// Calculate cost breakdown
	costBreakdown := make(map[string]CostBreakdown)
	totalCosts := ResourceCosts{}

	for _, resource := range data.UnusedResources {
		if _, exists := costBreakdown[resource.Type]; !exists {
			costBreakdown[resource.Type] = CostBreakdown{Type: resource.Type}
		}

		cb := costBreakdown[resource.Type]
		cb.Hourly += resource.Costs.Hourly
		cb.Daily += resource.Costs.Daily
		cb.Monthly += resource.Costs.Monthly
		cb.Yearly += resource.Costs.Yearly
		cb.Lifetime += resource.Costs.Lifetime
		costBreakdown[resource.Type] = cb

		totalCosts.Hourly += resource.Costs.Hourly
		totalCosts.Daily += resource.Costs.Daily
		totalCosts.Monthly += resource.Costs.Monthly
		totalCosts.Yearly += resource.Costs.Yearly
		totalCosts.Lifetime += resource.Costs.Lifetime
	}

	data.CostBreakdown = make([]CostBreakdown, 0, len(costBreakdown))
	for _, cb := range costBreakdown {
		data.CostBreakdown = append(data.CostBreakdown, cb)
	}
	data.TotalCosts = totalCosts

	return data
}

func calculateScanMetrics(totalResources int) ScanMetrics {
	metrics := ScanMetrics{
		CompletedAt:    time.Now().Format("2006-01-02T15:04:05"),
		TotalResources: totalResources,
		MaxWorkers:     10,
		TotalRunTime:   "1m0s",
	}

	metrics.PeakWorkers = rand.Intn(4) + 7 // 7-10
	metrics.WorkerUtilization = float64(metrics.PeakWorkers) / float64(metrics.MaxWorkers) * 100
	metrics.TasksPerSecond = rand.Float64()*3 + 5 // 5-8
	metrics.TotalScans = int(metrics.TasksPerSecond * 60)
	metrics.FailedScans = rand.Intn(3) + 1
	metrics.CompletedScans = metrics.TotalScans - metrics.FailedScans
	metrics.AvgExecutionTimeMs = (60.0 * 1000) / float64(metrics.TotalScans)
	metrics.TotalCost = rand.Float64()*10000 + 5000 // 5000-15000

	return metrics
}

func main() {
	rand.Seed(time.Now().UnixNano())

	data := generateSampleData()
	outputFile := "examples/sample_scan_data.json"

	// Ensure the directory exists
	if err := os.MkdirAll(filepath.Dir(outputFile), 0755); err != nil {
		fmt.Printf("Error creating directory: %v\n", err)
		return
	}

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		fmt.Printf("Error marshaling JSON: %v\n", err)
		return
	}

	if err := os.WriteFile(outputFile, jsonData, 0644); err != nil {
		fmt.Printf("Error writing file: %v\n", err)
		return
	}

	fmt.Printf("Sample data written to %s\n", outputFile)
}
