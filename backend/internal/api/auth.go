package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/shahprincea/leo/backend/internal/auth"
	"github.com/shahprincea/leo/backend/internal/config"
)

// User is the domain model returned by UserRepository.
type User struct {
	ID           string
	Email        string
	FullName     string
	Phone        string
	PasswordHash string
	CreatedAt    time.Time
}

// Wearer holds the minimal wearer data needed for device auth.
type Wearer struct {
	ID      string
	PinHash string
}

// UserRepository abstracts DB access for auth handlers.
// This interface enables injection of test doubles without a real database.
type UserRepository interface {
	CreateUser(ctx context.Context, email, passwordHash, fullName string, phone *string) (*User, error)
	FindUserByEmail(ctx context.Context, email string) (*User, error)
	FindUserByID(ctx context.Context, id string) (*User, error)
	FindWearerByID(ctx context.Context, id string) (*Wearer, error)
}

// RedisTokenStore abstracts Redis operations for auth handlers.
type RedisTokenStore interface {
	StoreRefreshToken(ctx context.Context, token, userID string) error
	ValidateRefreshToken(ctx context.Context, token string) (userID string, err error)
	RevokeRefreshToken(ctx context.Context, token string) error
	StoreDeviceToken(ctx context.Context, token, wearerID string) error
}

// PostgresUserRepository implements UserRepository using pgxpool.
type PostgresUserRepository struct {
	db *pgxpool.Pool
}

// NewPostgresUserRepository creates a PostgresUserRepository.
func NewPostgresUserRepository(db *pgxpool.Pool) *PostgresUserRepository {
	return &PostgresUserRepository{db: db}
}

func (r *PostgresUserRepository) CreateUser(ctx context.Context, email, passwordHash, fullName string, phone *string) (*User, error) {
	u := &User{}
	err := r.db.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, full_name, phone)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, email, full_name, COALESCE(phone, ''), created_at`,
		email, passwordHash, fullName, phone,
	).Scan(&u.ID, &u.Email, &u.FullName, &u.Phone, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (r *PostgresUserRepository) FindUserByEmail(ctx context.Context, email string) (*User, error) {
	u := &User{}
	err := r.db.QueryRow(ctx,
		`SELECT id, email, full_name, COALESCE(phone, ''), created_at, password_hash
		 FROM users
		 WHERE email = $1 AND deleted_at IS NULL`,
		email,
	).Scan(&u.ID, &u.Email, &u.FullName, &u.Phone, &u.CreatedAt, &u.PasswordHash)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (r *PostgresUserRepository) FindUserByID(ctx context.Context, id string) (*User, error) {
	u := &User{}
	err := r.db.QueryRow(ctx,
		`SELECT id, email, full_name, COALESCE(phone, ''), created_at
		 FROM users
		 WHERE id = $1 AND deleted_at IS NULL`,
		id,
	).Scan(&u.ID, &u.Email, &u.FullName, &u.Phone, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (r *PostgresUserRepository) FindWearerByID(ctx context.Context, id string) (*Wearer, error) {
	w := &Wearer{}
	err := r.db.QueryRow(ctx,
		`SELECT id, pin_hash FROM wearers WHERE id = $1 AND deleted_at IS NULL`,
		id,
	).Scan(&w.ID, &w.PinHash)
	if err != nil {
		return nil, err
	}
	return w, nil
}

// redisTokenStore wraps a *redis.Client to satisfy RedisTokenStore.
type redisTokenStore struct {
	rdb *redis.Client
}

func newRedisTokenStore(rdb *redis.Client) RedisTokenStore {
	return &redisTokenStore{rdb: rdb}
}

func (s *redisTokenStore) StoreRefreshToken(ctx context.Context, token, userID string) error {
	return auth.StoreRefreshToken(ctx, s.rdb, token, userID)
}

func (s *redisTokenStore) ValidateRefreshToken(ctx context.Context, token string) (string, error) {
	return auth.ValidateRefreshToken(ctx, s.rdb, token)
}

func (s *redisTokenStore) RevokeRefreshToken(ctx context.Context, token string) error {
	return auth.RevokeRefreshToken(ctx, s.rdb, token)
}

func (s *redisTokenStore) StoreDeviceToken(ctx context.Context, token, wearerID string) error {
	return auth.StoreDeviceToken(ctx, s.rdb, token, wearerID)
}

// AuthHandler handles authentication endpoints.
type AuthHandler struct {
	repo   UserRepository
	tokens RedisTokenStore
	config *config.Config
}

// NewAuthHandler creates a new AuthHandler backed by real Postgres and Redis.
func NewAuthHandler(db *pgxpool.Pool, rdb *redis.Client, cfg *config.Config) *AuthHandler {
	return &AuthHandler{
		repo:   NewPostgresUserRepository(db),
		tokens: newRedisTokenStore(rdb),
		config: cfg,
	}
}

// newAuthHandlerWith creates an AuthHandler with injected dependencies (used in tests).
func newAuthHandlerWith(repo UserRepository, tokens RedisTokenStore, cfg *config.Config) *AuthHandler {
	return &AuthHandler{repo: repo, tokens: tokens, config: cfg}
}

// userResponse is the public representation of a user (no password_hash).
type userResponse struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	FullName  string    `json:"full_name"`
	Phone     string    `json:"phone,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// authResponse is returned on successful register/login.
