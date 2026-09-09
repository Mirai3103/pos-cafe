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
	stopCh   chan struct{}
}

func NewRateLimiter(maxAttempts int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		attempts: make(map[string][]time.Time),
		max:      maxAttempts,
		window:   window,
		stopCh:   make(chan struct{}),
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

// StartCleanup launches a background goroutine that evicts stale keys
// every 5 minutes. Call the returned function to stop the goroutine.
func (r *RateLimiter) StartCleanup() func() {
	ticker := time.NewTicker(5 * time.Minute)
	go func() {
		for {
			select {
			case <-ticker.C:
				r.evictStale()
			case <-r.stopCh:
				ticker.Stop()
				return
			}
		}
	}()
	return func() { close(r.stopCh) }
}

func (r *RateLimiter) evictStale() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-r.window)
	for key, attempts := range r.attempts {
		valid := attempts[:0]
		for _, a := range attempts {
			if a.After(cutoff) {
				valid = append(valid, a)
			}
		}
		if len(valid) == 0 {
			delete(r.attempts, key)
		} else {
			r.attempts[key] = valid
		}
	}
}
