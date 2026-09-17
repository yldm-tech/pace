package auth

import (
	"context"
	"testing"
	"time"
)

func TestAuthenticationRateLimitIsSharedByIdentity(t *testing.T) {
	limiter, err := newMemoryRateLimiter("2/minute")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	limiter.clock = func() time.Time { return now }
	for attempt := 0; attempt < 2; attempt++ {
		allowed, err := limiter.Allow(context.Background(), "127.0.0.1")
		if err != nil || !allowed {
			t.Fatalf("attempt %d allowed=%t err=%v", attempt+1, allowed, err)
		}
	}
	allowed, err := limiter.Allow(context.Background(), "127.0.0.1")
	if err != nil || allowed {
		t.Fatalf("third attempt allowed=%t err=%v", allowed, err)
	}
	now = now.Add(time.Minute + time.Second)
	allowed, err = limiter.Allow(context.Background(), "127.0.0.1")
	if err != nil || !allowed {
		t.Fatalf("attempt after window allowed=%t err=%v", allowed, err)
	}
}

func TestParseDjangoRESTFrameworkRates(t *testing.T) {
	for _, rate := range []string{"10/second", "10/minute", "3/hour", "100/day"} {
		if _, _, err := parseRate(rate); err != nil {
			t.Fatalf("parseRate(%q): %v", rate, err)
		}
	}
}
