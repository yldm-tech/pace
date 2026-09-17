package auth

import (
	"context"
	"encoding/json"
	"testing"

	miniredis "github.com/alicebob/miniredis/v2"
	redis "github.com/redis/go-redis/v9"
)

func TestMagicCodeLifecycleAndAttemptCap(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := NewRedisMagicStore(client)
	ctx := context.Background()

	key, token, authenticationError, err := store.Generate(ctx, "user@pace.test", true)
	if err != nil || authenticationError != nil {
		t.Fatalf("generate error = %v auth=%v", err, authenticationError)
	}
	if key != "magic_user@pace.test" || len(token) != 6 {
		t.Fatalf("key=%q token=%q", key, token)
	}
	for attempt := 1; attempt < maxMagicVerifyAttempts; attempt++ {
		authenticationError, err = store.Verify(ctx, key, "000000", "user@pace.test", true)
		if err != nil || authenticationError == nil || authenticationError.Code != ErrorInvalidMagicCodeSignIn {
			t.Fatalf("attempt %d: error=%v auth=%#v", attempt, err, authenticationError)
		}
	}
	authenticationError, err = store.Verify(ctx, key, "000000", "user@pace.test", true)
	if err != nil || authenticationError == nil || authenticationError.Code != ErrorEmailCodeAttemptExhaustedSignIn {
		t.Fatalf("exhaustion: error=%v auth=%#v", err, authenticationError)
	}
	if server.Exists(key) || server.Exists(key+":verify_attempts") {
		t.Fatal("exhaustion should delete token and counter")
	}
}

func TestMagicCodeRegenerationAndSuccess(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := NewRedisMagicStore(client)
	ctx := context.Background()
	key, _, _, err := store.Generate(ctx, "new@pace.test", false)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, _ = store.Generate(ctx, "new@pace.test", false)
	encoded, err := client.Get(ctx, key).Bytes()
	if err != nil {
		t.Fatal(err)
	}
	var value magicCodeValue
	if err := json.Unmarshal(encoded, &value); err != nil {
		t.Fatal(err)
	}
	if value.CurrentAttempt != 1 {
		t.Fatalf("generation attempt = %d", value.CurrentAttempt)
	}
	if authenticationError, err := store.Verify(ctx, key, value.Token, value.Email, false); err != nil || authenticationError != nil {
		t.Fatalf("verify error=%v auth=%v", err, authenticationError)
	}
	if server.Exists(key) {
		t.Fatal("successful verification should consume token")
	}
}
