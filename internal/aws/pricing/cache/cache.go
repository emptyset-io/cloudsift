package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"cloudsift/internal/logging"
)

// PriceCache handles caching of AWS pricing data
type PriceCache struct {
	cacheFile  string
	priceCache map[string]float64
	cacheLock  sync.RWMutex
	saveLock   sync.Mutex
}

// NewPriceCache creates a new price cache instance
func NewPriceCache(cacheFile string) (*PriceCache, error) {
	if cacheFile == "" {
		cacheFile = "cost_estimator.json"
	}

	// Ensure cache directory exists
	cacheDir := filepath.Dir(cacheFile)
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	pc := &PriceCache{
		cacheFile:  cacheFile,
		priceCache: make(map[string]float64),
	}

	if err := pc.Load(); err != nil {
		logging.Error("Failed to load price cache", err, nil)
	}

	return pc, nil
}

// Get retrieves a price from the cache
func (pc *PriceCache) Get(key string) (float64, bool) {
	pc.cacheLock.RLock()
	defer pc.cacheLock.RUnlock()
	price, ok := pc.priceCache[key]
	return price, ok
}

// Set stores a price in the cache
func (pc *PriceCache) Set(key string, price float64) {
	pc.cacheLock.Lock()
	pc.priceCache[key] = price
	pc.cacheLock.Unlock()
}

// Load reads the cache from disk
func (pc *PriceCache) Load() error {
	data, err := os.ReadFile(pc.cacheFile)
	if err != nil {
		if os.IsNotExist(err) {
			pc.cacheLock.Lock()
			pc.priceCache = make(map[string]float64)
			pc.cacheLock.Unlock()
			return nil
		}
		return fmt.Errorf("failed to read cache file: %w", err)
	}

	var cache map[string]float64
	if err := json.Unmarshal(data, &cache); err != nil {
		return fmt.Errorf("failed to parse cache data: %w", err)
	}

	pc.cacheLock.Lock()
	pc.priceCache = cache
	pc.cacheLock.Unlock()

	return nil
}

// Save writes the cache to disk
func (pc *PriceCache) Save() error {
	pc.saveLock.Lock()
	defer pc.saveLock.Unlock()

	pc.cacheLock.RLock()
	cache := make(map[string]float64, len(pc.priceCache))
	for k, v := range pc.priceCache {
		cache[k] = v
	}
	pc.cacheLock.RUnlock()

	data, err := json.Marshal(cache)
	if err != nil {
		return fmt.Errorf("failed to marshal cache data: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(pc.cacheFile), 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	tempFile := pc.cacheFile + ".tmp"
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp cache file: %w", err)
	}

	if err := os.Rename(tempFile, pc.cacheFile); err != nil {
		os.Remove(tempFile)
		return fmt.Errorf("failed to rename temp cache file: %w", err)
	}

	logging.Debug("Cache saved successfully", map[string]interface{}{
		"cache_file": pc.cacheFile,
		"entries":    len(cache),
	})

	return nil
}
