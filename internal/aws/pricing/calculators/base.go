package calculators

import (
	"fmt"
	"math"

	"cloudsift/internal/aws/pricing/models"
)

// BaseCalculator provides common functionality for all cost calculators
type BaseCalculator struct{}

// RoundCost rounds a cost value to 4 decimal places
func (bc *BaseCalculator) RoundCost(cost float64) float64 {
	return math.Round(cost*10000) / 10000
}

// CalculateRates calculates standard rates from hourly price
func (bc *BaseCalculator) CalculateRates(hourlyPrice float64) *models.CostBreakdown {
	dailyPrice := hourlyPrice * 24
	monthlyPrice := dailyPrice * 30 // Approximate
	yearlyPrice := dailyPrice * 365

	return &models.CostBreakdown{
		HourlyRate:   bc.RoundCost(hourlyPrice),
		DailyRate:    bc.RoundCost(dailyPrice),
		MonthlyRate:  bc.RoundCost(monthlyPrice),
		YearlyRate:   bc.RoundCost(yearlyPrice),
		HoursRunning: nil,
		Lifetime:     nil,
	}
}

// ValidateSize ensures the resource size is of the expected type
func (bc *BaseCalculator) ValidateSize(size interface{}, expectedType string) error {
	switch expectedType {
	case "int64":
		if _, ok := size.(int64); !ok {
			return fmt.Errorf("invalid resource size type, expected int64, got %T", size)
		}
	case "string":
		if _, ok := size.(string); !ok {
			return fmt.Errorf("invalid resource size type, expected string, got %T", size)
		}
	default:
		return fmt.Errorf("unsupported size type validation: %s", expectedType)
	}
	return nil
}
