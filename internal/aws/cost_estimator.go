package aws

import (
	"encoding/json"
	"fmt"
	"math"
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
	HourlyRate   float64  `json:"hourly_rate"`
	DailyRate    float64  `json:"daily_rate"`
	MonthlyRate  float64  `json:"monthly_rate"`
	YearlyRate   float64  `json:"yearly_rate"`
	HoursRunning *float64 `json:"hours_running,omitempty"`
	Lifetime     *float64 `json:"lifetime,omitempty"`
}

// ResourceCostConfig holds configuration for resource cost calculation
type ResourceCostConfig struct {
	ResourceType string
	ResourceSize interface{} // Can be int64 for storage sizes or string for instance types
	Region       string
	CreationTime time.Time
	VolumeType   string // Volume type for EBS (e.g., "gp2", "gp3", "io1")
	LBType       string // Load balancer type (e.g., "application", "network")
	ProcessedGB  float64 // Processed GB for load balancers
	InstanceCount int64  // Instance count for OpenSearch
	StorageSize  int64  // Storage size for OpenSearch
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

// DefaultCostEstimator is the default cost estimator instance
var DefaultCostEstimator *CostEstimator

func init() {
	var err error
	DefaultCostEstimator, err = NewCostEstimator("cache/costs.json")
	if err != nil {
		panic(fmt.Sprintf("Failed to create default cost estimator: %v", err))
	}
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

	logging.Debug("Cost estimator initialized", map[string]interface{}{
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

	// Lock the cache while marshaling to prevent concurrent modifications
	ce.cacheLock.RLock()
	data, err := json.MarshalIndent(ce.priceCache, "", "  ")
	ce.cacheLock.RUnlock()
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
	cacheKey := fmt.Sprintf("%s:%s:%v:%s:%s:%v", resourceType, region, config.ResourceSize, config.VolumeType, config.LBType, config.ProcessedGB)
	ce.cacheLock.RLock()
	if price, ok := ce.priceCache[cacheKey]; ok {
		ce.cacheLock.RUnlock()
		return price, nil
	}
	ce.cacheLock.RUnlock()

	// Get location name for pricing API
	location, ok := regionToLocation[region]
	if !ok {
		return 0, fmt.Errorf("unknown region: %s", region)
	}

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
			return 0, fmt.Errorf("invalid resource size type for %s: %T", resourceType, config.ResourceSize)
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
			return 0, fmt.Errorf("invalid resource size type for %s: %T", resourceType, config.ResourceSize)
		}
		if config.VolumeType == "" {
			return 0, fmt.Errorf("volume type is required for %s", resourceType)
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
				Field: aws.String("location"),
				Value: aws.String(location),
			},
			{
				Type:  aws.String("TERM_MATCH"),
				Field: aws.String("storageMedia"),
				Value: aws.String("Amazon S3"),
			},
		}

		logging.Debug("Fetching snapshot pricing", map[string]interface{}{
			"region":      region,
			"volume_type": config.VolumeType,
			"size":        config.ResourceSize,
			"filters":     filters,
		})
	case "elb":
		filters = []*pricing.Filter{
			{
				Field: aws.String("servicecode"),
				Type:  aws.String("TERM_MATCH"),
				Value: aws.String("AWSElasticLoadBalancer"),
			},
			{
				Field: aws.String("location"),
				Type:  aws.String("TERM_MATCH"),
				Value: aws.String(location),
			},
		}

		if config.LBType == "application" {
			filters = append(filters, &pricing.Filter{
				Field: aws.String("usagetype"),
				Type:  aws.String("TERM_MATCH"),
				Value: aws.String("LoadBalancerUsage"),
			})
		} else if config.LBType == "network" {
			filters = append(filters, &pricing.Filter{
				Field: aws.String("usagetype"),
				Type:  aws.String("TERM_MATCH"),
				Value: aws.String("NetworkLoadBalancer-Hours"),
			})
		}

		// Add data processing cost if ProcessedGB > 0
		if config.ProcessedGB > 0 {
			dataFilters := []*pricing.Filter{
				{
					Field: aws.String("servicecode"),
					Type:  aws.String("TERM_MATCH"),
					Value: aws.String("AWSElasticLoadBalancer"),
				},
				{
					Field: aws.String("location"),
					Type:  aws.String("TERM_MATCH"),
					Value: aws.String(location),
				},
				{
					Field: aws.String("usagetype"),
					Type:  aws.String("TERM_MATCH"),
					Value: aws.String("DataProcessing-Bytes"),
				},
			}
			
			dataPrice, err := ce.getPriceFromAPI(dataFilters)
			if err != nil {
				return 0, fmt.Errorf("failed to get data processing price: %w", err)
			}
			
			// Convert bytes to GB and calculate data processing cost
			gbPrice := dataPrice * config.ProcessedGB
			
			// Get hourly LB price
			lbPrice, err := ce.getPriceFromAPI(filters)
			if err != nil {
				return 0, fmt.Errorf("failed to get load balancer price: %w", err)
			}
			
			// Cache and return combined price
			totalPrice := lbPrice + gbPrice
			ce.cacheLock.Lock()
			ce.priceCache[cacheKey] = totalPrice
			ce.cacheLock.Unlock()
			
			return totalPrice, nil
		}
	case "DynamoDB":
		// For DynamoDB we need to check both storage and throughput costs
		// First get storage cost per GB
		storageFilters := []*pricing.Filter{
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
				Field: aws.String("productFamily"),
				Value: aws.String("Database Storage"),
			},
		}

		// Get storage price per GB
		storagePrice, err := ce.getPriceFromAPI(storageFilters)
		if err != nil {
			return 0, fmt.Errorf("failed to get DynamoDB storage price: %w", err)
		}

		// Get write capacity price
		writeFilters := []*pricing.Filter{
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
				Field: aws.String("productFamily"),
				Value: aws.String("Provisioned IOPS"),
			},
			{
				Type:  aws.String("TERM_MATCH"),
				Field: aws.String("group"),
				Value: aws.String("DDB-WriteUnits"),
			},
		}

		writePrice, err := ce.getPriceFromAPI(writeFilters)
		if err != nil {
			return 0, fmt.Errorf("failed to get DynamoDB write capacity price: %w", err)
		}

		// Get read capacity price
		readFilters := []*pricing.Filter{
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
				Field: aws.String("productFamily"),
				Value: aws.String("Provisioned IOPS"),
			},
			{
				Type:  aws.String("TERM_MATCH"),
				Field: aws.String("group"),
				Value: aws.String("DDB-ReadUnits"),
			},
		}

		readPrice, err := ce.getPriceFromAPI(readFilters)
		if err != nil {
			return 0, fmt.Errorf("failed to get DynamoDB read capacity price: %w", err)
		}

		// Calculate total cost based on table size and provisioned capacity
		tableSizeGB := float64(config.ResourceSize.(int64)) / (1024 * 1024 * 1024) // Convert bytes to GB
		totalCost := (storagePrice * tableSizeGB) + (writePrice * 25) + (readPrice * 25) // Assume minimum 25 WCU and RCU
		
		ce.cacheLock.Lock()
		ce.priceCache[cacheKey] = totalCost
		ce.cacheLock.Unlock()
		
		return totalCost, nil
	case "OpenSearch":
		// For OpenSearch we need to calculate both instance and storage costs
		// First get instance price
		instanceType, ok := config.ResourceSize.(string)
		if !ok {
			return 0, fmt.Errorf("invalid resource size type for OpenSearch: %T", config.ResourceSize)
		}

		instanceFilters := []*pricing.Filter{
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
			{
				Type:  aws.String("TERM_MATCH"),
				Field: aws.String("usagetype"),
				Value: aws.String(fmt.Sprintf("%s-BoxUsage:%s", strings.ToUpper(strings.Split(region, "-")[0]), instanceType)),
			},
		}

		// Get instance price per hour
		instancePrice, err := ce.getPriceFromAPI(instanceFilters)
		if err != nil {
			return 0, fmt.Errorf("failed to get OpenSearch instance price: %w", err)
		}

		// Get storage price
		storageFilters := []*pricing.Filter{
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
				Field: aws.String("volumeType"),
				Value: aws.String(config.VolumeType),
			},
			{
				Type:  aws.String("TERM_MATCH"),
				Field: aws.String("productFamily"),
				Value: aws.String("Storage"),
			},
		}

		// Get storage price per GB per month
		storagePrice, err := ce.getPriceFromAPI(storageFilters)
		if err != nil {
			return 0, fmt.Errorf("failed to get OpenSearch storage price: %w", err)
		}

		// Calculate total cost
		// Storage price is per GB per month, convert to per hour
		storagePricePerHour := (storagePrice * float64(config.StorageSize)) / (24 * 30) // Approximate month to 30 days
		totalCost := (instancePrice * float64(config.InstanceCount)) + storagePricePerHour

		ce.cacheLock.Lock()
		ce.priceCache[cacheKey] = totalCost
		ce.cacheLock.Unlock()

		return totalCost, nil
	case "ElasticIP":
		// Elastic IPs have a flat rate of $0.005 per hour when not attached
		hourlyRate := roundCost(0.005) // $0.005 per hour
		return hourlyRate, nil
	case "RDS":
		// RDS has a flat rate of $0.005 per hour when not attached
		hourlyRate := 0.005 // $0.005 per hour
		return hourlyRate, nil
	default:
		cacheKey = fmt.Sprintf("%s:%s", resourceType, region)
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
		"resource_type":    resourceType,
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
						"cache_key":  cacheKey,
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

