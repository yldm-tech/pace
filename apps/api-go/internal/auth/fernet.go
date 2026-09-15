package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"

	"golang.org/x/crypto/pbkdf2"
)

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
