package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// Device token is separate from user JWT — watch uses device_token, not JWT.
// Stored in Redis as: "device:<token>" → wearerID (before watch registration)
// After watch registration (slice 4): "device:<token>" → watch_id
//
// Flow: POST /auth/device { wearer_id, pin } → validate pin → return device_token
// The device_token is then used with POST /watches/register to link to a real watch.

const deviceTokenTTL = 30 * 24 * time.Hour
const deviceTokenPrefix = "device:"

// GenerateDeviceToken creates a cryptographically random 32-byte hex token.
func GenerateDeviceToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// StoreDeviceToken persists token → watchID (or wearerID before registration) in Redis with a 30-day TTL.
func StoreDeviceToken(ctx context.Context, rdb *redis.Client, token, watchID string) error {
	return rdb.Set(ctx, deviceTokenPrefix+token, watchID, deviceTokenTTL).Err()
}

// ValidateDeviceToken looks up the token in Redis and returns the associated watchID (or wearerID).
// Returns an error if the token is not found or has expired.
func ValidateDeviceToken(ctx context.Context, rdb *redis.Client, token string) (watchID string, err error) {
	val, err := rdb.Get(ctx, deviceTokenPrefix+token).Result()
	if errors.Is(err, redis.Nil) {
		return "", errors.New("auth: device token not found or expired")
	}
	if err != nil {
		return "", err
	}
	return val, nil
}

// RevokeDeviceToken deletes the device token from Redis, invalidating the watch session.
func RevokeDeviceToken(ctx context.Context, rdb *redis.Client, token string) error {
	return rdb.Del(ctx, deviceTokenPrefix+token).Err()
}
