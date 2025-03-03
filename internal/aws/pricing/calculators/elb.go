package calculators

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/pricing"

	"cloudsift/internal/aws/pricing/models"
)

// ELBCalculator handles Elastic Load Balancer cost calculations
type ELBCalculator struct {
	BaseCalculator
}

// GetPricingFilters returns the pricing filters for ELB
func (ec *ELBCalculator) GetPricingFilters(config models.ResourceCostConfig, location string) ([]*pricing.Filter, error) {
	return []*pricing.Filter{
		{
			Type:  aws.String("TERM_MATCH"),
			Field: aws.String("servicecode"),
			Value: aws.String("AmazonEC2"),
		},
		{
			Type:  aws.String("TERM_MATCH"),
			Field: aws.String("productFamily"),
			Value: aws.String("Load Balancer"),
		},
		{
			Type:  aws.String("TERM_MATCH"),
			Field: aws.String("location"),
			Value: aws.String(location),
		},
		{
			Type:  aws.String("TERM_MATCH"),
			Field: aws.String("usagetype"),
			Value: aws.String("LoadBalancerUsage"),
		},
	}, nil
}

// CalculateCost calculates the cost for an ELB
func (ec *ELBCalculator) CalculateCost(hourlyPrice float64) *models.CostBreakdown {
	return ec.CalculateRates(hourlyPrice)
}
