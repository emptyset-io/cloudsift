package calculators

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/pricing"

	"cloudsift/internal/aws/pricing/models"
)

// EC2Calculator handles EC2 instance cost calculations
type EC2Calculator struct {
	BaseCalculator
}

// GetPricingFilters returns the pricing filters for EC2 instances
func (ec *EC2Calculator) GetPricingFilters(config models.ResourceCostConfig, location string) ([]*pricing.Filter, error) {
	if err := ec.ValidateSize(config.ResourceSize, "string"); err != nil {
		return nil, err
	}

	instanceType := config.ResourceSize.(string)
	return []*pricing.Filter{
		{
			Type:  aws.String("TERM_MATCH"),
			Field: aws.String("operatingSystem"),
			Value: aws.String("Linux"),
		},
		{
			Type:  aws.String("TERM_MATCH"),
			Field: aws.String("instanceType"),
			Value: aws.String(instanceType),
		},
		{
			Type:  aws.String("TERM_MATCH"),
			Field: aws.String("location"),
			Value: aws.String(location),
		},
		{
			Type:  aws.String("TERM_MATCH"),
			Field: aws.String("tenancy"),
			Value: aws.String("Shared"),
		},
		{
			Type:  aws.String("TERM_MATCH"),
			Field: aws.String("preInstalledSw"),
			Value: aws.String("NA"),
		},
		{
			Type:  aws.String("TERM_MATCH"),
			Field: aws.String("capacityStatus"),
			Value: aws.String("Used"),
		},
		{
			Type:  aws.String("TERM_MATCH"),
			Field: aws.String("servicecode"),
			Value: aws.String("AmazonEC2"),
		},
		{
			Type:  aws.String("TERM_MATCH"),
			Field: aws.String("productFamily"),
			Value: aws.String("Compute Instance"),
		},
	}, nil
}

// CalculateCost calculates the cost for an EC2 instance
func (ec *EC2Calculator) CalculateCost(hourlyPrice float64) *models.CostBreakdown {
	return ec.CalculateRates(hourlyPrice)
}
