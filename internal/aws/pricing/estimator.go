package pricing

import (
	"context"
	"encoding/json"
	"fmt"

	awssdk "github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/pricing"

	internalaws "cloudsift/internal/aws"
	"cloudsift/internal/aws/pricing/cache"
	"cloudsift/internal/aws/pricing/calculators"
	pricingconfig "cloudsift/internal/aws/pricing/config"
	"cloudsift/internal/aws/pricing/models"
	"cloudsift/internal/config"
	"cloudsift/internal/logging"
)

// CostEstimator handles AWS resource cost calculations with caching
type CostEstimator struct {
	pricingClient *pricing.Pricing
	priceCache    *cache.PriceCache
	rateLimiter   *internalaws.RateLimiter
	calculators   map[string]interface{}
}

// DefaultCostEstimator is the default cost estimator instance
var DefaultCostEstimator *CostEstimator

// InitializeDefaultCostEstimator initializes the default cost estimator with the given session
func InitializeDefaultCostEstimator(sess *session.Session) error {
	var err error
	DefaultCostEstimator, err = NewCostEstimator(sess, "cache/costs.json")
	if err != nil {
		return fmt.Errorf("failed to create default cost estimator: %w", err)
	}
	return nil
}

// NewCostEstimator creates a new CostEstimator instance
func NewCostEstimator(sess *session.Session, cacheFile string) (*CostEstimator, error) {
	pc, err := cache.NewPriceCache(cacheFile)
	if err != nil {
		return nil, fmt.Errorf("failed to create price cache: %w", err)
	}

	// Create pricing client with explicit config to ensure region is us-east-1 (required for pricing API)
	cfg := awssdk.NewConfig().WithRegion("us-east-1")
	ce := &CostEstimator{
		pricingClient: pricing.New(sess, cfg),
		priceCache:    pc,
		rateLimiter:   internalaws.NewRateLimiter(&config.DefaultRateLimitConfig),
		calculators: map[string]interface{}{
			"EC2":        &calculators.EC2Calculator{},
			"EBSVolumes": &calculators.EBSCalculator{},
			"ElasticIP":  &calculators.EIPCalculator{},
			"ELB":        &calculators.ELBCalculator{},
			"DynamoDB":   &calculators.DynamoDBCalculator{},
			"OpenSearch": &calculators.OpenSearchCalculator{},
			"NATGateway": &calculators.NATGatewayCalculator{},
		},
	}

	return ce, nil
}

// getPriceFromAPI retrieves pricing information from AWS Pricing API
func (ce *CostEstimator) getPriceFromAPI(filters []*pricing.Filter) (float64, error) {
	// Wait for rate limiter
	ctx := context.Background()
	if err := ce.rateLimiter.Wait(ctx); err != nil {
		return 0, fmt.Errorf("rate limiter interrupted: %w", err)
	}

	// Find the service code from filters
	serviceCode := "AmazonEC2" // default to EC2
	for _, filter := range filters {
		if awssdk.StringValue(filter.Field) == "servicecode" {
			serviceCode = awssdk.StringValue(filter.Value)
			break
		}
	}

	input := &pricing.GetProductsInput{
		ServiceCode: awssdk.String(serviceCode),
		Filters:     filters,
	}

	result, err := ce.pricingClient.GetProducts(input)
	if err != nil {
		ce.rateLimiter.OnFailure()
		return 0, fmt.Errorf("failed to get pricing: %w", err)
	}

	ce.rateLimiter.OnSuccess()

	if len(result.PriceList) == 0 {
		return 0, fmt.Errorf("no pricing information found")
	}

	// Parse the price from the response
	var priceFloat float64
	for _, priceData := range result.PriceList {
		jsonBytes, err := json.Marshal(priceData)
		if err != nil {
			continue
		}

		var data map[string]interface{}
		if err := json.Unmarshal(jsonBytes, &data); err != nil {
			continue
		}

		terms, ok := data["terms"].(map[string]interface{})
		if !ok {
			continue
		}

		onDemand, ok := terms["OnDemand"].(map[string]interface{})
		if !ok {
			continue
		}

		for _, term := range onDemand {
			termData, ok := term.(map[string]interface{})
			if !ok {
				continue
			}

			priceDimensions, ok := termData["priceDimensions"].(map[string]interface{})
			if !ok {
				continue
			}

			for _, dimension := range priceDimensions {
				dimData, ok := dimension.(map[string]interface{})
				if !ok {
					continue
				}

				pricePerUnit, ok := dimData["pricePerUnit"].(map[string]interface{})
				if !ok {
					continue
				}

				priceStr, ok := pricePerUnit["USD"].(string)
				if !ok {
					continue
				}

				if _, err := fmt.Sscanf(priceStr, "%f", &priceFloat); err != nil {
					continue
				}

				return priceFloat, nil
			}
		}
	}

	return 0, fmt.Errorf("could not find valid price in response")
}

