package ratelimit

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"cloudsift/internal/logging"
)

const (
	maxRetries    = 5
	baseDelay     = 100 * time.Millisecond
	maxDelay      = 30 * time.Second
	jitterPercent = 0.1
	
	// Default rate limits
	defaultRequestsPerSecond = 5 // Conservative default of 5 requests per second
)

var (
	// Global instance of the service limiter registry
	globalRegistry = &ServiceLimiterRegistry{
		limiters: make(map[string]*ServiceLimiter),
	}
)

// ServiceConfig holds configuration for a service's rate limits
type ServiceConfig struct {
	// Default requests per second for APIs not explicitly configured
	DefaultRequestsPerSecond int
	// Specific API rate limits, overrides default
	APILimits map[string]int
}

// DefaultServiceConfig returns a default configuration for a service
func DefaultServiceConfig() ServiceConfig {
	return ServiceConfig{
		DefaultRequestsPerSecond: defaultRequestsPerSecond,
		APILimits: map[string]int{
			// IAM specific limits - more generous for common operations
			"GetRole":                     10, // 10 requests per second
			"GetRolePolicy":              10,
			"ListAttachedRolePolicies":   10,
			"ListRolePolicies":           10,
			"ListInstanceProfilesForRole": 10,
			// Add more API limits as needed
		},
	}
}

// ServiceLimiterRegistry manages a global registry of service limiters
type ServiceLimiterRegistry struct {
	mu       sync.RWMutex
	limiters map[string]*ServiceLimiter
}

// GetServiceLimiter returns a service limiter for the given service name, creating it if it doesn't exist
func GetServiceLimiter(serviceName string, configs ...ServiceConfig) *ServiceLimiter {
	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()

	if limiter, exists := globalRegistry.limiters[serviceName]; exists {
		return limiter
	}

	limiter := newServiceLimiter(configs...)
	globalRegistry.limiters[serviceName] = limiter
	return limiter
}

// ServiceLimiter manages rate limiting for a specific AWS service
type ServiceLimiter struct {
	mu            sync.Mutex
	lastCallTimes map[string]time.Time
	config        ServiceConfig
}

// newServiceLimiter creates a new ServiceLimiter with optional configuration
func newServiceLimiter(configs ...ServiceConfig) *ServiceLimiter {
	config := DefaultServiceConfig()
	if len(configs) > 0 {
		config = configs[0]
	}
	
	return &ServiceLimiter{
		lastCallTimes: make(map[string]time.Time),
		config:        config,
	}
}

// getInterval returns the minimum interval between requests for a given API
func (l *ServiceLimiter) getInterval(apiName string) time.Duration {
	if rps, ok := l.config.APILimits[apiName]; ok {
		return time.Second / time.Duration(rps)
	}
	return time.Second / time.Duration(l.config.DefaultRequestsPerSecond)
}

// addJitter adds random jitter to the delay
func addJitter(delay time.Duration) time.Duration {
	jitter := float64(delay) * jitterPercent
	return delay + time.Duration(jitter*(rand.Float64()*2-1))
}

// shouldRetry determines if an error is retryable
func shouldRetry(err error) bool {
	if err == nil {
		return false
	}

	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "throttling") ||
		strings.Contains(errStr, "rate exceeded") ||
		strings.Contains(errStr, "limit exceeded") ||
		strings.Contains(errStr, "too many requests")
}

// Execute executes a function with rate limiting and exponential backoff
func (l *ServiceLimiter) Execute(ctx context.Context, apiName string, operation func() error) error {
	l.mu.Lock()
	lastCall, exists := l.lastCallTimes[apiName]
	minWait := l.getInterval(apiName)
	
	now := time.Now()
	if exists && now.Sub(lastCall) < minWait {
		sleepTime := minWait - now.Sub(lastCall)
		l.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sleepTime):
		}
		l.mu.Lock()
	}
	l.lastCallTimes[apiName] = now
	l.mu.Unlock()

	var err error
	delay := baseDelay

	for attempt := 0; attempt < maxRetries; attempt++ {
		err = operation()
		if !shouldRetry(err) {
			return err
		}

		logging.Debug("Rate limited, retrying operation", map[string]interface{}{
			"api":      apiName,
			"attempt":  attempt + 1,
			"maxRetry": maxRetries,
			"delay":    delay.String(),
		})

		delayWithJitter := addJitter(delay)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delayWithJitter):
		}

		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
		}
	}

	return fmt.Errorf("max retries exceeded for %s: %w", apiName, err)
}
