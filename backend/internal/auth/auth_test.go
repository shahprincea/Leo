package auth

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
)

// signWithTTL is a test helper that creates a JWT with an arbitrary TTL,
// allowing negative values to produce already-expired tokens.
func signWithTTL(userID, email, secret string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID: userID,
		Email:  email,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// ---- JWT tests ----

func TestSignAndVerifyAccessToken(t *testing.T) {
	secret := "test-secret"
	userID := "user-123"
	email := "alice@example.com"

	token, err := SignAccessToken(userID, email, secret)
	if err != nil {
		t.Fatalf("SignAccessToken error: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	claims, err := VerifyAccessToken(token, secret)
	if err != nil {
		t.Fatalf("VerifyAccessToken error: %v", err)
	}
	if claims.UserID != userID {
		t.Errorf("UserID: got %q, want %q", claims.UserID, userID)
	}
	if claims.Email != email {
		t.Errorf("Email: got %q, want %q", claims.Email, email)
	}
}

func TestVerifyAccessToken_Expired(t *testing.T) {
	secret := "test-secret"

	token, err := signWithTTL("u1", "u@example.com", secret, -1*time.Minute)
	if err != nil {
		t.Fatalf("signWithTTL: %v", err)
	}

	_, err = VerifyAccessToken(token, secret)
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
}

func TestVerifyAccessToken_WrongSecret(t *testing.T) {
	token, err := SignAccessToken("u1", "u@example.com", "correct-secret")
	if err != nil {
		t.Fatalf("SignAccessToken: %v", err)
	}

	_, err = VerifyAccessToken(token, "wrong-secret")
	if err == nil {
		t.Fatal("expected error for wrong secret, got nil")
	}
}

// ---- Password tests ----

func TestHashAndVerifyPassword(t *testing.T) {
	password := "correct-password"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}

	if err := VerifyPassword(hash, password); err != nil {
		t.Errorf("VerifyPassword correct password: unexpected error: %v", err)
	}

	if err := VerifyPassword(hash, "wrong-password"); err == nil {
		t.Error("VerifyPassword wrong password: expected error, got nil")
	}
}

// ---- Refresh token generation test ----

func TestGenerateRefreshToken(t *testing.T) {
	tok1, err := GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken: %v", err)
	}
	if len(tok1) != 64 {
		t.Errorf("expected 64 hex chars, got %d", len(tok1))
	}

	tok2, err := GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken second call: %v", err)
	}
	if tok1 == tok2 {
		t.Error("expected two different tokens, got identical values")
	}
}

// ---- Redis-backed refresh token tests ----

func newTestRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return mr, rdb
}

func TestStoreAndValidateRefreshToken(t *testing.T) {
	_, rdb := newTestRedis(t)
	ctx := context.Background()

	token, _ := GenerateRefreshToken()
	userID := "user-abc"

	if err := StoreRefreshToken(ctx, rdb, token, userID); err != nil {
		t.Fatalf("StoreRefreshToken: %v", err)
	}

	got, err := ValidateRefreshToken(ctx, rdb, token)
	if err != nil {
		t.Fatalf("ValidateRefreshToken: %v", err)
	}
	if got != userID {
		t.Errorf("ValidateRefreshToken: got %q, want %q", got, userID)
	}
}

func TestRevokeRefreshToken(t *testing.T) {
	_, rdb := newTestRedis(t)
	ctx := context.Background()

	token, _ := GenerateRefreshToken()
	userID := "user-xyz"

	_ = StoreRefreshToken(ctx, rdb, token, userID)
	if err := RevokeRefreshToken(ctx, rdb, token); err != nil {
		t.Fatalf("RevokeRefreshToken: %v", err)
	}

	_, err := ValidateRefreshToken(ctx, rdb, token)
	if err == nil {
		t.Fatal("expected error after revocation, got nil")
	}
}

func TestRefreshToken_Expired(t *testing.T) {
	mr, rdb := newTestRedis(t)
	ctx := context.Background()

	token, _ := GenerateRefreshToken()
	userID := "user-exp"

	if err := StoreRefreshToken(ctx, rdb, token, userID); err != nil {
		t.Fatalf("StoreRefreshToken: %v", err)
	}

	// Fast-forward miniredis clock past the refresh token TTL (30 days + 1 second).
	mr.FastForward(refreshTokenTTL + time.Second)

	_, err := ValidateRefreshToken(ctx, rdb, token)
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
}
