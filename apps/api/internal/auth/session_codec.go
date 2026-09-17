package auth

import (
	"bytes"
	"compress/zlib"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	djangoSessionSalt = "django.contrib.sessions.SessionStore"
	base62Alphabet    = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
)

type SessionCodec struct {
	secretKeys []string
	clock      func() time.Time
}

func NewSessionCodec(secretKey string, fallbackKeys []string) (*SessionCodec, error) {
	if strings.TrimSpace(secretKey) == "" {
		return nil, fmt.Errorf("SECRET_KEY is required for Django-compatible sessions")
	}
	keys := make([]string, 0, len(fallbackKeys)+1)
	keys = append(keys, secretKey)
	for _, key := range fallbackKeys {
		if strings.TrimSpace(key) != "" {
			keys = append(keys, key)
		}
	}
	return &SessionCodec{secretKeys: keys, clock: time.Now}, nil
}

func (codec *SessionCodec) Encode(value map[string]any) (string, error) {
	serialized, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("serialize session: %w", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(serialized)
	compressed, err := zlibCompress(serialized)
	if err != nil {
		return "", err
	}
	if len(compressed) < len(serialized)-1 {
		payload = "." + base64.RawURLEncoding.EncodeToString(compressed)
	}
	timestamp := base62Encode(codec.clock().Unix())
	unsigned := payload + ":" + timestamp
	return unsigned + ":" + djangoSignature(djangoSessionSalt, unsigned, codec.secretKeys[0]), nil
}

func (codec *SessionCodec) Decode(encoded string) (map[string]any, error) {
	unsigned, signature, found := strings.Cut(encoded, ":")
	if !found {
		return nil, fmt.Errorf("invalid session data")
	}
	// Cut at the last separator: payload itself is followed by a timestamp.
	lastSeparator := strings.LastIndex(encoded, ":")
	if lastSeparator <= 0 {
		return nil, fmt.Errorf("invalid session signature")
	}
	unsigned = encoded[:lastSeparator]
	signature = encoded[lastSeparator+1:]
	valid := false
	for _, key := range codec.secretKeys {
		expected := djangoSignature(djangoSessionSalt, unsigned, key)
		if subtle.ConstantTimeCompare([]byte(signature), []byte(expected)) == 1 {
			valid = true
			break
		}
	}
	if !valid {
		return nil, fmt.Errorf("invalid session signature")
	}
	payload, _, ok := strings.Cut(unsigned, ":")
	if !ok {
		return nil, fmt.Errorf("invalid session timestamp")
	}
	compressed := strings.HasPrefix(payload, ".")
	if compressed {
		payload = strings.TrimPrefix(payload, ".")
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("decode session payload: %w", err)
	}
	if compressed {
		raw, err = zlibDecompress(raw)
		if err != nil {
			return nil, err
		}
	}
	result := make(map[string]any)
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("deserialize session: %w", err)
	}
	return result, nil
}

func djangoSignature(salt, value, secret string) string {
	derivedKey := sha256.Sum256([]byte(salt + "signer" + secret))
	hash := hmac.New(sha256.New, derivedKey[:])
	_, _ = hash.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString(hash.Sum(nil))
}

func zlibCompress(value []byte) ([]byte, error) {
	var output bytes.Buffer
	writer := zlib.NewWriter(&output)
	if _, err := writer.Write(value); err != nil {
		return nil, fmt.Errorf("compress session: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("finish session compression: %w", err)
	}
	return output.Bytes(), nil
}

func zlibDecompress(value []byte) ([]byte, error) {
	reader, err := zlib.NewReader(bytes.NewReader(value))
	if err != nil {
		return nil, fmt.Errorf("open compressed session: %w", err)
	}
	defer reader.Close()
	result, err := io.ReadAll(io.LimitReader(reader, 1024*1024))
	if err != nil {
		return nil, fmt.Errorf("decompress session: %w", err)
	}
	return result, nil
}

func base62Encode(value int64) string {
	if value == 0 {
		return "0"
	}
	result := make([]byte, 0, 12)
	for value > 0 {
		remainder := value % 62
		result = append(result, base62Alphabet[remainder])
		value /= 62
	}
	for left, right := 0, len(result)-1; left < right; left, right = left+1, right-1 {
		result[left], result[right] = result[right], result[left]
	}
	return string(result)
}
