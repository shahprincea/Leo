package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/shahprincea/leo/backend/internal/auth"
)

// ─── Fakes ────────────────────────────────────────────────────────────────────

type fakeWatchRepository struct {
	mu      sync.Mutex
	watches map[string]*Watch // keyed by device_id
}

func newFakeWatchRepository() *fakeWatchRepository {
	return &fakeWatchRepository{watches: make(map[string]*Watch)}
}

func (f *fakeWatchRepository) RegisterWatch(_ context.Context, wearerID, deviceID, model, osVersion, carrier string) (*Watch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if existing, ok := f.watches[deviceID]; ok {
		existing.WearerID = wearerID
		existing.Model = model
		existing.OSVersion = osVersion
		existing.Carrier = carrier
		now := time.Now()
		existing.LastSeenAt = &now
		return existing, nil
	}
	now := time.Now()
	w := &Watch{
		ID:           "watch-" + deviceID,
		WearerID:     wearerID,
		DeviceID:     deviceID,
		Model:        model,
		OSVersion:    osVersion,
		Carrier:      carrier,
		IsSamsung:    isSamsungModel(model),
		LastSeenAt:   &now,
		RegisteredAt: now,
	}
	f.watches[deviceID] = w
	return w, nil
}

func (f *fakeWatchRepository) FindWatchByWearerID(_ context.Context, wearerID string) (*Watch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, w := range f.watches {
		if w.WearerID == wearerID {
			return w, nil
		}
	}
	return nil, nil
}

func (f *fakeWatchRepository) UpdateConfigHash(_ context.Context, watchID, hash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, w := range f.watches {
		if w.ID == watchID {
			w.ConfigHash = &hash
			return nil
		}
	}
	return nil
}

func (f *fakeWatchRepository) GetWatchConfig(_ context.Context, _ string) (*WatchConfig, error) {
	cfg := &WatchConfig{
		Geofences:          []GeofenceConfig{},
		Medications:        []MedicationConfig{},
		EscalationContacts: []ContactConfig{},
		PresetMessages:     []PresetMessageConfig{},
		EmergencyMedicalID: EmergencyMedicalID{
			FullName:          "Test Wearer",
			MedicalConditions: []string{},
			Allergies:         []string{},
		},
		WellnessPromptTime: "08:00",
		NightMode:          NightModeConfig{Start: "22:00", End: "07:00", Timezone: "UTC"},
	}
	cfg.ConfigHash = computeConfigHash(cfg)
	return cfg, nil
}

// fakeBroadcaster records all Broadcast calls for assertion.
type fakeBroadcaster struct {
	mu     sync.Mutex
	events []broadcastCall
}

type broadcastCall struct {
	WearerID string
	Event    any
}

func (f *fakeBroadcaster) Broadcast(wearerID string, event any) {
	f.mu.Lock()
	f.events = append(f.events, broadcastCall{WearerID: wearerID, Event: event})
	f.mu.Unlock()
}

func (f *fakeBroadcaster) calls() []broadcastCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]broadcastCall, len(f.events))
	copy(out, f.events)
	return out
}

// newWatchTestHandler builds a WatchHandler with fakes.
func newWatchTestHandler(repo *fakeWatchRepository, hub *fakeBroadcaster) *WatchHandler {
	return &WatchHandler{db: repo, hub: hub}
}

// ─── Tests ────────────────────────────────────────────────────────────────────

func TestWatchRegister_Success(t *testing.T) {
	repo := newFakeWatchRepository()
	hub := &fakeBroadcaster{}
	h := newWatchTestHandler(repo, hub)

	const wearerID = "wearer-123"
	body := map[string]string{
		"device_id":  "hw-abc123",
		"model":      "samsung_galaxy_watch6_lte",
		"os_version": "4.0",
		"carrier":    "T-Mobile",
	}
	reqBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/watches/register", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	// Inject wearerID into context as device auth would.
	req = withDeviceToken(req, wearerID)

	rr := httptest.NewRecorder()
	h.Register(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]watchResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	watch := resp["watch"]
	if watch.WearerID != wearerID {
		t.Errorf("expected wearer_id %q, got %q", wearerID, watch.WearerID)
	}
	if watch.DeviceID != "hw-abc123" {
		t.Errorf("expected device_id hw-abc123, got %q", watch.DeviceID)
	}
	if !watch.IsSamsung {
		t.Error("expected is_samsung true for samsung model")
	}

	// Verify WS event was broadcast.
	calls := hub.calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 broadcast call, got %d", len(calls))
	}
	if calls[0].WearerID != wearerID {
		t.Errorf("broadcast to wrong wearer: got %q", calls[0].WearerID)
	}
}

