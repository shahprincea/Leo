package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// ─── Fakes ────────────────────────────────────────────────────────────────────

type fakeFallRepository struct {
	mu      sync.Mutex
	events  map[string]*FallEvent
	counter int
}

func newFakeFallRepository() *fakeFallRepository {
	return &fakeFallRepository{events: make(map[string]*FallEvent)}
}

func (f *fakeFallRepository) CreateFallEvent(_ context.Context, wearerID, fallType string) (*FallEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counter++
	id := fmt.Sprintf("fall-%d", f.counter)
	ev := &FallEvent{
		ID:         id,
		WearerID:   wearerID,
		FallType:   fallType,
		Status:     "detected",
		DetectedAt: time.Now(),
	}
	f.events[id] = ev
	return ev, nil
}

func (f *fakeFallRepository) GetActiveFallEvent(_ context.Context, fallID string) (*FallEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ev, ok := f.events[fallID]; ok && ev.Status == "detected" {
		return ev, nil
	}
	return nil, nil
}

func (f *fakeFallRepository) CancelFallEvent(_ context.Context, fallID, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ev, ok := f.events[fallID]; ok {
		now := time.Now()
		ev.Status = "false_alarm"
		ev.ResolvedAt = &now
	}
	return nil
}

func (f *fakeFallRepository) ConfirmFallEvent(_ context.Context, fallID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ev, ok := f.events[fallID]; ok {
		now := time.Now()
		ev.Status = "confirmed"
		ev.ResolvedAt = &now
	}
	return nil
}

func newFallTestHandler(repo *fakeFallRepository) *FallHandler {
	return &FallHandler{db: repo}
}

// ─── Tests ────────────────────────────────────────────────────────────────────

// Behavior 1: POST /falls creates a fall_event and returns fall_id + confirmation window.
func TestFallReport_CreatesEvent(t *testing.T) {
	repo := newFakeFallRepository()
	h := newFallTestHandler(repo)

	const wearerID = "wearer-fall-1"
	body := map[string]string{"fall_type": "hard"}
	reqBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/falls", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req = withDeviceToken(req, wearerID)
	rr := httptest.NewRecorder()
	h.Report(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["fall_id"] == nil || resp["fall_id"] == "" {
		t.Error("expected fall_id in response")
	}
	windowSec, ok := resp["confirmation_window_sec"].(float64)
	if !ok || windowSec <= 0 {
		t.Errorf("expected positive confirmation_window_sec, got %v", resp["confirmation_window_sec"])
	}
}

// Behavior 1b: soft fall type is accepted.
func TestFallReport_SoftFall(t *testing.T) {
	repo := newFakeFallRepository()
	h := newFallTestHandler(repo)

	body := map[string]string{"fall_type": "soft"}
	reqBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/falls", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req = withDeviceToken(req, "wearer-soft")
	rr := httptest.NewRecorder()
	h.Report(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d", rr.Code)
	}
	if repo.events["fall-1"].FallType != "soft" {
		t.Errorf("expected fall_type soft, got %q", repo.events["fall-1"].FallType)
	}
}

// Behavior 2: invalid fall_type → 400.
func TestFallReport_InvalidType(t *testing.T) {
	repo := newFakeFallRepository()
	h := newFallTestHandler(repo)

	body := map[string]string{"fall_type": "giant"}
	reqBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/falls", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req = withDeviceToken(req, "wearer-x")
	rr := httptest.NewRecorder()
	h.Report(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

// POST /falls with no auth → 401.
func TestFallReport_MissingAuth(t *testing.T) {
	repo := newFakeFallRepository()
	h := newFallTestHandler(repo)

	body := map[string]string{"fall_type": "hard"}
	reqBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/falls", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.Report(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

// Behavior 3: POST /falls/:id/cancel marks event as false_alarm.
func TestFallCancel_Success(t *testing.T) {
	repo := newFakeFallRepository()
	h := newFallTestHandler(repo)

	const wearerID = "wearer-cancel-fall"
	// Seed an active fall event directly.
	repo.events["fall-99"] = &FallEvent{
		ID: "fall-99", WearerID: wearerID, FallType: "hard",
		Status: "detected", DetectedAt: time.Now(),
	}

	req := httptest.NewRequest(http.MethodPost, "/falls/fall-99/cancel", nil)
	req = withDeviceToken(req, wearerID)
	req = injectURLParam(req, "id", "fall-99")
	rr := httptest.NewRecorder()
	h.Cancel(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if repo.events["fall-99"].Status != "false_alarm" {
		t.Errorf("expected status false_alarm, got %q", repo.events["fall-99"].Status)
	}
}

// POST /falls/:id/cancel with no auth → 401.
func TestFallCancel_MissingAuth(t *testing.T) {
	repo := newFakeFallRepository()
	h := newFallTestHandler(repo)

	req := httptest.NewRequest(http.MethodPost, "/falls/fall-1/cancel", nil)
	req = injectURLParam(req, "id", "fall-1")
	rr := httptest.NewRecorder()
	h.Cancel(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

// POST /falls/:id/cancel for non-existent or wrong-wearer → 404.
func TestFallCancel_NotFound(t *testing.T) {
	repo := newFakeFallRepository()
	h := newFallTestHandler(repo)

	req := httptest.NewRequest(http.MethodPost, "/falls/no-such/cancel", nil)
	req = withDeviceToken(req, "wearer-x")
	req = injectURLParam(req, "id", "no-such")
	rr := httptest.NewRecorder()
	h.Cancel(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

// Behavior 4: POST /sos accepts triggered_by=fall and fall_event_id (regression).
func TestSOS_AcceptsTriggeredByFall(t *testing.T) {
	repo := newFakeSOSRepository()
	caller := &fakeCaller{}
	h := newSOSTestHandler(repo, caller, nil)

	const wearerID = "wearer-sos-fall"
	repo.contacts[wearerID] = []ContactConfig{
		{FullName: "Alice", Phone: "+15550001111", Tier: 1, TimeoutSec: 60},
	}

	body := map[string]string{"triggered_by": "fall", "fall_event_id": "fall-99"}
	reqBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/sos", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req = withDeviceToken(req, wearerID)
	rr := httptest.NewRecorder()
	h.Trigger(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rr.Code, rr.Body.String())
	}
	// Verify the event stored triggered_by correctly.
	for _, ev := range repo.events {
		if ev.TriggeredBy != "fall" {
			t.Errorf("expected triggered_by fall, got %q", ev.TriggeredBy)
		}
	}
}
