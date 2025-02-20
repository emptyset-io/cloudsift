package aws

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

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
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	sess, err := GetSession("us-east-1") // Pricing API is only available in us-east-1
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS session: %w", err)
	}

	ce := &CostEstimator{
		pricingClient: pricing.New(sess),
		cacheFile:     cacheFile,
		priceCache:    make(map[string]float64),
	}

	if err := ce.loadCache(); err != nil {
		return nil, fmt.Errorf("failed to load cache: %w", err)
	}

	return ce, nil
}

func (ce *CostEstimator) loadCache() error {
	ce.cacheLock.Lock()
	defer ce.cacheLock.Unlock()

	data, err := os.ReadFile(ce.cacheFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Cache file doesn't exist yet
		}
		return fmt.Errorf("failed to read cache file: %w", err)
	}

	return json.Unmarshal(data, &ce.priceCache)
}

func (ce *CostEstimator) saveCache() error {
	ce.saveLock.Lock()
	defer ce.saveLock.Unlock()

	ce.cacheLock.RLock()
	data, err := json.MarshalIndent(ce.priceCache, "", "  ")
	ce.cacheLock.RUnlock()

	if err != nil {
		return fmt.Errorf("failed to marshal cache: %w", err)
	}

	return os.WriteFile(ce.cacheFile, data, 0644)
}

func (ce *CostEstimator) getAWSPrice(serviceCode string, filters map[string]string) (float64, error) {
	// Create cache key from service code and filters
	filterBytes, _ := json.Marshal(filters)
	cacheKey := fmt.Sprintf("%s_%s", serviceCode, string(filterBytes))

	// Check cache first
	ce.cacheLock.RLock()
	if price, ok := ce.priceCache[cacheKey]; ok {
		ce.cacheLock.RUnlock()
		return price, nil
	}
	ce.cacheLock.RUnlock()

	// Convert filters to AWS format
	awsFilters := make([]*pricing.Filter, 0, len(filters))
	for k, v := range filters {
		awsFilters = append(awsFilters, &pricing.Filter{
			Type:  aws.String("TERM_MATCH"),
			Field: aws.String(k),
			Value: aws.String(v),
		})
	}

	input := &pricing.GetProductsInput{
		ServiceCode: aws.String(serviceCode),
		Filters:     awsFilters,
	}

	result, err := ce.pricingClient.GetProducts(input)
	if err != nil {
		return 0, fmt.Errorf("failed to get pricing: %w", err)
	}

	if len(result.PriceList) == 0 {
		return 0, fmt.Errorf("no pricing information found")
	}

	// Marshal and unmarshal to handle AWS JSONValue type
	jsonBytes, err := json.Marshal(result.PriceList[0])
	if err != nil {
		return 0, fmt.Errorf("failed to marshal pricing data: %w", err)
	}

	var jsonData map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &jsonData); err != nil {
		return 0, fmt.Errorf("failed to unmarshal pricing data: %w", err)
	}

	terms, ok := jsonData["terms"].(map[string]interface{})
	if !ok {
		return 0, fmt.Errorf("invalid terms format in pricing data")
	}

	onDemand, ok := terms["OnDemand"].(map[string]interface{})
	if !ok {
		return 0, fmt.Errorf("invalid OnDemand format in pricing data")
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

			var priceFloat float64
			if _, err := fmt.Sscanf(priceStr, "%f", &priceFloat); err != nil {
				continue
			}

			// Cache the price
			ce.cacheLock.Lock()
			ce.priceCache[cacheKey] = priceFloat
			ce.cacheLock.Unlock()

			if err := ce.saveCache(); err != nil {
				// Log error but continue
				fmt.Printf("Failed to save cache: %v\n", err)
			}

			return priceFloat, nil
		}
	}

	return 0, fmt.Errorf("could not find price in response")
}

// CalculateCost calculates the cost for a given resource
func (ce *CostEstimator) CalculateCost(config ResourceCostConfig) (*CostBreakdown, error) {
	// Service code and filter mappings
	serviceCodeMap := map[string]string{
		"EBSVolumes":    "AmazonEC2",
		"EC2Instances":  "AmazonEC2",
		"EBSSnapshots":  "AmazonEC2",
		"RDSInstances":  "AmazonRDS",
		"DynamoDB":      "AmazonDynamoDB",
		"ElasticIPs":    "AmazonEC2",
		"LoadBalancers": "ElasticLoadBalancing",
		"EKSCluster":    "AmazonEKS",
	}

	// Get service code
	serviceCode, ok := serviceCodeMap[config.ResourceType]
	if !ok {
		return nil, fmt.Errorf("unsupported resource type: %s", config.ResourceType)
	}

	// Build filters based on resource type
	filters := map[string]string{
		"location": config.Region,
	}

	switch config.ResourceType {
	case "EBSVolumes":
		filters["productFamily"] = "Storage"
		filters["volumeType"] = "General Purpose"
	case "EBSSnapshots":
		filters["productFamily"] = "Storage Snapshot"
	case "EC2Instances":
		filters["productFamily"] = "Compute Instance"
		filters["instanceType"] = fmt.Sprintf("%d", config.ResourceSize)
		// Add more cases as needed
	}

	// Get price per unit
	pricePerUnit, err := ce.getAWSPrice(serviceCode, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to get price: %w", err)
	}

	// Calculate running hours
	hoursRunning := time.Since(config.CreationTime).Hours()

	// Calculate costs
	var hourly, daily, monthly, yearly, lifetime float64

	if config.ResourceType == "EBSVolumes" || config.ResourceType == "EBSSnapshots" {
		// EBS volumes and snapshots are charged per GB-month
		monthly = pricePerUnit * float64(config.ResourceSize)
		hourly = monthly / 720 // Approximate hours in a month
		daily = hourly * 24
		yearly = monthly * 12
		lifetime = monthly * (hoursRunning / 720)
	} else {
		// Other resources typically charged per hour
		hourly = pricePerUnit
		daily = hourly * 24
		monthly = hourly * 720
		yearly = monthly * 12
		lifetime = hourly * hoursRunning
	}

	return &CostBreakdown{
		Hourly:   hourly,
		Daily:    daily,
		Monthly:  monthly,
		Yearly:   yearly,
		Lifetime: lifetime,
	}, nil
}
