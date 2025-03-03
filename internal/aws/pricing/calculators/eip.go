package calculators

import "cloudsift/internal/aws/pricing/models"

// EIPCalculator handles Elastic IP cost calculations
type EIPCalculator struct {
	BaseCalculator
}

// CalculateCost calculates the cost for an unattached Elastic IP
func (ec *EIPCalculator) CalculateCost() *models.CostBreakdown {
	// Elastic IPs have a flat rate of $0.005 per hour when not attached
	return ec.CalculateRates(0.005)
}
