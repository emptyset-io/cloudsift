package calculators

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/pricing"

	"cloudsift/internal/aws/pricing/models"
)

// EBSCalculator handles EBS volume cost calculations
type EBSCalculator struct {
	BaseCalculator
}

// GetPricingFilters returns the pricing filters for EBS volumes
func (ec *EBSCalculator) GetPricingFilters(config models.ResourceCostConfig, location string) ([]*pricing.Filter, error) {
	if err := ec.ValidateSize(config.ResourceSize, "int64"); err != nil {
		return nil, err
	}

	return []*pricing.Filter{
		{
			Type:  aws.String("TERM_MATCH"),
			Field: aws.String("servicecode"),
			Value: aws.String("AmazonEC2"),
		},
		{
			Type:  aws.String("TERM_MATCH"),
			Field: aws.String("productFamily"),
			Value: aws.String("Storage"),
		},
		{
			Type:  aws.String("TERM_MATCH"),
			Field: aws.String("volumeApiName"),
			Value: aws.String(config.VolumeType),
		},
		{
			Type:  aws.String("TERM_MATCH"),
			Field: aws.String("location"),
			Value: aws.String(location),
		},
	}, nil
}

// CalculateCost calculates the cost for an EBS volume
func (ec *EBSCalculator) CalculateCost(pricePerUnit float64, config models.ResourceCostConfig) (*models.CostBreakdown, error) {
	if err := ec.ValidateSize(config.ResourceSize, "int64"); err != nil {
		return nil, err
	}

	size := config.ResourceSize.(int64)
	monthlyPrice := float64(size) * pricePerUnit // Price per GB-month
	hourlyPrice := monthlyPrice / 730            // Convert to hourly (730 hours in a month)

	return ec.CalculateRates(hourlyPrice), nil
}
