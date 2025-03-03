package calculators

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/pricing"

	"cloudsift/internal/aws/pricing/models"
)

// OpenSearchCalculator handles OpenSearch cost calculations
type OpenSearchCalculator struct {
	BaseCalculator
}

// GetPricingFilters returns the pricing filters for OpenSearch
func (oc *OpenSearchCalculator) GetPricingFilters(config models.ResourceCostConfig, location string) ([]*pricing.Filter, error) {
	if err := oc.ValidateSize(config.ResourceSize, "string"); err != nil {
		return nil, err
	}

	instanceType := config.ResourceSize.(string)
	return []*pricing.Filter{
		{
			Type:  aws.String("TERM_MATCH"),
			Field: aws.String("servicecode"),
			Value: aws.String("AmazonES"),
		},
		{
			Type:  aws.String("TERM_MATCH"),
			Field: aws.String("location"),
			Value: aws.String(location),
		},
		{
			Type:  aws.String("TERM_MATCH"),
			Field: aws.String("instanceType"),
			Value: aws.String(instanceType),
		},
	}, nil
}

// CalculateCost calculates the cost for OpenSearch
func (oc *OpenSearchCalculator) CalculateCost(hourlyPrice float64, config models.ResourceCostConfig) *models.CostBreakdown {
	// Adjust price based on instance count
	totalHourlyPrice := hourlyPrice * float64(config.InstanceCount)
	return oc.CalculateRates(totalHourlyPrice)
}
