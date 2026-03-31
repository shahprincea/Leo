package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shahprincea/leo/backend/internal/auth"
	"github.com/shahprincea/leo/backend/internal/config"
)

// ---- fakeRepo ----

type fakeRepo struct {
	mu      sync.Mutex
	users   map[string]*User // keyed by email
	wearers map[string]*Wearer
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		users:   make(map[string]*User),
		wearers: make(map[string]*Wearer),
	}
}

func (f *fakeRepo) CreateUser(_ context.Context, email, passwordHash, fullName string, phone *string) (*User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.users[email]; exists {
		// Simulate a unique-constraint violation signal by returning a sentinel error.
		return nil, errors.New("duplicate email")
	}
	ph := ""
	if phone != nil {
		ph = *phone
	}
	u := &User{
		ID:           "id-" + email,
		Email:        email,
		FullName:     fullName,
		Phone:        ph,
		PasswordHash: passwordHash,
		CreatedAt:    time.Now(),
	}
	f.users[email] = u
	return u, nil
}

func (f *fakeRepo) FindUserByEmail(_ context.Context, email string) (*User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[email]
	if !ok {
		return nil, pgx.ErrNoRows
	}
	return u, nil
}

func (f *fakeRepo) FindUserByID(_ context.Context, id string) (*User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, u := range f.users {
		if u.ID == id {
			return u, nil
		}
	}
	return nil, pgx.ErrNoRows
}

func (f *fakeRepo) FindWearerByID(_ context.Context, id string) (*Wearer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w, ok := f.wearers[id]
	if !ok {
		return nil, pgx.ErrNoRows
	}
	return w, nil
}

// ---- fakeTokenStore ----

type fakeTokenStore struct {
	mu       sync.Mutex
	refresh  map[string]string // token → userID
	device   map[string]string // token → wearerID
	failNext bool              // if true, next mutating call returns error
}

func newFakeTokenStore() *fakeTokenStore {
	return &fakeTokenStore{
		refresh: make(map[string]string),
		device:  make(map[string]string),
	}
}

func (s *fakeTokenStore) StoreRefreshToken(_ context.Context, token, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh[token] = userID
	return nil
}

func (s *fakeTokenStore) ValidateRefreshToken(_ context.Context, token string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	uid, ok := s.refresh[token]
	if !ok {
		return "", errors.New("auth: refresh token not found or expired")
	}
	return uid, nil
}

func (s *fakeTokenStore) RevokeRefreshToken(_ context.Context, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.refresh, token)
	return nil
}

func (s *fakeTokenStore) StoreDeviceToken(_ context.Context, token, wearerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.device[token] = wearerID
	return nil
}

// ---- helpers ----

const testJWTSecret = "test-handler-secret"

func testHandler(repo UserRepository, tokens RedisTokenStore) *AuthHandler {
	return newAuthHandlerWith(repo, tokens, &config.Config{JWTSecret: testJWTSecret})
}

func postJSON(handler http.HandlerFunc, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler(rr, req)
	return rr
}

func decodeBody(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&m); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	return m
}

// seedUser adds a user with a real bcrypt hash to the repo.
func seedUser(t *testing.T, repo *fakeRepo, email, password, fullName string) *User {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	u, err := repo.CreateUser(context.Background(), email, hash, fullName, nil)
	if err != nil {
		t.Fatalf("seedUser: %v", err)
	}
	return u
}

// ---- Register tests ----

func TestRegister_Success(t *testing.T) {
	repo := newFakeRepo()
	tokens := newFakeTokenStore()
	h := testHandler(repo, tokens)

	rr := postJSON(h.Register, map[string]string{
		"email":     "alice@example.com",
		"password":  "password123",
		"full_name": "Alice",
	})

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d — body: %s", rr.Code, rr.Body)
	}
	m := decodeBody(t, rr)
	if m["access_token"] == "" || m["access_token"] == nil {
		t.Error("missing access_token")
	}
	if m["refresh_token"] == "" || m["refresh_token"] == nil {
		t.Error("missing refresh_token")
	}
}

func TestRegister_DuplicateEmail(t *testing.T) {
	repo := newFakeRepo()
	tokens := newFakeTokenStore()
	h := testHandler(repo, tokens)

	body := map[string]string{
		"email":     "dup@example.com",
		"password":  "password123",
		"full_name": "Dup",
	}
	// First registration — success, pre-seed the repo.
	_ = seedUser(t, repo, "dup@example.com", "password123", "Dup")

	// Second registration — should be conflict.
	rr := postJSON(h.Register, body)
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d — body: %s", rr.Code, rr.Body)
	}
}

