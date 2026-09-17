package auth

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

type RateLimiter interface {
	Allow(ctx context.Context, identity string) (bool, error)
}

type memoryRateLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	clock   func() time.Time
	history map[string][]time.Time
}

func newMemoryRateLimiter(rate string) (*memoryRateLimiter, error) {
	limit, window, err := parseRate(rate)
	if err != nil {
		return nil, err
	}
	return &memoryRateLimiter{limit: limit, window: window, clock: time.Now, history: make(map[string][]time.Time)}, nil
}

func (limiter *memoryRateLimiter) Allow(_ context.Context, identity string) (bool, error) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	now := limiter.clock()
	cutoff := now.Add(-limiter.window)
	history := limiter.history[identity]
	kept := history[:0]
	for _, timestamp := range history {
		if timestamp.After(cutoff) {
			kept = append(kept, timestamp)
		}
	}
	if len(kept) >= limiter.limit {
		limiter.history[identity] = kept
		return false, nil
	}
	limiter.history[identity] = append(kept, now)
	return true, nil
}

func parseRate(rate string) (int, time.Duration, error) {
	parts := strings.Split(strings.TrimSpace(rate), "/")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid authentication rate %q", rate)
	}
	limit, err := strconv.Atoi(parts[0])
	if err != nil || limit <= 0 {
		return 0, 0, fmt.Errorf("invalid authentication rate %q", rate)
	}
	var window time.Duration
	switch strings.ToLower(strings.TrimSpace(parts[1])) {
	case "s", "sec", "second", "seconds":
		window = time.Second
	case "m", "min", "minute", "minutes":
		window = time.Minute
	case "h", "hour", "hours":
		window = time.Hour
	case "d", "day", "days":
		window = 24 * time.Hour
	default:
		return 0, 0, fmt.Errorf("invalid authentication rate period %q", parts[1])
	}
	return limit, window, nil
}
