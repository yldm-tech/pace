package auth

import (
	"context"
	"fmt"
	"time"

	redis "github.com/redis/go-redis/v9"
)

func OpenRedis(ctx context.Context, redisURL string) (redis.UniversalClient, error) {
	if redisURL == "" {
		return nil, fmt.Errorf("REDIS_URL is required for authentication")
	}
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse REDIS_URL: %w", err)
	}
	client := redis.NewClient(options)
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("connect to Redis: %w", err)
	}
	return client, nil
}

type RedisRateLimiter struct {
	client redis.UniversalClient
	limit  int
	window time.Duration
	clock  func() time.Time
}

func NewRedisRateLimiter(client redis.UniversalClient, rate string) (*RedisRateLimiter, error) {
	limit, window, err := parseRate(rate)
	if err != nil {
		return nil, err
	}
	return &RedisRateLimiter{client: client, limit: limit, window: window, clock: time.Now}, nil
}

var rateLimitScript = redis.NewScript(`
redis.call("ZREMRANGEBYSCORE", KEYS[1], 0, ARGV[1])
local count = redis.call("ZCARD", KEYS[1])
if count >= tonumber(ARGV[2]) then
  return 0
end
redis.call("ZADD", KEYS[1], ARGV[3], ARGV[4])
redis.call("PEXPIRE", KEYS[1], ARGV[5])
return 1
`)

func (limiter *RedisRateLimiter) Allow(ctx context.Context, identity string) (bool, error) {
	now := limiter.clock().UnixMilli()
	member, err := randomHex(8)
	if err != nil {
		return false, err
	}
	result, err := rateLimitScript.Run(
		ctx,
		limiter.client,
		[]string{"pace:auth:throttle:" + identity},
		now-limiter.window.Milliseconds(),
		limiter.limit,
		now,
		fmt.Sprintf("%d:%s", now, member),
		limiter.window.Milliseconds(),
	).Int()
	return result == 1, err
}
