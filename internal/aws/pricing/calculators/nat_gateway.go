package calculators

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/pricing"

	"cloudsift/internal/aws/pricing/models"
)

// NATGatewayCalculator handles NAT Gateway cost calculations
type NATGatewayCalculator struct {
	BaseCalculator
}

// Default hourly price for NAT Gateway if pricing API fails
const defaultNATGatewayHourlyPrice = 0.045

// CalculateCost calculates the cost for a NAT Gateway
func (nc *NATGatewayCalculator) CalculateCost(price float64) *models.CostBreakdown {
	// If price is 0 (API failed to return a price), use the default price
	if price == 0 {
		price = defaultNATGatewayHourlyPrice
	}
	
	// NAT Gateway pricing is already per hour, so we can use the base calculator
	return nc.CalculateRates(price)
}

// GetPricingFilters returns the filters needed for the AWS Pricing API
func (nc *NATGatewayCalculator) GetPricingFilters(config models.ResourceCostConfig, location string) ([]*pricing.Filter, error) {
	// NAT Gateway pricing is based on the region
	filters := []*pricing.Filter{
		{
			Type:  aws.String("TERM_MATCH"),
			Field: aws.String("servicecode"),
			Value: aws.String("AmazonVPC"),
		},
		{
			Type:  aws.String("TERM_MATCH"),
			Field: aws.String("location"),
			Value: aws.String(location), // Replace 'location' with your desired region
		},
		{
			Type:  aws.String("TERM_MATCH"),
			Field: aws.String("operation"),
			Value: aws.String("NatGateway"),
		},
		{
			Type:  aws.String("TERM_MATCH"),
			Field: aws.String("productFamily"),
			Value: aws.String("NAT Gateway"),
		},
		{
			Type:  aws.String("TERM_MATCH"),
			Field: aws.String("usagetype"),
			Value: aws.String("NatGateway-Hours"), // For NAT Gateway hourly cost
		},
	}

	return filters, nil
}
