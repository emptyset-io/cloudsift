package html

import (
	"bytes"
	"embed"
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
	TotalHourly       float64   `json:"total_hourly"`
	TotalDaily        float64   `json:"total_daily"`
	TotalMonthly      float64   `json:"total_monthly"`
	TotalYearly       float64   `json:"total_yearly"`
	TotalLifetime     float64   `json:"total_lifetime"`
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
	data := TemplateData{
		AccountsAndRegions: make(map[string][]string),
		AccountNames:       make(map[string]string),
		ResourceTypeCounts: make(map[string]int),
		CombinedCosts:     make(map[string]map[string]interface{}),
		Resources:         make([]Resource, 0),
	}

	// Initialize metrics
	var totalHourly, totalDaily, totalMonthly, totalYearly, totalLifetime float64

	startTime := time.Now()
	totalScans := len(results)
	avgScansPerSecond := float64(totalScans) / time.Since(startTime).Seconds()
	totalRunTime := time.Since(startTime).Seconds()

	// Process each result
	for _, result := range results {
		// Get account and region info from Details
		accountID, _ := result.Details["account_id"].(string)
		accountName, _ := result.Details["account_name"].(string)
		region, _ := result.Details["region"].(string)

		// Add account and region info
		if accountID != "" && region != "" {
			if !contains(data.AccountsAndRegions[accountID], region) {
				data.AccountsAndRegions[accountID] = append(data.AccountsAndRegions[accountID], region)
			}
		}

		// Store account name mapping
		if accountID != "" && accountName != "" {
			data.AccountNames[accountID] = accountName
		}

		// Add resource type count
		data.ResourceTypeCounts[result.ResourceType]++

		// Add costs to combined costs
		if result.Cost != nil {
			if _, exists := data.CombinedCosts[result.ResourceType]; !exists {
				data.CombinedCosts[result.ResourceType] = make(map[string]interface{})
				data.CombinedCosts[result.ResourceType]["hourly_rate"] = 0.0
				data.CombinedCosts[result.ResourceType]["daily_rate"] = 0.0
				data.CombinedCosts[result.ResourceType]["monthly_rate"] = 0.0
				data.CombinedCosts[result.ResourceType]["yearly_rate"] = 0.0
				data.CombinedCosts[result.ResourceType]["lifetime"] = 0.0
			}

			// Update resource type totals
			if hourly, ok := result.Cost["hourly_rate"].(float64); ok {
				current, _ := data.CombinedCosts[result.ResourceType]["hourly_rate"].(float64)
				data.CombinedCosts[result.ResourceType]["hourly_rate"] = current + hourly
				totalHourly += hourly
			}
			if daily, ok := result.Cost["daily_rate"].(float64); ok {
				current, _ := data.CombinedCosts[result.ResourceType]["daily_rate"].(float64)
				data.CombinedCosts[result.ResourceType]["daily_rate"] = current + daily
				totalDaily += daily
			}
			if monthly, ok := result.Cost["monthly_rate"].(float64); ok {
				current, _ := data.CombinedCosts[result.ResourceType]["monthly_rate"].(float64)
				data.CombinedCosts[result.ResourceType]["monthly_rate"] = current + monthly
				totalMonthly += monthly
			}
			if yearly, ok := result.Cost["yearly_rate"].(float64); ok {
				current, _ := data.CombinedCosts[result.ResourceType]["yearly_rate"].(float64)
				data.CombinedCosts[result.ResourceType]["yearly_rate"] = current + yearly
				totalYearly += yearly
			}
			if lifetime, ok := result.Cost["lifetime"].(float64); ok && result.ResourceType != "Elastic IPs" && result.ResourceType != "Load Balancers" {
				current, _ := data.CombinedCosts[result.ResourceType]["lifetime"].(float64)
				data.CombinedCosts[result.ResourceType]["lifetime"] = current + lifetime
				totalLifetime += lifetime
			}
		}

		// Add to resources list
		resourceName, _ := result.Details["name"].(string)
		resourceID, _ := result.Details["id"].(string)

		data.Resources = append(data.Resources, Resource{
			AccountID:    accountID,
			AccountName:  accountName,
			Region:      region,
			ResourceType: result.ResourceType,
			Name:        resourceName,
			ResourceID:  resourceID,
			Reason:      result.Reason,
			Details:     fmt.Sprintf("%+v", result.Details),
		})
	}

	// Set the totals in metrics
	data.ScanMetrics = ScanMetrics{
		TotalScans:        totalScans,
		AvgScansPerSecond: avgScansPerSecond,
		TotalRunTime:      totalRunTime,
		CompletedAt:       time.Now(),
		TotalHourly:       totalHourly,
		TotalDaily:        totalDaily,
		TotalMonthly:      totalMonthly,
		TotalYearly:       totalYearly,
		TotalLifetime:     totalLifetime,
	}

	return data
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