func TestWatchRegister_MissingDeviceID(t *testing.T) {
	repo := newFakeWatchRepository()
	hub := &fakeBroadcaster{}
	h := newWatchTestHandler(repo, hub)

	body := map[string]string{"model": "pixel_watch_2"}
	reqBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/watches/register", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req = withDeviceToken(req, "wearer-456")

	rr := httptest.NewRecorder()
	h.Register(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestWatchRegister_MissingAuth(t *testing.T) {
	repo := newFakeWatchRepository()
	hub := &fakeBroadcaster{}
	h := newWatchTestHandler(repo, hub)

	body := map[string]string{"device_id": "hw-xyz", "model": "pixel"}
	reqBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/watches/register", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	// No device auth in context.

	rr := httptest.NewRecorder()
	h.Register(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestWatchRegister_Idempotent(t *testing.T) {
	repo := newFakeWatchRepository()
	hub := &fakeBroadcaster{}
	h := newWatchTestHandler(repo, hub)

	const wearerID = "wearer-789"
	body := map[string]string{
		"device_id":  "hw-dup",
		"model":      "pixel_watch_2",
		"os_version": "3.0",
	}
	reqBody, _ := json.Marshal(body)

	for i := range 2 {
		req := httptest.NewRequest(http.MethodPost, "/watches/register", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		req = withDeviceToken(req, wearerID)
		rr := httptest.NewRecorder()
		h.Register(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("call %d: expected 201, got %d", i+1, rr.Code)
		}
		reqBody, _ = json.Marshal(body) // re-marshal for next iteration
	}

	// Only one watch should exist in the fake repo.
	if len(repo.watches) != 1 {
		t.Errorf("expected 1 watch, got %d", len(repo.watches))
	}
}

func TestWatchConfig_ReturnsFullPayload(t *testing.T) {
	repo := newFakeWatchRepository()
	hub := &fakeBroadcaster{}
	h := newWatchTestHandler(repo, hub)

	req := httptest.NewRequest(http.MethodGet, "/watches/config", nil)
	req = withDeviceToken(req, "wearer-cfg-1")

	rr := httptest.NewRecorder()
	h.Config(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var cfg WatchConfig
	if err := json.NewDecoder(rr.Body).Decode(&cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if cfg.ConfigHash == "" {
		t.Error("expected non-empty config_hash")
	}
	if cfg.NightMode.Start != "22:00" {
		t.Errorf("expected night mode start 22:00, got %q", cfg.NightMode.Start)
	}
}

func TestWatchConfig_304WhenHashUnchanged(t *testing.T) {
	repo := newFakeWatchRepository()
	hub := &fakeBroadcaster{}
	h := newWatchTestHandler(repo, hub)

	// First call to get the current hash.
	req := httptest.NewRequest(http.MethodGet, "/watches/config", nil)
	req = withDeviceToken(req, "wearer-304")
	rr := httptest.NewRecorder()
	h.Config(rr, req)

	var cfg WatchConfig
	_ = json.NewDecoder(rr.Body).Decode(&cfg)
	currentHash := cfg.ConfigHash

	// Second call with the same hash — must return 304.
	req2 := httptest.NewRequest(http.MethodGet, "/watches/config", nil)
	req2.Header.Set("If-None-Match", currentHash)
	req2 = withDeviceToken(req2, "wearer-304")
	rr2 := httptest.NewRecorder()
	h.Config(rr2, req2)

	if rr2.Code != http.StatusNotModified {
		t.Errorf("expected 304, got %d", rr2.Code)
	}
}

func TestWatchConfig_MissingAuth(t *testing.T) {
	repo := newFakeWatchRepository()
	hub := &fakeBroadcaster{}
	h := newWatchTestHandler(repo, hub)

	req := httptest.NewRequest(http.MethodGet, "/watches/config", nil)
	// No device auth context.

	rr := httptest.NewRecorder()
	h.Config(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestIsSamsungModel(t *testing.T) {
	cases := []struct {
		model    string
		expected bool
	}{
		{"samsung_galaxy_watch6_lte", true},
		{"samsung_galaxy_watch5_pro", true},
		{"pixel_watch_2", false},
		{"", false},
	}
	for _, tc := range cases {
		got := isSamsungModel(tc.model)
		if got != tc.expected {
			t.Errorf("isSamsungModel(%q) = %v, want %v", tc.model, got, tc.expected)
		}
	}
}

// ─── Context helpers ──────────────────────────────────────────────────────────

// withDeviceToken stores token→wearerID in miniredis, then runs RequireDeviceAuth
// middleware inline to inject the wearerID into the request context — same
// approach used by withUserID in wearers_test.go.
func withDeviceToken(r *http.Request, wearerID string) *http.Request {
	mr, err := miniredis.Run()
	if err != nil {
		panic("miniredis: " + err.Error())
	}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	token := "test-device-token-" + wearerID
	if err := auth.StoreDeviceToken(context.Background(), rdb, token, wearerID); err != nil {
		panic("StoreDeviceToken: " + err.Error())
	}

	mw := auth.RequireDeviceAuth(rdb)
	var captured context.Context
	inner := http.HandlerFunc(func(_ http.ResponseWriter, r2 *http.Request) {
		captured = r2.Context()
	})
	rec := httptest.NewRecorder()
	r.Header.Set("Authorization", "Bearer "+token)
	mw(inner).ServeHTTP(rec, r)
	if captured == nil {
		panic("withDeviceToken: middleware did not call inner handler")
	}
	return r.WithContext(captured)
}
