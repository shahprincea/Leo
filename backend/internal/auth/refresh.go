package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

const refreshTokenTTL = 30 * 24 * time.Hour
const refreshTokenPrefix = "refresh:"

// GenerateRefreshToken creates a cryptographically random 32-byte hex token.
func GenerateRefreshToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// StoreRefreshToken persists token → userID in Redis with a 30-day TTL.
func StoreRefreshToken(ctx context.Context, rdb *redis.Client, token, userID string) error {
	return rdb.Set(ctx, refreshTokenPrefix+token, userID, refreshTokenTTL).Err()
}

// ValidateRefreshToken looks up the token in Redis and returns the associated userID.
// Returns an error if the token is not found or has expired.
func ValidateRefreshToken(ctx context.Context, rdb *redis.Client, token string) (userID string, err error) {
	val, err := rdb.Get(ctx, refreshTokenPrefix+token).Result()
	if errors.Is(err, redis.Nil) {
		return "", errors.New("auth: refresh token not found or expired")
	}
	if err != nil {
		return "", err
	}
	return val, nil
}

// RevokeRefreshToken deletes the token from Redis, invalidating the session.
func RevokeRefreshToken(ctx context.Context, rdb *redis.Client, token string) error {
	return rdb.Del(ctx, refreshTokenPrefix+token).Err()
}
