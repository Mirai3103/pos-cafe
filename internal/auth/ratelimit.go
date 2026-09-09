package auth

import (
	"sync"
	"time"
)

type RateLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
	max      int
	window   time.Duration
}

func NewRateLimiter(maxAttempts int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		attempts: make(map[string][]time.Time),
		max:      maxAttempts,
		window:   window,
	}
}

func (r *RateLimiter) Allow(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-r.window)
	attempts := r.attempts[key]
	valid := attempts[:0]
	for _, attempt := range attempts {
		if attempt.After(cutoff) {
			valid = append(valid, attempt)
		}
	}

	if len(valid) >= r.max {
		r.attempts[key] = valid
		return false
	}

	r.attempts[key] = append(valid, now)
	return true
}

func (r *RateLimiter) Reset(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.attempts, key)
}
