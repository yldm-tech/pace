package auth

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"

	redis "github.com/redis/go-redis/v9"
)

const (
	magicCodeTTL           = 10 * time.Minute
	maxMagicVerifyAttempts = 5
)

type MagicStore interface {
	Generate(ctx context.Context, email string, userExists bool) (key, token string, authenticationError *Error, err error)
	Verify(ctx context.Context, key, code, email string, userExists bool) (*Error, error)
}

type TaskPublisher interface {
	PublishMagicLink(ctx context.Context, email, key, token string) error
	PublishForgotPassword(ctx context.Context, firstName, email, uid, token, currentSite string) error
	PublishUserActivation(ctx context.Context, currentSite, userID string) error
}

type RedisMagicStore struct{ client redis.UniversalClient }

func NewRedisMagicStore(client redis.UniversalClient) *RedisMagicStore {
	return &RedisMagicStore{client: client}
}

type magicCodeValue struct {
	CurrentAttempt int    `json:"current_attempt"`
	Email          string `json:"email"`
	Token          string `json:"token"`
}

func (store *RedisMagicStore) Generate(ctx context.Context, email string, userExists bool) (string, string, *Error, error) {
	key := "magic_" + email
	token, err := randomSixDigitCode()
	if err != nil {
		return "", "", nil, err
	}
	currentAttempt := 0
	value, err := store.client.Get(ctx, key).Bytes()
	if err == nil {
		var current magicCodeValue
		if err := json.Unmarshal(value, &current); err != nil {
			return "", "", nil, fmt.Errorf("decode magic code: %w", err)
		}
		if current.CurrentAttempt > 2 {
			if userExists {
				return "", "", authError(ErrorEmailCodeAttemptExhaustedSignIn, "EMAIL_CODE_ATTEMPT_EXHAUSTED_SIGN_IN", map[string]any{"email": email}), nil
			}
			return "", "", authError(ErrorEmailCodeAttemptExhaustedSignUp, "EMAIL_CODE_ATTEMPT_EXHAUSTED_SIGN_UP", map[string]any{"email": email}), nil
		}
		currentAttempt = current.CurrentAttempt + 1
	} else if !errors.Is(err, redis.Nil) {
		return "", "", nil, err
	}
	encoded, err := json.Marshal(magicCodeValue{CurrentAttempt: currentAttempt, Email: email, Token: token})
	if err != nil {
		return "", "", nil, err
	}
	pipeline := store.client.TxPipeline()
	pipeline.Set(ctx, key, encoded, magicCodeTTL)
	pipeline.Del(ctx, key+":verify_attempts")
	if _, err := pipeline.Exec(ctx); err != nil {
		return "", "", nil, err
	}
	return key, token, nil, nil
}

var incrementVerifyAttempts = redis.NewScript(`
local count = redis.call("INCR", KEYS[1])
if count == 1 then
  redis.call("EXPIRE", KEYS[1], tonumber(ARGV[1]))
end
return count
`)

func (store *RedisMagicStore) Verify(ctx context.Context, key, code, email string, userExists bool) (*Error, error) {
	encoded, err := store.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		if userExists {
			return authError(ErrorExpiredMagicCodeSignIn, "EXPIRED_MAGIC_CODE_SIGN_IN", map[string]any{"email": email}), nil
		}
		return authError(ErrorExpiredMagicCodeSignUp, "EXPIRED_MAGIC_CODE_SIGN_UP", map[string]any{"email": email}), nil
	}
	if err != nil {
		return nil, err
	}
	var value magicCodeValue
	if err := json.Unmarshal(encoded, &value); err != nil {
		return nil, fmt.Errorf("decode magic code: %w", err)
	}
	if value.Token == code {
		if err := store.client.Del(ctx, key, key+":verify_attempts").Err(); err != nil {
			return nil, err
		}
		return nil, nil
	}
	ttl, err := store.client.TTL(ctx, key).Result()
	if err != nil || ttl <= 0 {
		ttl = time.Second
	}
	attempts, err := incrementVerifyAttempts.Run(ctx, store.client, []string{key + ":verify_attempts"}, max(1, int(ttl.Seconds()))).Int()
	if err != nil {
		return nil, err
	}
	if attempts >= maxMagicVerifyAttempts {
		if err := store.client.Del(ctx, key, key+":verify_attempts").Err(); err != nil {
			return nil, err
		}
		if userExists {
			return authError(ErrorEmailCodeAttemptExhaustedSignIn, "EMAIL_CODE_ATTEMPT_EXHAUSTED_SIGN_IN", map[string]any{"email": email}), nil
		}
		return authError(ErrorEmailCodeAttemptExhaustedSignUp, "EMAIL_CODE_ATTEMPT_EXHAUSTED_SIGN_UP", map[string]any{"email": email}), nil
	}
	if userExists {
		return authError(ErrorInvalidMagicCodeSignIn, "INVALID_MAGIC_CODE_SIGN_IN", map[string]any{"email": email}), nil
	}
	return authError(ErrorInvalidMagicCodeSignUp, "INVALID_MAGIC_CODE_SIGN_UP", map[string]any{"email": email}), nil
}

func randomSixDigitCode() (string, error) {
	value, err := rand.Int(rand.Reader, big.NewInt(900000))
	if err != nil {
		return "", fmt.Errorf("generate magic code: %w", err)
	}
	return fmt.Sprintf("%06d", value.Int64()+100000), nil
}
