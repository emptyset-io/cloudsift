package models

import "time"

// CostBreakdown represents the cost of a resource over different time periods
type CostBreakdown struct {
	HourlyRate   float64  `json:"hourly_rate"`
	DailyRate    float64  `json:"daily_rate"`
	MonthlyRate  float64  `json:"monthly_rate"`
	YearlyRate   float64  `json:"yearly_rate"`
	HoursRunning *float64 `json:"hours_running,omitempty"`
	Lifetime     *float64 `json:"lifetime,omitempty"`
}

// ResourceCostConfig holds configuration for resource cost calculation
type ResourceCostConfig struct {
	ResourceType  string
	ResourceSize  interface{} // Can be int64 for storage sizes or string for instance types
	Region        string
	CreationTime  time.Time
	VolumeType    string  // Volume type for EBS (e.g., "gp2", "gp3", "io1")
	LBType        string  // Load balancer type (e.g., "application", "network")
	ProcessedGB   float64 // Processed GB for load balancers
	InstanceCount int64   // Instance count for OpenSearch
	StorageSize   int64   // Storage size for OpenSearch
	MultiAZ       bool    // Multi-AZ for RDS
	Engine        string  // Database engine for RDS
}
