package auth

import (
	"context"
	"fmt"

	redis "github.com/redis/go-redis/v9"
)

// CacheInvalidator mirrors the small set of Django cache side effects made by
// authentication. It is optional so contract tests can use an in-memory fake.
type CacheInvalidator interface {
	InvalidatePattern(ctx context.Context, pattern string) error
}

type RedisCacheInvalidator struct {
	client redis.UniversalClient
}

func NewRedisCacheInvalidator(client redis.UniversalClient) *RedisCacheInvalidator {
	return &RedisCacheInvalidator{client: client}
}

func (invalidator *RedisCacheInvalidator) InvalidatePattern(ctx context.Context, pattern string) error {
	if invalidator == nil || invalidator.client == nil {
		return nil
	}
	var cursor uint64
	for {
		keys, next, err := invalidator.client.Scan(ctx, cursor, pattern, 200).Result()
		if err != nil {
			return fmt.Errorf("scan cache keys: %w", err)
		}
		if len(keys) > 0 {
			if err := invalidator.client.Del(ctx, keys...).Err(); err != nil {
				return fmt.Errorf("delete cache keys: %w", err)
			}
		}
		cursor = next
		if cursor == 0 {
			return nil
		}
	}
}

func invalidateUserCache(ctx context.Context, invalidator CacheInvalidator, userID string) error {
	if invalidator == nil {
		return nil
	}
	return invalidator.InvalidatePattern(ctx, "*/api/users/me/:"+userID+"*")
}
