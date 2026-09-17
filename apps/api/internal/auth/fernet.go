package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"time"

	"golang.org/x/crypto/pbkdf2"
)

// DecryptConfiguration is decrypt_data, exported for the admin console — which reads the stored settings back out in full for whoever is signed in as an administrator.
func DecryptConfiguration(value, secret string) (string, error) {
	return decryptDjangoConfiguration(value, secret)
}

func decryptDjangoConfiguration(value, secret string) (string, error) {
	key := pbkdf2.Key([]byte(secret), []byte("salt"), 100_000, 32, sha256.New)
	token, err := base64.URLEncoding.DecodeString(value)
	if err != nil {
		if token, err = base64.RawURLEncoding.DecodeString(value); err != nil {
			return "", fmt.Errorf("decode encrypted configuration: %w", err)
		}
	}
	if len(token) < 1+8+aes.BlockSize+aes.BlockSize+sha256.Size || token[0] != 0x80 {
		return "", fmt.Errorf("invalid encrypted configuration token")
	}
	signed := token[:len(token)-sha256.Size]
	providedSignature := token[len(token)-sha256.Size:]
	signature := hmac.New(sha256.New, key[:16])
	_, _ = signature.Write(signed)
	if subtle.ConstantTimeCompare(providedSignature, signature.Sum(nil)) != 1 {
		return "", fmt.Errorf("invalid encrypted configuration signature")
	}
	iv := token[9 : 9+aes.BlockSize]
	ciphertext := token[9+aes.BlockSize : len(token)-sha256.Size]
	if len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
		return "", fmt.Errorf("invalid encrypted configuration ciphertext")
	}
	block, err := aes.NewCipher(key[16:])
	if err != nil {
		return "", err
	}
	plaintext := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plaintext, ciphertext)
	padding := int(plaintext[len(plaintext)-1])
	if padding < 1 || padding > aes.BlockSize || padding > len(plaintext) {
		return "", fmt.Errorf("invalid encrypted configuration padding")
	}
	for _, value := range plaintext[len(plaintext)-padding:] {
		if int(value) != padding {
			return "", fmt.Errorf("invalid encrypted configuration padding")
		}
	}
	return string(plaintext[:len(plaintext)-padding]), nil
}

// EncryptConfiguration is plane.license.utils.encryption.encrypt_data: a Fernet token under the key derived from SECRET_KEY, which is what an encrypted instance configuration row holds.
//
// An empty value is stored as an empty string rather than as a token, which is what the python guard leaves behind — so an encrypted variable nobody set reads back as empty rather than failing to decrypt.
func EncryptConfiguration(value, secret string) (string, error) {
	if value == "" {
		return "", nil
	}
	key := pbkdf2.Key([]byte(secret), []byte("salt"), 100_000, 32, sha256.New)
	iv := make([]byte, aes.BlockSize)
	if _, err := rand.Read(iv); err != nil {
		return "", err
	}
	padding := aes.BlockSize - len(value)%aes.BlockSize
	plaintext := make([]byte, 0, len(value)+padding)
	plaintext = append(plaintext, value...)
	for index := 0; index < padding; index++ {
		plaintext = append(plaintext, byte(padding))
	}
	block, err := aes.NewCipher(key[16:])
	if err != nil {
		return "", err
	}
	ciphertext := make([]byte, len(plaintext))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, plaintext)

	// The token is version, timestamp, iv, ciphertext, and an HMAC over all of it.
	token := make([]byte, 0, 1+8+len(iv)+len(ciphertext)+sha256.Size)
	token = append(token, 0x80)
	timestamp := make([]byte, 8)
	binary.BigEndian.PutUint64(timestamp, uint64(time.Now().Unix()))
	token = append(token, timestamp...)
	token = append(token, iv...)
	token = append(token, ciphertext...)
	signature := hmac.New(sha256.New, key[:16])
	_, _ = signature.Write(token)
	token = append(token, signature.Sum(nil)...)
	return base64.URLEncoding.EncodeToString(token), nil
}
