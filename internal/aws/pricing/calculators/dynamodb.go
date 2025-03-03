package calculators

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/pricing"

	"cloudsift/internal/aws/pricing/models"
)

// DynamoDBCalculator handles DynamoDB cost calculations
type DynamoDBCalculator struct {
	BaseCalculator
}

// GetPricingFilters returns the pricing filters for DynamoDB
func (dc *DynamoDBCalculator) GetPricingFilters(config models.ResourceCostConfig, location string) ([]*pricing.Filter, error) {
	return []*pricing.Filter{
		{
			Type:  aws.String("TERM_MATCH"),
			Field: aws.String("servicecode"),
			Value: aws.String("AmazonDynamoDB"),
		},
		{
			Type:  aws.String("TERM_MATCH"),
			Field: aws.String("location"),
			Value: aws.String(location),
		},
		{
			Type:  aws.String("TERM_MATCH"),
			Field: aws.String("group"),
			Value: aws.String("DDB-ReadWriteCapacityUnit"),
		},
	}, nil
}

// CalculateCost calculates the cost for DynamoDB
func (dc *DynamoDBCalculator) CalculateCost(hourlyPrice float64) *models.CostBreakdown {
	return dc.CalculateRates(hourlyPrice)
}