func TestRegister_InvalidBody(t *testing.T) {
	repo := newFakeRepo()
	tokens := newFakeTokenStore()
	h := testHandler(repo, tokens)

	// Missing email.
	rr := postJSON(h.Register, map[string]string{
		"password":  "password123",
		"full_name": "No Email",
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// ---- Login tests ----

func TestLogin_Success(t *testing.T) {
	repo := newFakeRepo()
	tokens := newFakeTokenStore()
	h := testHandler(repo, tokens)

	seedUser(t, repo, "bob@example.com", "mypassword", "Bob")

	rr := postJSON(h.Login, map[string]string{
		"email":    "bob@example.com",
		"password": "mypassword",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", rr.Code, rr.Body)
	}
	m := decodeBody(t, rr)
	if m["access_token"] == nil {
		t.Error("missing access_token")
	}
	if m["refresh_token"] == nil {
		t.Error("missing refresh_token")
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	repo := newFakeRepo()
	tokens := newFakeTokenStore()
	h := testHandler(repo, tokens)

	seedUser(t, repo, "carol@example.com", "correct-pass", "Carol")

	rr := postJSON(h.Login, map[string]string{
		"email":    "carol@example.com",
		"password": "wrong-pass",
	})
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestLogin_UserNotFound(t *testing.T) {
	repo := newFakeRepo()
	tokens := newFakeTokenStore()
	h := testHandler(repo, tokens)

	rr := postJSON(h.Login, map[string]string{
		"email":    "nobody@example.com",
		"password": "whatever",
	})
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
	m := decodeBody(t, rr)
	if m["error"] != "invalid credentials" {
		t.Errorf("expected 'invalid credentials', got %v", m["error"])
	}
}

// ---- Refresh tests ----

func TestRefresh_Success(t *testing.T) {
	repo := newFakeRepo()
	tokens := newFakeTokenStore()
	h := testHandler(repo, tokens)

	u := seedUser(t, repo, "dave@example.com", "pass1234", "Dave")

	// Pre-seed a refresh token.
	rt, _ := auth.GenerateRefreshToken()
	_ = tokens.StoreRefreshToken(context.Background(), rt, u.ID)

	rr := postJSON(h.Refresh, map[string]string{"refresh_token": rt})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", rr.Code, rr.Body)
	}
	m := decodeBody(t, rr)
	if m["access_token"] == nil {
		t.Error("missing access_token")
	}
	if m["refresh_token"] == nil {
		t.Error("missing refresh_token")
	}
	// Old token must be revoked.
	if _, ok := tokens.refresh[rt]; ok {
		t.Error("old refresh token was not revoked")
	}
}

func TestRefresh_InvalidToken(t *testing.T) {
	repo := newFakeRepo()
	tokens := newFakeTokenStore()
	h := testHandler(repo, tokens)

	rr := postJSON(h.Refresh, map[string]string{"refresh_token": "not-a-real-token"})
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

// ---- DeviceAuth tests ----

func TestDeviceAuth_Success(t *testing.T) {
	repo := newFakeRepo()
	tokens := newFakeTokenStore()
	h := testHandler(repo, tokens)

	// Seed a wearer with a known PIN.
	pinHash, _ := auth.HashPassword("1234")
	repo.wearers["wearer-1"] = &Wearer{ID: "wearer-1", PinHash: pinHash}

	rr := postJSON(h.DeviceAuth, map[string]string{
		"wearer_id": "wearer-1",
		"pin":       "1234",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", rr.Code, rr.Body)
	}
	m := decodeBody(t, rr)
	if m["device_token"] == nil || m["device_token"] == "" {
		t.Error("missing device_token")
	}
}

func TestDeviceAuth_WrongPin(t *testing.T) {
	repo := newFakeRepo()
	tokens := newFakeTokenStore()
	h := testHandler(repo, tokens)

	pinHash, _ := auth.HashPassword("1234")
	repo.wearers["wearer-2"] = &Wearer{ID: "wearer-2", PinHash: pinHash}

	rr := postJSON(h.DeviceAuth, map[string]string{
		"wearer_id": "wearer-2",
		"pin":       "9999",
	})
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

// ---- RequireAuth middleware tests ----

// dummyProtected is a trivial handler used to verify that the middleware
// passes through when the token is valid.
var dummyProtected = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
})

func TestRequireAuth_MissingHeader(t *testing.T) {
	mw := auth.RequireAuth(testJWTSecret)
	handler := mw(dummyProtected)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestRequireAuth_InvalidToken(t *testing.T) {
	mw := auth.RequireAuth(testJWTSecret)
	handler := mw(dummyProtected)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer not.a.valid.token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestRequireAuth_ValidToken(t *testing.T) {
	token, err := auth.SignAccessToken("u1", "u@example.com", testJWTSecret)
	if err != nil {
		t.Fatalf("SignAccessToken: %v", err)
	}

	mw := auth.RequireAuth(testJWTSecret)
	handler := mw(dummyProtected)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

