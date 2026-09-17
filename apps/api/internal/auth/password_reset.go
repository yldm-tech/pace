package auth

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const passwordResetTimeout = time.Hour

var errPasswordResetUIDUnicode = errors.New("password reset user id is not valid UTF-8")

type PasswordResetTokens struct {
	secretKeys []string
	clock      func() time.Time
}

func NewPasswordResetTokens(secretKey string, fallbackKeys []string) *PasswordResetTokens {
	return &PasswordResetTokens{secretKeys: append([]string{secretKey}, fallbackKeys...), clock: time.Now}
}

func (tokens *PasswordResetTokens) Make(user *User) (uid, token string) {
	uid = base64.RawURLEncoding.EncodeToString([]byte(user.ID))
	timestamp := secondsSince2001(tokens.clock())
	return uid, tokens.makeWithTimestamp(user, timestamp, tokens.secretKeys[0])
}

func (tokens *PasswordResetTokens) Check(user *User, token string) bool {
	timestampPart, _, found := strings.Cut(token, "-")
	if !found {
		return false
	}
	timestamp, err := strconv.ParseInt(timestampPart, 36, 64)
	if err != nil {
		return false
	}
	valid := false
	for _, secret := range tokens.secretKeys {
		expected := tokens.makeWithTimestamp(user, timestamp, secret)
		if subtle.ConstantTimeCompare([]byte(expected), []byte(token)) == 1 {
			valid = true
			break
		}
	}
	if !valid {
		return false
	}
	age := time.Duration(secondsSince2001(tokens.clock())-timestamp) * time.Second
	return age <= passwordResetTimeout
}

func (tokens *PasswordResetTokens) makeWithTimestamp(user *User, timestamp int64, secret string) string {
	timestampBase36 := strconv.FormatInt(timestamp, 36)
	lastLogin := ""
	if user.LastLogin != nil {
		lastLogin = user.LastLogin.UTC().Truncate(time.Second).Format("2006-01-02 15:04:05")
	}
	hashValue := user.ID + user.Password + lastLogin + strconv.FormatInt(timestamp, 10) + user.Email
	digest := hex.EncodeToString(saltedHMAC(
		"django.contrib.auth.tokens.PasswordResetTokenGenerator",
		hashValue,
		secret,
	))
	shortened := make([]byte, 0, len(digest)/2)
	for index := 0; index < len(digest); index += 2 {
		shortened = append(shortened, digest[index])
	}
	return fmt.Sprintf("%s-%s", timestampBase36, shortened)
}

func secondsSince2001(value time.Time) int64 {
	epoch := time.Date(2001, time.January, 1, 0, 0, 0, 0, time.UTC)
	return int64(value.UTC().Sub(epoch).Seconds())
}

func decodePasswordResetUID(uid string) (string, error) {
	value, err := base64.RawURLEncoding.DecodeString(uid)
	if err != nil {
		return "", fmt.Errorf("decode password reset user: %w", err)
	}
	if !utf8.Valid(value) {
		return "", errPasswordResetUIDUnicode
	}
	return string(value), nil
}