// CalculateCost calculates the cost for a given resource
func (ce *CostEstimator) CalculateCost(config models.ResourceCostConfig) (*models.CostBreakdown, error) {
	logging.Debug("Calculating cost", map[string]interface{}{
		"resource_type": config.ResourceType,
		"region":        config.Region,
	})

	// Special case for Elastic IP which has a fixed price
	if config.ResourceType == "ElasticIP" {
		calc := ce.calculators["ElasticIP"].(*calculators.EIPCalculator)
		return calc.CalculateCost(), nil
	}

	// Get location for pricing API
	location, ok := pricingconfig.GetLocationForRegion(config.Region)
	if !ok {
		return nil, fmt.Errorf("unknown region: %s", config.Region)
	}

	// Get the appropriate calculator
	calc, ok := ce.calculators[config.ResourceType]
	if !ok {
		return nil, fmt.Errorf("unsupported resource type: %s", config.ResourceType)
	}

	// Generate cache key
	var resourceSizeStr string
	if config.ResourceType == "EBSVolumes" {
		resourceSizeStr = config.VolumeType
	} else {
		resourceSizeStr = fmt.Sprintf("%v", config.ResourceSize)
	}
	cacheKey := fmt.Sprintf("%s:%s:%s", config.ResourceType, config.Region, resourceSizeStr)

	// Check cache
	if price, ok := ce.priceCache.Get(cacheKey); ok {
		return ce.calculateWithPrice(calc, price, config)
	}

	// Get filters for pricing API
	filters, err := ce.getFiltersForResource(calc, config, location)
	if err != nil {
		return nil, err
	}

	// Get price from API
	price, err := ce.getPriceFromAPI(filters)
	if err != nil {
		return nil, err
	}

	// Update cache
	ce.priceCache.Set(cacheKey, price)
	if err := ce.priceCache.Save(); err != nil {
		logging.Error("Failed to save price cache", err, nil)
	}

	return ce.calculateWithPrice(calc, price, config)
}

// calculateWithPrice uses the appropriate calculator to calculate costs
func (ce *CostEstimator) calculateWithPrice(calc interface{}, price float64, config models.ResourceCostConfig) (*models.CostBreakdown, error) {
	switch c := calc.(type) {
	case *calculators.EC2Calculator:
		return c.CalculateCost(price), nil
	case *calculators.EBSCalculator:
		return c.CalculateCost(price, config)
	case *calculators.ELBCalculator:
		return c.CalculateCost(price), nil
	case *calculators.DynamoDBCalculator:
		return c.CalculateCost(price), nil
	case *calculators.OpenSearchCalculator:
		return c.CalculateCost(price, config), nil
	case *calculators.NATGatewayCalculator:
		return c.CalculateCost(price), nil
	default:
		return nil, fmt.Errorf("unsupported calculator type for resource: %s", config.ResourceType)
	}
}

// getFiltersForResource gets the appropriate filters for the resource type
func (ce *CostEstimator) getFiltersForResource(calc interface{}, config models.ResourceCostConfig, location string) ([]*pricing.Filter, error) {
	switch c := calc.(type) {
	case *calculators.EC2Calculator:
		return c.GetPricingFilters(config, location)
	case *calculators.EBSCalculator:
		return c.GetPricingFilters(config, location)
	case *calculators.ELBCalculator:
		return c.GetPricingFilters(config, location)
	case *calculators.DynamoDBCalculator:
		return c.GetPricingFilters(config, location)
	case *calculators.OpenSearchCalculator:
		return c.GetPricingFilters(config, location)
	case *calculators.NATGatewayCalculator:
		return c.GetPricingFilters(config, location)
	default:
		return nil, fmt.Errorf("unsupported calculator type for resource: %s", config.ResourceType)
	}
}
