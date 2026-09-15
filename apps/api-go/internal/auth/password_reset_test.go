package auth

import (
	"testing"
	"time"
)

func TestPasswordResetTokenMatchesDjangoGoldenVector(t *testing.T) {
	lastLogin := time.Date(2024, time.January, 2, 3, 4, 5, 678901000, time.UTC)
	user := &User{
		ID: "018f3c3e-1234-7abc-9def-1234567890ab", Email: "user@pace.test",
		Password:  "pbkdf2_sha256$1000$abcdefghijklmnopqrstuv$gyIKGYrTlRTAwlDBIge8uQnhX+Bk/ZRF8dGfP4K2Zjc=",
		LastLogin: &lastLogin,
	}
	tokens := NewPasswordResetTokens("pace-test-secret", nil)
	tokens.clock = func() time.Time { return time.Date(2025, time.January, 2, 3, 4, 5, 0, time.UTC) }
	uid, token := tokens.Make(user)
	if uid != "MDE4ZjNjM2UtMTIzNC03YWJjLTlkZWYtMTIzNDU2Nzg5MGFi" {
		t.Fatalf("uid = %q", uid)
	}
	if token != "cizf6t-78dd129af09b339ebb2724cb78716ef4" {
		t.Fatalf("token = %q", token)
	}
	if !tokens.Check(user, token) {
		t.Fatal("fresh token should verify")
	}
	tokens.clock = func() time.Time { return time.Date(2025, time.January, 2, 4, 4, 6, 0, time.UTC) }
	if tokens.Check(user, token) {
		t.Fatal("token older than Django's one-hour timeout should expire")
	}
}
