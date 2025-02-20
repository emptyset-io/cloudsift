package html

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cloudsift/internal/aws"
	"cloudsift/internal/logging"
)

//go:embed assets/* templates/*
var content embed.FS

// TemplateData represents the data structure passed to the HTML template
type TemplateData struct {
	AccountsAndRegions map[string][]string
	AccountNames       map[string]string
	ResourceTypeCounts map[string]int
	CombinedCosts      map[string]map[string]interface{}
	ScanMetrics        ScanMetrics
	Resources          []Resource
	Styles             template.CSS
	Scripts            template.JS
}

// ScanMetrics represents metrics about the scan operation
type ScanMetrics struct {
	TotalScans        int       `json:"total_scans"`
	AvgScansPerSecond float64   `json:"avg_scans_per_second"`
	TotalRunTime      float64   `json:"total_run_time"`
	CompletedAt       time.Time `json:"completed_at"`
}

// Resource represents a single resource in the scan results
type Resource struct {
	AccountID    string
	AccountName  string
	Region       string
	ResourceType string
	Name         string
	ResourceID   string
	Reason       string
	Details      string
}

// WriteHTML writes scan results to an HTML file
func WriteHTML(results []aws.ScanResult, outputPath string, metrics ScanMetrics) error {
	// Read template files
	tmpl, err := template.New("scan_report.html").Funcs(template.FuncMap{
		"join": strings.Join,
		"truncate": func(s string, n int) string {
			if len(s) <= n {
				return s
			}
			return s[:n]
		},
		"formatTime": func(t time.Time) string {
			return t.Format("January 2, 2006 at 3:04 PM MST")
		},
		"formatHourlyCost":   formatHourlyCost,
		"formatDailyCost":    formatDailyCost,
		"formatMonthlyCost":  formatMonthlyCost,
		"formatYearlyCost":   formatYearlyCost,
		"formatLifetimeCost": formatLifetimeCost,
		"formatDuration":     formatDuration,
	}).ParseFS(content, "templates/scan_report.html")
	if err != nil {
		return fmt.Errorf("error parsing template: %v", err)
	}

	// Read assets
	styles, err := content.ReadFile("assets/styles.css")
	if err != nil {
		return fmt.Errorf("error reading styles: %v", err)
	}

	scripts, err := content.ReadFile("assets/scripts.js")
	if err != nil {
		return fmt.Errorf("error reading scripts: %v", err)
	}

	// Process the scan results
	data := processResults(results)
	data.ScanMetrics = metrics
	data.ScanMetrics.CompletedAt = time.Now()
	data.Styles = template.CSS(styles)
	data.Scripts = template.JS(scripts)

	// Create output directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("error creating output directory: %v", err)
	}

	// Create output file
	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("error creating output file: %v", err)
	}
	defer f.Close()

	// Execute template
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("error executing template: %v", err)
	}

	// Write to file
	if _, err := io.Copy(f, &buf); err != nil {
		return fmt.Errorf("error writing to file: %v", err)
	}

	return nil
}

