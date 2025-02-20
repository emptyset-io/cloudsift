package aws

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"cloudsift/internal/logging"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
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
	ResourceType  string
	ResourceSize  interface{} // Can be int64 for storage sizes or string for instance types
	Region       string
	CreationTime time.Time
	VolumeType   string // Volume type for EBS (e.g., "gp2", "gp3", "io1")
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

	// Create session in us-east-1 (required for pricing API)
	sess := session.Must(session.NewSession(&aws.Config{Region: aws.String("us-east-1")}))

	// Create pricing client with explicit config to ensure region is set
	cfg := aws.NewConfig().WithRegion("us-east-1")
	ce := &CostEstimator{
		pricingClient: pricing.New(sess, cfg),
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
	// Create cache directory if it doesn't exist
	cacheDir := filepath.Dir(ce.cacheFile)
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Read cache file
	data, err := os.ReadFile(ce.cacheFile)
	if err != nil {
		if os.IsNotExist(err) {
			// Initialize empty cache if file doesn't exist
			ce.priceCache = make(map[string]float64)
			return nil
		}
		return fmt.Errorf("failed to read cache file: %w", err)
	}

	// Parse cache data
	var cache map[string]float64
	if err := json.Unmarshal(data, &cache); err != nil {
		return fmt.Errorf("failed to parse cache data: %w", err)
	}

	// Convert old cache format to new format if needed
	newCache := make(map[string]float64)
	for key, price := range cache {
		parts := strings.Split(key, "_")
		if len(parts) == 2 && (parts[0] == "EBSVolumes" || parts[0] == "EBSSnapshots") {
			// Old format: EBSVolumes_us-west-2
			// Convert to new format: EBSVolumes_us-west-2_gp2
			newKey := fmt.Sprintf("%s_%s_%s", parts[0], parts[1], "gp2")
			newCache[newKey] = price
			logging.Debug("Converting cache key format", map[string]interface{}{
				"old_key": key,
				"new_key": newKey,
				"price":   price,
			})
		} else {
			newCache[key] = price
		}
	}

	ce.priceCache = newCache
	return nil
}

func (ce *CostEstimator) saveCache() error {
	ce.saveLock.Lock()
	defer ce.saveLock.Unlock()

	// Create cache directory if it doesn't exist
	cacheDir := filepath.Dir(ce.cacheFile)
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Marshal cache data
	data, err := json.MarshalIndent(ce.priceCache, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cache data: %w", err)
	}

	// Write to temp file first
	tempFile := ce.cacheFile + ".tmp"
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp cache file: %w", err)
	}

	// Rename temp file to actual cache file (atomic operation)
	if err := os.Rename(tempFile, ce.cacheFile); err != nil {
		return fmt.Errorf("failed to rename temp cache file: %w", err)
	}

	logging.Debug("Cache saved successfully", map[string]interface{}{
		"cache_file": ce.cacheFile,
		"entries":    len(ce.priceCache),
	})

	return nil
}