func (ce *CostEstimator) getPriceFromAPI(filters []*pricing.Filter) (float64, error) {
	input := &pricing.GetProductsInput{
		ServiceCode: aws.String("AWSElasticLoadBalancer"),
		Filters:     filters,
	}

	result, err := ce.pricingClient.GetProducts(input)
	if err != nil {
		return 0, fmt.Errorf("failed to get pricing: %w", err)
	}

	if len(result.PriceList) == 0 {
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

				return priceFloat, nil
			}
		}
	}

	return 0, fmt.Errorf("could not find valid price in response")
}

// roundCost rounds a cost value to 4 decimal places
func roundCost(cost float64) float64 {
	return math.Round(cost*10000) / 10000
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

	// Calculate base price based on resource type
	var hourlyPrice float64
	switch config.ResourceType {
	case "EC2":
		// For EC2, price is already per hour
		hourlyPrice = pricePerUnit
	case "EBSVolumes", "EBSSnapshots":
		// For storage resources, we only care about size and rates
		size, ok := config.ResourceSize.(int64)
		if !ok {
			return nil, fmt.Errorf("invalid resource size type for cost calculation: %T", config.ResourceSize)
		}
		monthlyPrice := float64(size) * pricePerUnit // Price per GB-month
		hourlyPrice = monthlyPrice / 730             // Convert to hourly (730 hours in a month)
		dailyPrice := hourlyPrice * 24
		monthlyPrice = dailyPrice * 30 // Approximate
		yearlyPrice := dailyPrice * 365

		return &CostBreakdown{
			HourlyRate:   roundCost(hourlyPrice),
			DailyRate:    roundCost(dailyPrice),
			MonthlyRate:  roundCost(monthlyPrice),
			YearlyRate:   roundCost(yearlyPrice),
			HoursRunning: nil, // Hours running should be stored in details
			Lifetime:     nil, // Lifetime will be calculated by the application
		}, nil
	case "ElasticIP":
		// Elastic IPs have a flat rate of $0.005 per hour when not attached
		hourlyPrice = 0.005
		dailyPrice := hourlyPrice * 24
		monthlyPrice := dailyPrice * 30 // Approximate
		yearlyPrice := dailyPrice * 365

		// For Elastic IPs, we return immediately since we can't calculate lifetime
		// (we don't know when it became unattached)
		return &CostBreakdown{
			HourlyRate:   roundCost(hourlyPrice),
			DailyRate:    roundCost(dailyPrice),
			MonthlyRate:  roundCost(monthlyPrice),
			YearlyRate:   roundCost(yearlyPrice),
			HoursRunning: nil,
			Lifetime:     nil,
		}, nil
	case "elb":
		// For ELB, price is already per hour
		hourlyPrice = pricePerUnit
		dailyPrice := hourlyPrice * 24
		monthlyPrice := dailyPrice * 30 // Approximate
		yearlyPrice := dailyPrice * 365

		return &CostBreakdown{
			HourlyRate:   roundCost(hourlyPrice),
			DailyRate:    roundCost(dailyPrice),
			MonthlyRate:  roundCost(monthlyPrice),
			YearlyRate:   roundCost(yearlyPrice),
			HoursRunning: nil, // Hours running should be stored in details
			Lifetime:     nil, // Lifetime will be calculated by the application
		}, nil
	case "DynamoDB":
		// For DynamoDB, price is already per hour
		hourlyPrice = pricePerUnit
		dailyPrice := hourlyPrice * 24
		monthlyPrice := dailyPrice * 30 // Approximate
		yearlyPrice := dailyPrice * 365

		return &CostBreakdown{
			HourlyRate:   roundCost(hourlyPrice),
			DailyRate:    roundCost(dailyPrice),
			MonthlyRate:  roundCost(monthlyPrice),
			YearlyRate:   roundCost(yearlyPrice),
			HoursRunning: nil, // Hours running should be stored in details
			Lifetime:     nil, // Lifetime will be calculated by the application
		}, nil
	case "OpenSearch":
		// For OpenSearch, price is already per hour
		hourlyPrice = pricePerUnit
		dailyPrice := hourlyPrice * 24
		monthlyPrice := dailyPrice * 30 // Approximate
		yearlyPrice := dailyPrice * 365

		return &CostBreakdown{
			HourlyRate:   roundCost(hourlyPrice),
			DailyRate:    roundCost(dailyPrice),
			MonthlyRate:  roundCost(monthlyPrice),
			YearlyRate:   roundCost(yearlyPrice),
			HoursRunning: nil, // Hours running should be stored in details
			Lifetime:     nil, // Lifetime will be calculated by the application
		}, nil
	default:
		return nil, fmt.Errorf("unsupported resource type: %s", config.ResourceType)
	}

	// Calculate prices for different time periods
	dailyPrice := hourlyPrice * 24
	monthlyPrice := dailyPrice * 30 // Approximate
	yearlyPrice := dailyPrice * 365
	lifetimeHours := time.Since(config.CreationTime).Hours()
	lifetime := hourlyPrice * lifetimeHours

	// For all other resources, we calculate lifetime cost based on hours running
	hours := roundCost(lifetimeHours)

	return &CostBreakdown{
		HourlyRate:   roundCost(hourlyPrice),
		DailyRate:    roundCost(dailyPrice),
		MonthlyRate:  roundCost(monthlyPrice),
		YearlyRate:   roundCost(yearlyPrice),
		HoursRunning: &hours,
		Lifetime:     &lifetime,
	}, nil
}
