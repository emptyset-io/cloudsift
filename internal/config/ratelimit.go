package config

import "time"

// RateLimitConfig holds configuration for rate limiting
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
		RequestsPerSecond: 5.0, // 5 requests per second by default
		MaxRetries:        10,  // 10 retries
		BaseDelay:         time.Second,
		MaxDelay:          time.Second * 120,
	}
)
