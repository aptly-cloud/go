package aptly

import (
	"math"
	"sync"
	"time"
)

// BackoffConfig holds the configuration for exponential backoff
type BackoffConfig struct {
	InitialDelay time.Duration
	Multiplier   float64
	MaxDelay     time.Duration
	MaxAttempts  int
}

// ExponentialBackoff manages exponential backoff for reconnection attempts
type ExponentialBackoff struct {
	config   BackoffConfig
	attempts int
	mu       sync.RWMutex
	logger   *Logger
}

// NewExponentialBackoff creates a new exponential backoff instance
func NewExponentialBackoff(config BackoffConfig, logger *Logger) *ExponentialBackoff {
	if config.InitialDelay == 0 {
		config.InitialDelay = 3 * time.Second
	}
	if config.Multiplier == 0 {
		config.Multiplier = 1.5
	}
	if config.MaxDelay == 0 {
		config.MaxDelay = 30 * time.Second
	}
	if config.MaxAttempts == 0 {
		config.MaxAttempts = 20
	}

	return &ExponentialBackoff{
		config: config,
		logger: logger,
	}
}

// GetDelay calculates and returns the next delay duration
func (b *ExponentialBackoff) GetDelay() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.attempts++

	// Calculate exponential delay
	delay := float64(b.config.InitialDelay) * math.Pow(b.config.Multiplier, float64(b.attempts-1))
	delayDuration := time.Duration(delay)

	// Cap at max delay
	if delayDuration > b.config.MaxDelay {
		delayDuration = b.config.MaxDelay
	}

	b.logger.Infof("Backoff attempt %d, delay: %v", b.attempts, delayDuration)

	return delayDuration
}

// Reset resets the backoff attempts counter
func (b *ExponentialBackoff) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.attempts = 0
	b.logger.Info("Backoff reset")
}

// GetAttempts returns the current number of attempts
func (b *ExponentialBackoff) GetAttempts() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.attempts
}

// IsMaxAttemptsReached checks if max attempts have been reached
func (b *ExponentialBackoff) IsMaxAttemptsReached() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.attempts >= b.config.MaxAttempts
}

// GetStatus returns the current backoff status
func (b *ExponentialBackoff) GetStatus() map[string]interface{} {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return map[string]interface{}{
		"attempts":    b.attempts,
		"maxAttempts": b.config.MaxAttempts,
		"maxReached":  b.attempts >= b.config.MaxAttempts,
	}
}