func (ce *CostEstimator) getAWSPrice(resourceType, region string, config ResourceCostConfig) (float64, error) {
	// Get location name for the region
	location, ok := regionToLocation[region]
	if !ok {
		return 0, fmt.Errorf("unsupported region: %s", region)
	}

	// Create cache key
	var cacheKey string
	switch resourceType {
	case "EC2":
		instanceType, ok := config.ResourceSize.(string)
		if !ok {
			return 0, fmt.Errorf("invalid resource size type for EC2: %T", config.ResourceSize)
		}
		cacheKey = fmt.Sprintf("%s_%s_%s", resourceType, region, instanceType)
	case "EBSVolumes", "EBSSnapshots":
		_, ok := config.ResourceSize.(int64)
		if !ok {
			return 0, fmt.Errorf("invalid resource size type for %s: %T", resourceType, config.ResourceSize)
		}
		if config.VolumeType == "" {
			return 0, fmt.Errorf("volume type is required for %s", resourceType)
		}
		cacheKey = fmt.Sprintf("%s_%s_%s", resourceType, region, config.VolumeType)
		logging.Debug("Generated cache key for storage", map[string]interface{}{
			"resource_type": resourceType,
			"region":        region,
			"volume_type":   config.VolumeType,
			"cache_key":     cacheKey,
		})
	default:
		cacheKey = fmt.Sprintf("%s_%s", resourceType, region)
	}

	// Check cache first
	ce.cacheLock.RLock()
	if price, ok := ce.priceCache[cacheKey]; ok {
		ce.cacheLock.RUnlock()
		logging.Debug("Price found in cache", map[string]interface{}{
			"resource_type": resourceType,
			"region":        region,
			"cache_key":     cacheKey,
			"price":         price,
		})
		return price, nil
	}
	ce.cacheLock.RUnlock()

	logging.Debug("Price not found in cache, fetching from AWS", map[string]interface{}{
		"resource_type": resourceType,
		"region":        region,
		"cache_key":     cacheKey,
	})

	var filters []*pricing.Filter
	switch resourceType {
	case "EC2":
		// Extract instance type from resource size string
		instanceType, ok := config.ResourceSize.(string)
		if !ok {
			return 0, fmt.Errorf("invalid resource size type for EC2: %T", config.ResourceSize)
		}
		filters = []*pricing.Filter{
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
		}
	case "EBSVolumes":
		_, ok := config.ResourceSize.(int64)
		if !ok {
			return 0, fmt.Errorf("invalid resource size type for EBSVolumes: %T", config.ResourceSize)
		}
		filters = []*pricing.Filter{
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
		}
	case "EBSSnapshots":
		_, ok := config.ResourceSize.(int64)
		if !ok {
			return 0, fmt.Errorf("invalid resource size type for EBSSnapshots: %T", config.ResourceSize)
		}
		filters = []*pricing.Filter{
			{
				Type:  aws.String("TERM_MATCH"),
				Field: aws.String("servicecode"),
				Value: aws.String("AmazonEC2"),
			},
			{
				Type:  aws.String("TERM_MATCH"),
				Field: aws.String("productFamily"),
				Value: aws.String("Storage Snapshot"),
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
			{
				Type:  aws.String("TERM_MATCH"),
				Field: aws.String("usagetype"),
				Value: aws.String("EBS:SnapshotUsage"),
			},
		}
	default:
		return 0, fmt.Errorf("unsupported resource type: %s", resourceType)
	}

	input := &pricing.GetProductsInput{
		ServiceCode: aws.String("AmazonEC2"),
		Filters:     filters,
	}

	logging.Debug("Fetching pricing from AWS", map[string]interface{}{
		"resource_type": resourceType,
		"region":        region,
		"filters":       filters,
	})

	result, err := ce.pricingClient.GetProducts(input)
	if err != nil {
		logging.Error("Failed to get pricing from AWS", err, map[string]interface{}{
			"resource_type": resourceType,
			"region":        region,
			"filters":       filters,
		})
		return 0, fmt.Errorf("failed to get pricing: %w", err)
	}

	if len(result.PriceList) == 0 {
		logging.Error("No pricing information found", fmt.Errorf("no pricing data"), map[string]interface{}{
			"resource_type": resourceType,
			"region":        region,
			"filters":       filters,
		})
		return 0, fmt.Errorf("no pricing information found")
	}

	logging.Debug("Got pricing results from AWS", map[string]interface{}{
		"resource_type":     resourceType,
		"region":           region,
		"price_list_count": len(result.PriceList),
	})

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
						"cache_key": cacheKey,
					})
				}

				logging.Debug("Price fetched and cached successfully", map[string]interface{}{
					"resource_type": resourceType,
					"region":        region,
					"cache_key":     cacheKey,
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
	logging.Debug("Calculating cost", map[string]interface{}{
		"resource_type": config.ResourceType,
		"region":        config.Region,
	})

	// Get price from AWS Pricing API
	pricePerUnit, err := ce.getAWSPrice(config.ResourceType, config.Region, config)
	if err != nil {
		logging.Error("Failed to get AWS price", err, map[string]interface{}{
			"resource_type": config.ResourceType,
			"region":        config.Region,
		})
		return nil, fmt.Errorf("failed to get AWS price: %w", err)
	}

	// Calculate total monthly price based on resource type
	var monthlyPrice float64
	switch config.ResourceType {
	case "EC2":
		// For EC2, price is already per hour, so just multiply by hours in a month
		monthlyPrice = pricePerUnit * 730 // Average hours in a month (365.25 * 24 / 12)
	case "EBSVolumes", "EBSSnapshots":
		// For storage resources, multiply by size (GB)
		// pricePerUnit is already in GB-month, so just multiply by size
		size, ok := config.ResourceSize.(int64)
		if !ok {
			return nil, fmt.Errorf("invalid resource size type for cost calculation: %T", config.ResourceSize)
		}
		monthlyPrice = float64(size) * pricePerUnit // Price per GB-month
	default:
		return nil, fmt.Errorf("unsupported resource type: %s", config.ResourceType)
	}

	// Calculate time periods
	now := time.Now()
	hourlyPrice := monthlyPrice / 730 // Average hours in a month
	dailyPrice := hourlyPrice * 24
	yearlyPrice := monthlyPrice * 12

	// Calculate lifetime cost based on how long it's been unused
	lifetimeHours := now.Sub(config.CreationTime).Hours()
	lifetimePrice := hourlyPrice * lifetimeHours

	return &CostBreakdown{
		Hourly:   hourlyPrice,
		Daily:    dailyPrice,
		Monthly:  monthlyPrice,
		Yearly:   yearlyPrice,
		Lifetime: lifetimePrice,
	}, nil
}
