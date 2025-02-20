package aws

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"cloudsift/internal/logging"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/pricing"
)

// CostBreakdown represents the cost of a resource over different time periods
type CostBreakdown struct {
	Hourly   float64 `json:"hourly"`
	Daily    float64 `json:"daily"`
	Monthly  float64 `json:"monthly"`
	Yearly   float64 `json:"yearly"`
	Lifetime float64 `json:"lifetime"`
}

// ResourceCostConfig holds configuration for resource cost calculation
type ResourceCostConfig struct {
	ResourceType string
	ResourceSize int64
	Region       string
	CreationTime time.Time
}

// AWS region to location name mapping for pricing API
var regionToLocation = map[string]string{
	// US Regions
	"us-east-1": "US East (N. Virginia)",
	"us-east-2": "US East (Ohio)",
	"us-west-1": "US West (N. California)",
	"us-west-2": "US West (Oregon)",

	// Canada
	"ca-central-1": "Canada (Central)",

	// South America
	"sa-east-1": "South America (São Paulo)",

	// Europe
	"eu-central-1": "Europe (Frankfurt)",
	"eu-central-2": "Europe (Zurich)",
	"eu-west-1":    "Europe (Ireland)",
	"eu-west-2":    "Europe (London)",
	"eu-west-3":    "Europe (Paris)",
	"eu-north-1":   "Europe (Stockholm)",
	"eu-south-1":   "Europe (Milan)",
	"eu-south-2":   "Europe (Spain)",

	// Africa
	"af-south-1": "Africa (Cape Town)",

	// Middle East
	"me-central-1": "Middle East (UAE)",
	"me-south-1":   "Middle East (Bahrain)",

	// Asia Pacific
	"ap-east-1":      "Asia Pacific (Hong Kong)",
	"ap-south-1":     "Asia Pacific (Mumbai)",
	"ap-south-2":     "Asia Pacific (Hyderabad)",
	"ap-southeast-1": "Asia Pacific (Singapore)",
	"ap-southeast-2": "Asia Pacific (Sydney)",
	"ap-southeast-3": "Asia Pacific (Jakarta)",
	"ap-southeast-4": "Asia Pacific (Melbourne)",
	"ap-northeast-1": "Asia Pacific (Tokyo)",
	"ap-northeast-2": "Asia Pacific (Seoul)",
	"ap-northeast-3": "Asia Pacific (Osaka)",

	// China (requires separate AWS accounts)
	"cn-north-1":     "China (Beijing)",
	"cn-northwest-1": "China (Ningxia)",
}

// CostEstimator handles AWS resource cost calculations with caching
type CostEstimator struct {
	pricingClient *pricing.Pricing
	cacheFile     string
	priceCache    map[string]float64
	cacheLock     sync.RWMutex
	saveLock      sync.Mutex
}

// NewCostEstimator creates a new CostEstimator instance
func NewCostEstimator(cacheFile string) (*CostEstimator, error) {
	if cacheFile == "" {
		cacheFile = "cost_estimator.json"
	}

	// Ensure cache directory exists
	cacheDir := filepath.Dir(cacheFile)
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		logging.Error("Failed to create cache directory", err, map[string]interface{}{
			"cache_dir": cacheDir,
		})
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	sess, err := GetSession("us-east-1") // Pricing API is only available in us-east-1
	if err != nil {
		logging.Error("Failed to create AWS session", err, map[string]interface{}{
			"region": "us-east-1",
		})
		return nil, fmt.Errorf("failed to create AWS session: %w", err)
	}

	ce := &CostEstimator{
		pricingClient: pricing.New(sess),
		cacheFile:     cacheFile,
		priceCache:    make(map[string]float64),
	}

	if err := ce.loadCache(); err != nil {
		logging.Error("Failed to load cache", err, map[string]interface{}{
			"cache_file": cacheFile,
		})
		return nil, fmt.Errorf("failed to load cache: %w", err)
	}

	logging.Info("Cost estimator initialized", map[string]interface{}{
		"cache_file": cacheFile,
	})

	return ce, nil
}

func (ce *CostEstimator) loadCache() error {
	ce.cacheLock.Lock()
	defer ce.cacheLock.Unlock()

	data, err := os.ReadFile(ce.cacheFile)
	if err != nil {
		if os.IsNotExist(err) {
			logging.Debug("Cache file does not exist, starting with empty cache", map[string]interface{}{
				"cache_file": ce.cacheFile,
			})
			return nil // Cache file doesn't exist yet
		}
		logging.Error("Failed to read cache file", err, map[string]interface{}{
			"cache_file": ce.cacheFile,
		})
		return fmt.Errorf("failed to read cache file: %w", err)
	}

	if err := json.Unmarshal(data, &ce.priceCache); err != nil {
		logging.Error("Failed to unmarshal cache data", err, map[string]interface{}{
			"cache_file": ce.cacheFile,
		})
		return fmt.Errorf("failed to unmarshal cache data: %w", err)
	}

	logging.Debug("Cache loaded successfully", map[string]interface{}{
		"cache_file": ce.cacheFile,
		"cache_size": len(ce.priceCache),
	})

	return nil
}