func processResults(results []aws.ScanResult) TemplateData {
	accountsAndRegions := make(map[string][]string)
	accountNames := make(map[string]string)
	resourceTypeCounts := make(map[string]int)
	combinedCosts := make(map[string]map[string]interface{})
	resources := make([]Resource, 0, len(results))

	startTime := time.Now()
	totalScans := len(results)

	// First pass: identify all resource types with cost data
	resourceTypesWithCosts := make(map[string]bool)
	for _, result := range results {
		if len(result.Cost) > 0 {
			resourceTypesWithCosts[result.ResourceType] = true
		}
	}

	// Initialize cost maps for each resource type that has costs
	for resourceType := range resourceTypesWithCosts {
		combinedCosts[resourceType] = map[string]interface{}{
			"hourly_rate":  float64(0),
			"daily_rate":   float64(0),
			"monthly_rate": float64(0),
			"yearly_rate":  float64(0),
			"lifetime":     float64(0),
		}
	}

	logging.Debug("Starting to process results", map[string]interface{}{
		"total_results":             len(results),
		"resource_types_with_costs": resourceTypesWithCosts,
	})

	for _, result := range results {
		// Process accounts and regions
		var accountID, accountName, region string

		// Try both camelCase and snake_case keys for backward compatibility
		if id, ok := result.Details["AccountId"].(string); ok {
			accountID = id
		} else if id, ok := result.Details["account_id"].(string); ok {
			accountID = id
		}

		if name, ok := result.Details["AccountName"].(string); ok {
			accountName = name
		} else if name, ok := result.Details["account_name"].(string); ok {
			accountName = name
		}

		if reg, ok := result.Details["Region"].(string); ok {
			region = reg
		} else if reg, ok := result.Details["region"].(string); ok {
			region = reg
		}

		// Store account name mapping
		if accountName != "" {
			accountNames[accountID] = accountName
		}

		// Process resource type counts
		resourceTypeCounts[result.ResourceType]++

		// Debug cost information for each result
		logging.Debug("Processing result costs", map[string]interface{}{
			"resource_type": result.ResourceType,
			"resource_id":   result.ResourceID,
			"cost":          result.Cost,
		})

		// Process costs based on resource type
		if result.Cost != nil {
			logging.Debug("Found cost data", map[string]interface{}{
				"resource_type": result.ResourceType,
				"resource_id":   result.ResourceID,
				"cost_details":  result.Cost,
			})

			switch result.ResourceType {
			case "EC2 Instances":
				// For EC2 instances, extract the total costs from the nested structure
				if totalCosts, ok := result.Cost["total"].(*aws.CostBreakdown); ok {
					logging.Debug("Adding EC2 instance total costs", map[string]interface{}{
						"resource_id": result.ResourceID,
						"total_costs": totalCosts,
					})
					addCostBreakdown(combinedCosts[result.ResourceType], result.Cost)
				} else {
					logging.Debug("No total costs found in EC2 instance", map[string]interface{}{
						"resource_id": result.ResourceID,
						"cost_data":   result.Cost,
					})
				}
			case "EBS Volumes", "EBS Snapshots":
				// For storage resources, add all costs
				logging.Debug("Adding storage costs", map[string]interface{}{
					"resource_type": result.ResourceType,
					"resource_id":   result.ResourceID,
					"costs":         result.Cost,
				})
				addCostBreakdown(combinedCosts[result.ResourceType], result.Cost)
			case "Elastic IPs":
				// For Elastic IPs, add all costs except lifetime which stays as N/A
				logging.Debug("Adding Elastic IP costs", map[string]interface{}{
					"resource_id": result.ResourceID,
					"costs":       result.Cost,
				})
				addCostBreakdownExceptLifetime(combinedCosts[result.ResourceType], result.Cost)
			case "Load Balancers":
				// For Load Balancers, add all costs
				addCostBreakdown(combinedCosts[result.ResourceType], result.Cost)
			default:
				// For other resource types, add all costs
				addCostBreakdown(combinedCosts[result.ResourceType], result.Cost)
			}

			// Debug combined costs after adding
			logging.Debug("Current combined costs", map[string]interface{}{
				"resource_type": result.ResourceType,
				"costs":         combinedCosts[result.ResourceType],
			})
		} else {
			logging.Debug("No cost data found", map[string]interface{}{
				"resource_type": result.ResourceType,
				"resource_id":   result.ResourceID,
			})
		}

		// Process resource details
		detailsJSON, err := json.MarshalIndent(result.Details, "", "  ")
		logging.Debug("Failed to marshal details", map[string]interface{}{
			"resource_type": result.ResourceType,
			"resource_id":   result.ResourceID,
		})
		if err != nil {
			logging.Error("Failed to marshal details", err, map[string]interface{}{
				"resource_type": result.ResourceType,
				"resource_id":   result.ResourceID,
			})
			continue
		}
		resources = append(resources, Resource{
			AccountID:    accountID,
			AccountName:  accountName,
			Region:       region,
			ResourceType: result.ResourceType,
			Name:         result.ResourceName,
			ResourceID:   result.ResourceID,
			Reason:       result.Reason,
			Details:      string(detailsJSON),
		})

		// Process accounts and regions
		if accountID != "" && region != "" {
			if regions, ok := accountsAndRegions[accountID]; ok {
				if !contains(regions, region) {
					accountsAndRegions[accountID] = append(regions, region)
				}
			} else {
				accountsAndRegions[accountID] = []string{region}
			}
		}
	}

	endTime := time.Now()
	totalRunTime := endTime.Sub(startTime).Seconds()
	avgScansPerSecond := float64(totalScans) / totalRunTime

	// Debug final combined costs
	for resourceType, costs := range combinedCosts {
		logging.Debug("Final costs for resource type", map[string]interface{}{
			"resource_type": resourceType,
			"costs":         costs,
		})
	}

	return TemplateData{
		AccountsAndRegions: accountsAndRegions,
		AccountNames:       accountNames,
		ResourceTypeCounts: resourceTypeCounts,
		CombinedCosts:      combinedCosts,
		Resources:          resources,
		ScanMetrics: ScanMetrics{
			TotalScans:        totalScans,
			AvgScansPerSecond: avgScansPerSecond,
			TotalRunTime:      totalRunTime,
		},
	}
}

func addCostBreakdown(target map[string]interface{}, source map[string]interface{}) {
	// First, try to get the total cost breakdown
	totalCost, ok := source["total"].(*aws.CostBreakdown)
	if !ok {
		logging.Debug("No total cost breakdown found", map[string]interface{}{
			"source": source,
		})
		return
	}

	// Map the fields from the cost breakdown
	if totalCost.HourlyRate != 0 {
		if current, ok := target["hourly_rate"].(float64); ok {
			target["hourly_rate"] = current + totalCost.HourlyRate
		} else {
			target["hourly_rate"] = totalCost.HourlyRate
		}
	}

	if totalCost.DailyRate != 0 {
		if current, ok := target["daily_rate"].(float64); ok {
			target["daily_rate"] = current + totalCost.DailyRate
		} else {
			target["daily_rate"] = totalCost.DailyRate
		}
	}

	if totalCost.MonthlyRate != 0 {
		if current, ok := target["monthly_rate"].(float64); ok {
			target["monthly_rate"] = current + totalCost.MonthlyRate
		} else {
			target["monthly_rate"] = totalCost.MonthlyRate
		}
	}

	if totalCost.YearlyRate != 0 {
		if current, ok := target["yearly_rate"].(float64); ok {
			target["yearly_rate"] = current + totalCost.YearlyRate
		} else {
			target["yearly_rate"] = totalCost.YearlyRate
		}
	}

	if totalCost.Lifetime != nil {
		if current, ok := target["lifetime"].(float64); ok {
			target["lifetime"] = current + *totalCost.Lifetime
		} else {
			target["lifetime"] = *totalCost.Lifetime
		}
	}

	// Debug the values after adding
	logging.Debug("Cost breakdown after adding", map[string]interface{}{
		"target": target,
		"source": source,
	})
}

