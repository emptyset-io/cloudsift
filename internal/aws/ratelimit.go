package aws

import (
	"context"
	"math"
	"sync"
	"time"

	"cloudsift/internal/config"
	"cloudsift/internal/logging"
)

// RateLimiter implements rate limiting with exponential backoff
type RateLimiter struct {
	tokens         chan struct{}
	interval       time.Duration
	maxRetries     int
	baseDelay      time.Duration
	maxDelay       time.Duration
	mu             sync.RWMutex
	failureCount   int
	lastFailure    time.Time
	backoffResetAt time.Time
}

// NewRateLimiter creates a new rate limiter with the specified rate and backoff settings.
// If cfg is nil, it uses the DefaultRateLimitConfig.
func NewRateLimiter(cfg *config.RateLimitConfig) *RateLimiter {
	if cfg == nil {
		cfg = &config.DefaultRateLimitConfig
	}

	tokenCount := int(math.Ceil(cfg.RequestsPerSecond))
	interval := time.Second / time.Duration(cfg.RequestsPerSecond)

	rl := &RateLimiter{
		tokens:     make(chan struct{}, tokenCount),
		interval:   interval,
		maxRetries: cfg.MaxRetries,
		baseDelay:  cfg.BaseDelay,
		maxDelay:   cfg.MaxDelay,
	}

	// Initialize token bucket
	for i := 0; i < tokenCount; i++ {
		rl.tokens <- struct{}{}
	}

	// Start token replenishment
	go rl.replenish()

	return rl
}

// replenish continuously replenishes tokens at the specified rate
func (rl *RateLimiter) replenish() {
	ticker := time.NewTicker(rl.interval)
	defer ticker.Stop()

	for range ticker.C {
		select {
		case rl.tokens <- struct{}{}:
		default:
			// Token bucket is full
		}
	}
}

// getCurrentBackoff calculates the current backoff duration based on failure count
func (rl *RateLimiter) getCurrentBackoff() time.Duration {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	// Reset backoff if enough time has passed since last failure
	if time.Since(rl.lastFailure) > time.Minute*5 {
		return 0
	}

	// Calculate exponential backoff
	backoff := float64(rl.baseDelay) * math.Pow(2, float64(rl.failureCount-1))
	if backoff > float64(rl.maxDelay) {
		backoff = float64(rl.maxDelay)
	}
	return time.Duration(backoff)
}

// Wait waits for rate limit with exponential backoff
func (rl *RateLimiter) Wait(ctx context.Context) error {
	// Check and apply backoff if needed
	backoff := rl.getCurrentBackoff()
	if backoff > 0 {
		logging.Debug("Rate limiter applying backoff", map[string]interface{}{
			"backoff_ms": backoff.Milliseconds(),
		})
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
	}

	// Wait for a token
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-rl.tokens:
		return nil
	}
}

// OnSuccess records a successful API call and potentially resets backoff
func (rl *RateLimiter) OnSuccess() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Reset failure count after consistent success
	if rl.failureCount > 0 && time.Since(rl.lastFailure) > time.Minute {
		prevCount := rl.failureCount
		rl.failureCount = 0
		rl.backoffResetAt = time.Now()

		logging.Debug("Rate limiter backoff reset", map[string]interface{}{
			"previous_failures":     prevCount,
			"time_since_failure_ms": time.Since(rl.lastFailure).Milliseconds(),
		})
	}
}

// OnFailure records a failed API call and updates backoff parameters
func (rl *RateLimiter) OnFailure() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.failureCount++
	rl.lastFailure = time.Now()

	// Cap failure count at maxRetries
	if rl.failureCount > rl.maxRetries {
		rl.failureCount = rl.maxRetries
	}

	// Log backoff state
	logging.Debug("Rate limiter backoff triggered", map[string]interface{}{
		"failure_count": rl.failureCount,
		"backoff_ms":    rl.getCurrentBackoff().Milliseconds(),
	})
}