type authResponse struct {
	User         userResponse `json:"user"`
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token"`
}

// refreshResponse is returned on successful token refresh.
type refreshResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// Register handles POST /auth/register.
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		FullName string `json:"full_name"`
		Phone    string `json:"phone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validation
	if req.Email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}
	if len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	if req.FullName == "" {
		writeError(w, http.StatusBadRequest, "full_name is required")
		return
	}

	ctx := r.Context()

	// Check for duplicate email
	existing, err := h.repo.FindUserByEmail(ctx, req.Email)
	if err != nil && err != pgx.ErrNoRows {
		// FindUserByEmail returns pgx.ErrNoRows when not found; any other error is unexpected.
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if existing != nil {
		writeError(w, http.StatusConflict, "email already registered")
		return
	}

	// Hash password
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Insert user
	u, err := h.repo.CreateUser(ctx, req.Email, hash, req.FullName, nullableString(req.Phone))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Generate tokens
	accessToken, err := auth.SignAccessToken(u.ID, u.Email, h.config.JWTSecret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	refreshToken, err := auth.GenerateRefreshToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if err := h.tokens.StoreRefreshToken(ctx, refreshToken, u.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusCreated, authResponse{
		User: userResponse{
			ID:        u.ID,
			Email:     u.Email,
			FullName:  u.FullName,
			Phone:     u.Phone,
			CreatedAt: u.CreatedAt,
		},
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	})
}

// Login handles POST /auth/login.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx := r.Context()

	// Fetch user by email (not deleted)
	u, err := h.repo.FindUserByEmail(ctx, req.Email)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Verify password (same error message to avoid leaking existence)
	if err := auth.VerifyPassword(u.PasswordHash, req.Password); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	// Generate tokens
	accessToken, err := auth.SignAccessToken(u.ID, u.Email, h.config.JWTSecret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	refreshToken, err := auth.GenerateRefreshToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if err := h.tokens.StoreRefreshToken(ctx, refreshToken, u.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, authResponse{
		User: userResponse{
			ID:        u.ID,
			Email:     u.Email,
			FullName:  u.FullName,
			Phone:     u.Phone,
			CreatedAt: u.CreatedAt,
		},
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	})
}

// Refresh handles POST /auth/refresh.
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx := r.Context()

	// Validate refresh token in Redis
	userID, err := h.tokens.ValidateRefreshToken(ctx, req.RefreshToken)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid or expired refresh token")
		return
	}

	// Fetch user from DB by ID
	u, err := h.repo.FindUserByID(ctx, userID)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusUnauthorized, "invalid or expired refresh token")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Revoke old token
	if err := h.tokens.RevokeRefreshToken(ctx, req.RefreshToken); err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Generate new tokens
	accessToken, err := auth.SignAccessToken(userID, u.Email, h.config.JWTSecret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	newRefreshToken, err := auth.GenerateRefreshToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if err := h.tokens.StoreRefreshToken(ctx, newRefreshToken, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, refreshResponse{
		AccessToken:  accessToken,
		RefreshToken: newRefreshToken,
	})
}

// DeviceAuth handles POST /auth/device.
// Validates a wearer's PIN and returns a device_token for watch use.
func (h *AuthHandler) DeviceAuth(w http.ResponseWriter, r *http.Request) {
	var req struct {
		WearerID string `json:"wearer_id"`
		PIN      string `json:"pin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate inputs
	if req.WearerID == "" {
		writeError(w, http.StatusBadRequest, "wearer_id is required")
		return
	}
	if len(req.PIN) != 4 {
		writeError(w, http.StatusBadRequest, "pin must be exactly 4 digits")
		return
	}
	for _, ch := range req.PIN {
		if ch < '0' || ch > '9' {
			writeError(w, http.StatusBadRequest, "pin must be exactly 4 digits")
			return
		}
	}

	ctx := r.Context()

	// Fetch wearer — treat not-found and wrong PIN identically to avoid leaking existence
	wearer, err := h.repo.FindWearerByID(ctx, req.WearerID)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Verify PIN
	if err := auth.VerifyPassword(wearer.PinHash, req.PIN); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	// Generate and store device token — keyed to wearer_id until watch registers
	deviceToken, err := auth.GenerateDeviceToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if err := h.tokens.StoreDeviceToken(ctx, deviceToken, req.WearerID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"device_token": deviceToken,
		"wearer_id":    req.WearerID,
	})
}

// nullableString converts an empty string to nil for nullable DB columns.
func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