// addCostBreakdownExceptLifetime adds all cost fields except lifetime
func addCostBreakdownExceptLifetime(target map[string]interface{}, source map[string]interface{}) {
	// First, try to get the total cost breakdown
	totalCost, ok := source["total"].(*aws.CostBreakdown)
	if !ok {
		logging.Debug("No total cost breakdown found or wrong type", map[string]interface{}{
			"source": source,
		})
		return
	}

	// Map the fields from the cost breakdown (excluding lifetime)
	if totalCost.HourlyRate != 0 {
		if current, ok := target["hourly_rate"].(float64); ok {
			target["hourly_rate"] = current + totalCost.HourlyRate
		} else {
			target["hourly_rate"] = totalCost.HourlyRate
		}
	}

	if totalCost.DailyRate != 0 {
		if current, ok := target["daily_rate"].(float64); ok {
			target["daily_rate"] = current + totalCost.DailyRate
		} else {
			target["daily_rate"] = totalCost.DailyRate
		}
	}

	if totalCost.MonthlyRate != 0 {
		if current, ok := target["monthly_rate"].(float64); ok {
			target["monthly_rate"] = current + totalCost.MonthlyRate
		} else {
			target["monthly_rate"] = totalCost.MonthlyRate
		}
	}

	if totalCost.YearlyRate != 0 {
		if current, ok := target["yearly_rate"].(float64); ok {
			target["yearly_rate"] = current + totalCost.YearlyRate
		} else {
			target["yearly_rate"] = totalCost.YearlyRate
		}
	}

	// Debug the values after adding
	logging.Debug("Cost breakdown after adding (except lifetime)", map[string]interface{}{
		"target": target,
		"source": source,
	})
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// formatCost formats a cost value with commas and appropriate decimal places
func formatCost(cost float64) string {
	if cost == 0 {
		return "0.00"
	}

	// For numbers between 0 and 1
	if cost > 0 && cost < 1 {
		// Get scientific notation to find magnitude
		sci := fmt.Sprintf("%.10e", cost)
		var mantissa float64
		var exponent int
		if _, err := fmt.Sscanf(sci, "%e", &mantissa); err != nil {
			return fmt.Sprintf("%.2f", cost)
		}
		if _, err := fmt.Sscanf(sci[strings.IndexByte(sci, 'e')+1:], "%d", &exponent); err != nil {
			return fmt.Sprintf("%.2f", cost)
		}

		// If exponent is -1 or -2, use 2 decimal places
		if exponent >= -2 {
			return fmt.Sprintf("%.2f", cost)
		}

		// For smaller numbers, show enough decimals to see first two significant digits
		precision := -exponent + 1
		str := fmt.Sprintf("%.*f", precision, cost)

		// Remove trailing zeros after significant digits
		for i := len(str) - 1; i >= 0; i-- {
			if str[i] != '0' {
				if str[i] == '.' {
					return str + "00"
				}
				return str[:i+1]
			}
		}
		return str
	}

	// For numbers >= 1, use 2 decimals
	str := fmt.Sprintf("%.2f", cost)

	// Split into integer and decimal parts
	parts := strings.Split(str, ".")
	integer := parts[0]
	decimal := parts[1]

	// Add commas to integer part
	var result []byte
	length := len(integer)
	for i := 0; i < length; i++ {
		if i > 0 && (length-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, integer[i])
	}

	// Combine with decimal part
	return string(result) + "." + decimal
}

// Helper functions for specific cost types
func formatHourlyCost(cost float64) string {
	return formatCost(cost)
}

func formatDailyCost(cost float64) string {
	return formatCost(cost)
}

func formatMonthlyCost(cost float64) string {
	return formatCost(cost)
}

func formatYearlyCost(cost float64) string {
	return formatCost(cost)
}

func formatLifetimeCost(cost float64) string {
	return formatCost(cost)
}

func formatDuration(seconds float64) string {
	if seconds < 1 {
		return fmt.Sprintf("%.6f seconds", seconds)
	}
	if seconds < 60 {
		return fmt.Sprintf("%.2f seconds", seconds)
	}
	minutes := int(seconds / 60)
	remainingSeconds := seconds - float64(minutes*60)
	return fmt.Sprintf("%d minutes %.2f seconds", minutes, remainingSeconds)
}