func (ce *CostEstimator) saveCache() error {
	ce.saveLock.Lock()
	defer ce.saveLock.Unlock()

	ce.cacheLock.RLock()
	data, err := json.MarshalIndent(ce.priceCache, "", "  ")
	ce.cacheLock.RUnlock()

	if err != nil {
		logging.Error("Failed to marshal cache", err, map[string]interface{}{
			"cache_file": ce.cacheFile,
		})
		return fmt.Errorf("failed to marshal cache: %w", err)
	}

	if err := os.WriteFile(ce.cacheFile, data, 0644); err != nil {
		logging.Error("Failed to write cache file", err, map[string]interface{}{
			"cache_file": ce.cacheFile,
		})
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	logging.Debug("Cache saved successfully", map[string]interface{}{
		"cache_file": ce.cacheFile,
		"cache_size": len(ce.priceCache),
	})

	return nil
}

func (ce *CostEstimator) getAWSPrice(resourceType, region string) (float64, error) {
	// Get location name for the region
	location, ok := regionToLocation[region]
	if !ok {
		return 0, fmt.Errorf("unsupported region: %s", region)
	}

	// Create cache key
	cacheKey := fmt.Sprintf("%s_%s", resourceType, region)

	// Check cache first
	ce.cacheLock.RLock()
	if price, ok := ce.priceCache[cacheKey]; ok {
		ce.cacheLock.RUnlock()
		logging.Debug("Price found in cache", map[string]interface{}{
			"resource_type": resourceType,
			"region":        region,
			"price":         price,
		})
		return price, nil
	}
	ce.cacheLock.RUnlock()

	logging.Debug("Price not found in cache, fetching from AWS", map[string]interface{}{
		"resource_type": resourceType,
		"region":        region,
	})

	var filters []*pricing.Filter
	switch resourceType {
	case "EBSVolumes":
		filters = []*pricing.Filter{
			{
				Type:  aws.String("TERM_MATCH"),
				Field: aws.String("productFamily"),
				Value: aws.String("Storage"),
			},
			{
				Type:  aws.String("TERM_MATCH"),
				Field: aws.String("volumeType"),
				Value: aws.String("General Purpose"),
			},
			{
				Type:  aws.String("TERM_MATCH"),
				Field: aws.String("location"),
				Value: aws.String(location),
			},
		}
	case "EBSSnapshots":
		filters = []*pricing.Filter{
			{
				Type:  aws.String("TERM_MATCH"),
				Field: aws.String("productFamily"),
				Value: aws.String("Storage Snapshot"),
			},
			{
				Type:  aws.String("TERM_MATCH"),
				Field: aws.String("location"),
				Value: aws.String(location),
			},
		}
	default:
		return 0, fmt.Errorf("unsupported resource type: %s", resourceType)
	}

	input := &pricing.GetProductsInput{
		ServiceCode: aws.String("AmazonEC2"),
		Filters:     filters,
	}

	result, err := ce.pricingClient.GetProducts(input)
	if err != nil {
		logging.Error("Failed to get pricing from AWS", err, map[string]interface{}{
			"resource_type": resourceType,
			"region":        region,
		})
		return 0, fmt.Errorf("failed to get pricing: %w", err)
	}

	if len(result.PriceList) == 0 {
		logging.Error("No pricing information found", fmt.Errorf("no pricing data"), map[string]interface{}{
			"resource_type": resourceType,
			"region":        region,
		})
		return 0, fmt.Errorf("no pricing information found")
	}

	// Parse the price from the response
	var priceFloat float64
	for _, priceData := range result.PriceList {
		// Convert to JSON for easier parsing
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

		// Get the first price dimension
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

				// Cache the price
				ce.cacheLock.Lock()
				ce.priceCache[cacheKey] = priceFloat
				ce.cacheLock.Unlock()

				if err := ce.saveCache(); err != nil {
					logging.Error("Failed to save cache", err, map[string]interface{}{
						"cache_file": ce.cacheFile,
					})
				}

				logging.Debug("Price fetched and cached successfully", map[string]interface{}{
					"resource_type": resourceType,
					"region":        region,
					"price":         priceFloat,
				})

				return priceFloat, nil
			}
		}
	}

	return 0, fmt.Errorf("could not find valid price in response")
}

// CalculateCost calculates the cost for a given resource
func (ce *CostEstimator) CalculateCost(config ResourceCostConfig) (*CostBreakdown, error) {
	logging.Debug("Calculating resource cost", map[string]interface{}{
		"resource_type": config.ResourceType,
		"resource_size": config.ResourceSize,
		"region":        config.Region,
		"creation_time": config.CreationTime.Format(time.RFC3339),
	})

	// Get price from AWS Pricing API
	pricePerGB, err := ce.getAWSPrice(config.ResourceType, config.Region)
	if err != nil {
		logging.Error("Failed to get AWS price", err, map[string]interface{}{
			"resource_type": config.ResourceType,
			"region":        config.Region,
		})
		return nil, fmt.Errorf("failed to get AWS price: %w", err)
	}

	// Calculate total monthly price based on size
	monthlyPrice := float64(config.ResourceSize) * pricePerGB

	// Calculate time periods
	now := time.Now()
	lifetime := now.Sub(config.CreationTime).Hours()

	// Convert monthly price to other time periods
	hourlyPrice := monthlyPrice / 730 // Average hours in a month
	breakdown := &CostBreakdown{
		Hourly:   hourlyPrice,
		Daily:    hourlyPrice * 24,
		Monthly:  monthlyPrice,
		Yearly:   monthlyPrice * 12,
		Lifetime: hourlyPrice * lifetime,
	}

	logging.Debug("Cost calculation completed", map[string]interface{}{
		"resource_type": config.ResourceType,
		"resource_size": config.ResourceSize,
		"price_per_gb":  pricePerGB,
		"monthly_price": monthlyPrice,
		"breakdown":     breakdown,
	})

	return breakdown, nil
}
