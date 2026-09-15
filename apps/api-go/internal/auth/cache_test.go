package auth

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	redis "github.com/redis/go-redis/v9"
)

func TestRedisCacheInvalidatorRemovesMatchingDjangoKeys(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	invalidator := NewRedisCacheInvalidator(client)
	ctx := context.Background()
	if err := client.Set(ctx, "/api/workspaces/acme/members/:user-a", "cached", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.Set(ctx, "/api/workspaces/acme/members/:user-b", "cached", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.Set(ctx, "/api/workspaces/other/members/:user-a", "keep", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := invalidator.InvalidatePattern(ctx, "*/api/workspaces/acme/members/*"); err != nil {
		t.Fatal(err)
	}
	if exists, err := client.Exists(ctx, "/api/workspaces/acme/members/:user-a").Result(); err != nil || exists != 0 {
		t.Fatalf("matching key exists=%d err=%v", exists, err)
	}
	if exists, err := client.Exists(ctx, "/api/workspaces/other/members/:user-a").Result(); err != nil || exists != 1 {
		t.Fatalf("unmatched key exists=%d err=%v", exists, err)
	}
}
