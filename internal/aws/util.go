package aws

import (
	"fmt"
	"strings"
	"time"
)

// Max returns the larger of a and b
func Max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// FormatTimeDifference formats the duration between now and a given time into a human-readable string
// showing years, months, and days. If the time pointer is nil, returns "Never used".
// Example output: "has not been used in 2 years 3 months 5 days"
func FormatTimeDifference(now time.Time, lastUsed *time.Time) string {
	if lastUsed == nil {
		return "Never used"
	}

	duration := now.Sub(*lastUsed)
	totalDays := int(duration.Hours() / 24)

	years := totalDays / 365
	remainingDays := totalDays % 365
	months := remainingDays / 30
	days := remainingDays % 30

	parts := make([]string, 0, 3)
	if years > 0 {
		if years == 1 {
			parts = append(parts, "1 year")
		} else {
			parts = append(parts, fmt.Sprintf("%d years", years))
		}
	}
	if months > 0 {
		if months == 1 {
			parts = append(parts, "1 month")
		} else {
			parts = append(parts, fmt.Sprintf("%d months", months))
		}
	}
	if days > 0 || len(parts) == 0 {
		if days == 1 {
			parts = append(parts, "1 day")
		} else {
			parts = append(parts, fmt.Sprintf("%d days", days))
		}
	}

	return fmt.Sprintf("%s", strings.Join(parts, " "))
}
