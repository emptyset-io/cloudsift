package config

import "time"

// RateLimitConfig defines rate limiting parameters
type RateLimitConfig struct {
	// RequestsPerSecond is the number of requests allowed per second
	RequestsPerSecond float64
	// MaxRetries is the maximum number of retries before giving up
	MaxRetries int
	// BaseDelay is the initial delay duration for backoff
	BaseDelay time.Duration
	// MaxDelay is the maximum delay duration for backoff
	MaxDelay time.Duration
}

var (
	// DefaultRateLimitConfig provides default values for rate limiting
	DefaultRateLimitConfig = RateLimitConfig{
		RequestsPerSecond: 20.0,                   // AWS APIs generally allow 20+ TPS for most operations
		MaxRetries:        10,                     // Keep 10 retries as it's reasonable
		BaseDelay:         100 * time.Millisecond, // Start with 100ms delay
		MaxDelay:          120 * time.Second,      // Keep 2 minute max delay
	}
)
