package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/redis/go-redis/v9"
)

type contextKey string

const claimsContextKey contextKey = "auth_claims"

// RequireAuth returns a chi-compatible middleware that validates Bearer JWTs.
// On success the verified Claims are stored in the request context.
// On failure it responds with 401 and a JSON error body.
func RequireAuth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				writeUnauthorized(w)
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				writeUnauthorized(w)
				return
			}

			claims, err := VerifyAccessToken(parts[1], secret)
			if err != nil {
				writeUnauthorized(w)
				return
			}

			ctx := context.WithValue(r.Context(), claimsContextKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserIDFromContext extracts the authenticated user's ID from a request context.
// Returns ("", false) if no authenticated claims are present.
func UserIDFromContext(ctx context.Context) (string, bool) {
	claims, ok := ctx.Value(claimsContextKey).(*Claims)
	if !ok || claims == nil {
		return "", false
	}
	return claims.UserID, true
}

const watchIDContextKey contextKey = "watch_id"

// RequireDeviceAuth validates a device_token from the Authorization: Bearer header.
// On success the associated watchID is stored in the request context.
// On failure it responds with 401 and a JSON error body.
func RequireDeviceAuth(rdb *redis.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				writeUnauthorized(w)
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				writeUnauthorized(w)
				return
			}

			watchID, err := ValidateDeviceToken(r.Context(), rdb, parts[1])
			if err != nil {
				writeUnauthorized(w)
				return
			}

			ctx := context.WithValue(r.Context(), watchIDContextKey, watchID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// WatchIDFromContext extracts the watch ID (or wearer ID before registration) stored by RequireDeviceAuth.
// Returns ("", false) if no device auth is present.
func WatchIDFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(watchIDContextKey).(string)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
}
