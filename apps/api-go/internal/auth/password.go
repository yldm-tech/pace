package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec -- required only to verify Django's legacy PBKDF2-SHA1 hashes.
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/pbkdf2"
	"golang.org/x/crypto/scrypt"
)

const djangoPBKDF2Iterations = 1_000_000

const djangoSaltCharacters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func HashPassword(password string) (string, error) {
	salt, err := randomString(22, djangoSaltCharacters)
	if err != nil {
		return "", err
	}
	return encodePassword(password, salt, djangoPBKDF2Iterations), nil
}

func encodePassword(password, salt string, iterations int) string {
	digest := pbkdf2.Key([]byte(password), []byte(salt), iterations, 32, sha256.New)
	return fmt.Sprintf("pbkdf2_sha256$%d$%s$%s", iterations, salt, base64.StdEncoding.EncodeToString(digest))
}

func VerifyPassword(password, encoded string) bool {
	algorithm, _, found := strings.Cut(encoded, "$")
	if !found {
		return false
	}
	switch algorithm {
	case "pbkdf2_sha256", "pbkdf2_sha1":
		parts := strings.SplitN(encoded, "$", 4)
		if len(parts) != 4 {
			return false
		}
		iterations, err := strconv.Atoi(parts[1])
		if err != nil || iterations <= 0 || iterations > 10_000_000 {
			return false
		}
		var digest []byte
		if algorithm == "pbkdf2_sha256" {
			digest = pbkdf2.Key([]byte(password), []byte(parts[2]), iterations, 32, sha256.New)
		} else {
			digest = pbkdf2.Key([]byte(password), []byte(parts[2]), iterations, 20, sha1.New)
		}
		computed := fmt.Sprintf("%s$%d$%s$%s", algorithm, iterations, parts[2], base64.StdEncoding.EncodeToString(digest))
		return subtle.ConstantTimeCompare([]byte(computed), []byte(encoded)) == 1
	case "scrypt":
		parts := strings.SplitN(encoded, "$", 7)
		if len(parts) != 6 {
			return false
		}
		n, nErr := strconv.Atoi(parts[1])
		r, rErr := strconv.Atoi(parts[3])
		p, pErr := strconv.Atoi(parts[4])
		if nErr != nil || rErr != nil || pErr != nil || n < 2 || n > 1<<20 || n&(n-1) != 0 || r <= 0 || p <= 0 {
			return false
		}
		digest, err := scrypt.Key([]byte(password), []byte(parts[2]), n, r, p, 64)
		if err != nil {
			return false
		}
		computed := fmt.Sprintf("scrypt$%d$%s$%d$%d$%s", n, parts[2], r, p, base64.StdEncoding.EncodeToString(digest))
		return subtle.ConstantTimeCompare([]byte(computed), []byte(encoded)) == 1
	case "bcrypt_sha256":
		data := strings.TrimPrefix(encoded, "bcrypt_sha256$")
		prehash := sha256.Sum256([]byte(password))
		return bcrypt.CompareHashAndPassword([]byte(data), []byte(hex.EncodeToString(prehash[:]))) == nil
	default:
		return false
	}
}

func sessionAuthHash(encodedPassword, secret string) string {
	return hex.EncodeToString(saltedHMAC(
		"django.contrib.auth.models.AbstractBaseUser.get_session_auth_hash",
		encodedPassword,
		secret,
	))
}

func saltedHMAC(keySalt, value, secret string) []byte {
	derivedKey := sha256.Sum256([]byte(keySalt + secret))
	hash := hmac.New(sha256.New, derivedKey[:])
	_, _ = hash.Write([]byte(value))
	return hash.Sum(nil)
}

func randomString(length int, alphabet string) (string, error) {
	if length <= 0 || len(alphabet) == 0 || len(alphabet) > 256 {
		return "", fmt.Errorf("invalid random string parameters")
	}
	result := make([]byte, length)
	limit := byte(256 - (256 % len(alphabet)))
	randomByte := []byte{0}
	for index := range result {
		for {
			if _, err := rand.Read(randomByte); err != nil {
				return "", fmt.Errorf("generate random string: %w", err)
			}
			if randomByte[0] < limit {
				result[index] = alphabet[int(randomByte[0])%len(alphabet)]
				break
			}
		}
	}
	return string(result), nil
}
