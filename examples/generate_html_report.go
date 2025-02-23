package main

import (
	"cloudsift/internal/aws"
	"cloudsift/internal/output/html"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"
)

type ResourceDistItem struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

type ResourceCost struct {
	Type     string  `json:"type"`
	Hourly   float64 `json:"hourly"`
	Daily    float64 `json:"daily"`
	Monthly  float64 `json:"monthly"`
	Yearly   float64 `json:"yearly"`
	Lifetime float64 `json:"lifetime"`
}

type SampleData struct {
	ScanMetrics struct {
		CompletedAt        string  `json:"CompletedAt"`
		TotalResources     int     `json:"TotalResources"`
		TotalCost          float64 `json:"TotalCost"`
		TotalScans         int64   `json:"TotalScans"`
		CompletedScans     int64   `json:"CompletedScans"`
		FailedScans        int64   `json:"FailedScans"`
		TasksPerSecond     float64 `json:"TasksPerSecond"`
		AvgExecutionTimeMs float64 `json:"AvgExecutionTimeMs"`
		WorkerUtilization  float64 `json:"WorkerUtilization"`
		PeakWorkers        int     `json:"PeakWorkers"`
		MaxWorkers         int     `json:"MaxWorkers"`
		TotalRunTime       string  `json:"TotalRunTime"`
	} `json:"ScanMetrics"`
	AccountsAndRegions   map[string][]string `json:"AccountsAndRegions"`
	AccountNames         map[string]string   `json:"AccountNames"`
	ResourceDistribution []ResourceDistItem  `json:"ResourceDistribution"`
	UnusedResources      []struct {
		ID          string                 `json:"id"`
		Name        string                 `json:"name"`
		Type        string                 `json:"type"`
		AccountID   string                 `json:"account_id"`
		AccountName string                 `json:"account_name"`
		Region      string                 `json:"region"`
		LastUsed    string                 `json:"last_used"`
		Reasons     []string               `json:"reasons"`
		Details     map[string]interface{} `json:"details"`
	} `json:"UnusedResources"`
	CostBreakdown []ResourceCost     `json:"CostBreakdown"`
	TotalCosts    map[string]float64 `json:"TotalCosts"`
}

// Float64Ptr returns a pointer to the given float64 value
func Float64Ptr(v float64) *float64 {
	return &v
}

func convertToScanResults(data *SampleData) []aws.ScanResult {
	var results []aws.ScanResult

	// Convert unused resources to scan results
	for _, res := range data.UnusedResources {
		// Use the details directly from the JSON
		details := res.Details

		// Get the first reason if available, otherwise use a default
		reason := "No reason provided"
		if len(res.Reasons) > 0 {
			reason = res.Reasons[0]
		}

		// Add cost information
		var resourceCost ResourceCost
		for _, cost := range data.CostBreakdown {
			if cost.Type == res.Type {
				resourceCost = cost
				break
			}
		}

		details["CostBreakdown"] = map[string]float64{
			"Hourly":   resourceCost.Hourly,
			"Daily":    resourceCost.Daily,
			"Monthly":  resourceCost.Monthly,
			"Yearly":   resourceCost.Yearly,
			"Lifetime": resourceCost.Lifetime,
		}

		result := aws.ScanResult{
			ResourceType: res.Type,
			ResourceName: res.Name,
			ResourceID:   res.ID,
			AccountID:    res.AccountID,
			AccountName:  res.AccountName,
			Reason:       reason,
			Details:      details,
		}
		results = append(results, result)
	}

	return results
}

func main() {
	// Read the sample data JSON file
	data, err := os.ReadFile("examples/sample_scan_data.json")
	if err != nil {
		log.Fatalf("Error reading sample data: %v", err)
	}

	// Parse the JSON into our sample data structure
	var sampleData SampleData
	if err := json.Unmarshal(data, &sampleData); err != nil {
		log.Fatalf("Error parsing JSON: %v", err)
	}

	// Parse the completed time
	completedAt, err := time.Parse("2006-01-02T15:04:05.999999", sampleData.ScanMetrics.CompletedAt)
	if err != nil {
		log.Fatalf("Error parsing completed time: %v", err)
	}
	completedAt = completedAt.UTC() // Ensure UTC timezone

	// Convert the data to scan results
	results := convertToScanResults(&sampleData)

	// Create output directory if it doesn't exist
	outputDir := "examples/output"
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		log.Fatalf("Error creating output directory: %v", err)
	}

	// Generate the HTML report using the existing html package
	outputPath := filepath.Join(outputDir, "sample_report.html")
	metrics := html.ScanMetrics{
		CompletedAt:        completedAt,
		TotalScans:         int(sampleData.ScanMetrics.TotalScans),
		CompletedScans:     sampleData.ScanMetrics.CompletedScans,
		FailedScans:        sampleData.ScanMetrics.FailedScans,
		TasksPerSecond:     sampleData.ScanMetrics.TasksPerSecond,
		AvgExecutionTimeMs: int64(sampleData.ScanMetrics.AvgExecutionTimeMs),
		WorkerUtilization:  sampleData.ScanMetrics.WorkerUtilization,
		PeakWorkers:        int64(sampleData.ScanMetrics.PeakWorkers),
		MaxWorkers:         sampleData.ScanMetrics.MaxWorkers,
		TotalRunTime:       60.0, // 1 minute in seconds
		AvgScansPerSecond:  float64(sampleData.ScanMetrics.CompletedScans) / 60.0,
	}

	// Add cost data to each resource
	for i := range results {
		// Create the Cost map if it doesn't exist
		if results[i].Cost == nil {
			results[i].Cost = make(map[string]interface{})
		}

		// Find matching cost breakdown
		var resourceCost ResourceCost
		for _, cost := range sampleData.CostBreakdown {
			if cost.Type == results[i].ResourceType {
				resourceCost = cost
				break
			}
		}

		// Add cost breakdown
		lifetime := resourceCost.Lifetime
		results[i].Cost["total"] = &aws.CostBreakdown{
			HourlyRate:  resourceCost.Hourly,
			DailyRate:   resourceCost.Daily,
			MonthlyRate: resourceCost.Monthly,
			YearlyRate:  resourceCost.Yearly,
			Lifetime:    &lifetime,
		}
	}

	// Write the HTML report
	if err := html.WriteHTML(results, outputPath, metrics); err != nil {
		log.Fatalf("Error generating HTML report: %v", err)
	}

	log.Printf("Report generated at: %s", outputPath)
}
