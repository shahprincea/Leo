package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
)

// ─── Fakes ────────────────────────────────────────────────────────────────────

type fakeCaller struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakeCaller) Call(_ context.Context, to string) error {
	f.mu.Lock()
	f.calls = append(f.calls, to)
	f.mu.Unlock()
	return nil
}

func (f *fakeCaller) getCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.calls))
	copy(out, f.calls)
	return out
}

type escLogEntry struct {
	SOSID string
	Phone string
	Tier  int
}

type fakeSOSRepository struct {
	mu       sync.Mutex
	contacts map[string][]ContactConfig // wearerID → contacts
	events   map[string]*SOSEvent       // id → event
	settings map[string]*SOSSettings    // wearerID → settings
	escLogs  []escLogEntry
	counter  int
}

func newFakeSOSRepository() *fakeSOSRepository {
	return &fakeSOSRepository{
		contacts: make(map[string][]ContactConfig),
		events:   make(map[string]*SOSEvent),
		settings: make(map[string]*SOSSettings),
	}
}

func (f *fakeSOSRepository) GetOnCallContact(_ context.Context, wearerID string) (*ContactConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	contacts := f.contacts[wearerID]
	if len(contacts) == 0 {
		return nil, nil
	}
	// Return the lowest-tier contact.
	best := contacts[0]
	for _, c := range contacts[1:] {
		if c.Tier < best.Tier {
			best = c
		}
	}
	return &best, nil
}

func (f *fakeSOSRepository) GetContactByTier(_ context.Context, wearerID string, tier int) (*ContactConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.contacts[wearerID] {
		if c.Tier == tier {
			return &c, nil
		}
	}
	return nil, nil
}

func (f *fakeSOSRepository) CreateSOSEvent(_ context.Context, wearerID, triggeredBy, fallEventID string) (*SOSEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if triggeredBy == "" {
		triggeredBy = "manual"
	}
	f.counter++
	id := fmt.Sprintf("sos-%d", f.counter)
	event := &SOSEvent{
		ID:          id,
		WearerID:    wearerID,
		Status:      "active",
		TriggeredBy: triggeredBy,
		FallEventID: fallEventID,
		TriggeredAt: time.Now(),
	}
	f.events[id] = event
	return event, nil
}

func (f *fakeSOSRepository) CancelSOSEvent(_ context.Context, sosID, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ev, ok := f.events[sosID]; ok {
		now := time.Now()
		ev.Status = "cancelled"
		ev.CancelledAt = &now
	}
	return nil
}

func (f *fakeSOSRepository) GetActiveSOSEvent(_ context.Context, sosID string) (*SOSEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ev, ok := f.events[sosID]; ok && ev.Status == "active" {
		return ev, nil
	}
	return nil, nil
}

func (f *fakeSOSRepository) LogEscalation(_ context.Context, sosID, phone string, tier int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.escLogs = append(f.escLogs, escLogEntry{SOSID: sosID, Phone: phone, Tier: tier})
	return nil
}

func (f *fakeSOSRepository) GetSOSSettings(_ context.Context, wearerID string) (*SOSSettings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.settings[wearerID]; ok {
		return s, nil
	}
	return &SOSSettings{WearerID: wearerID, Auto911: false}, nil
}

// newSOSTestHandler builds a SOSHandler with fakes and an optional Redis client.
func newSOSTestHandler(repo *fakeSOSRepository, caller *fakeCaller, rdb *redis.Client) *SOSHandler {
	return &SOSHandler{db: repo, caller: caller, rdb: rdb}
}

// newTestRedis creates a miniredis-backed Redis client for tests.
func newTestRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return rdb, mr
}

// ─── Handler tests ────────────────────────────────────────────────────────────

// Behavior 1 (tracer bullet): POST /sos stores event and returns the on-call contact.
func TestSOS_ReturnsOnCallContact(t *testing.T) {
	repo := newFakeSOSRepository()
	caller := &fakeCaller{}
	rdb, _ := newTestRedis(t)
	h := newSOSTestHandler(repo, caller, rdb)

	const wearerID = "wearer-sos-1"
	repo.contacts[wearerID] = []ContactConfig{
		{FullName: "Alice", Phone: "+15550001111", Tier: 1, TimeoutSec: 60},
	}

	req := httptest.NewRequest(http.MethodPost, "/sos", nil)
	req = withDeviceToken(req, wearerID)
	rr := httptest.NewRecorder()
	h.Trigger(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]json.RawMessage
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["sos_id"] == nil {
		t.Error("expected sos_id in response")
	}
	var contact ContactConfig
	if err := json.Unmarshal(resp["calling_contact"], &contact); err != nil {
		t.Fatalf("decode calling_contact: %v", err)
	}
	if contact.Phone != "+15550001111" {
		t.Errorf("expected phone +15550001111, got %q", contact.Phone)
	}
	if contact.FullName != "Alice" {
		t.Errorf("expected full_name Alice, got %q", contact.FullName)
	}
}

// Behavior 2: POST /sos with no contacts → 422.
func TestSOS_NoContacts_Returns422(t *testing.T) {
	repo := newFakeSOSRepository()
	caller := &fakeCaller{}
	h := newSOSTestHandler(repo, caller, nil)

	req := httptest.NewRequest(http.MethodPost, "/sos", nil)
	req = withDeviceToken(req, "wearer-no-contacts")
	rr := httptest.NewRecorder()
	h.Trigger(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", rr.Code)
	}
}

// Behavior 3: Caller.Call is invoked with the correct phone number.
func TestSOS_CallerInvokedWithOnCallPhone(t *testing.T) {
	repo := newFakeSOSRepository()
	caller := &fakeCaller{}
	h := newSOSTestHandler(repo, caller, nil)

	const wearerID = "wearer-caller-check"
	repo.contacts[wearerID] = []ContactConfig{
		{FullName: "Bob", Phone: "+15559998888", Tier: 1, TimeoutSec: 30},
	}

	req := httptest.NewRequest(http.MethodPost, "/sos", nil)
	req = withDeviceToken(req, wearerID)
	rr := httptest.NewRecorder()
	h.Trigger(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rr.Code)
	}
	calls := caller.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0] != "+15559998888" {
		t.Errorf("expected +15559998888, got %q", calls[0])
	}
}

// Behavior 3b: Lowest-tier contact is called when multiple tiers exist.
func TestSOS_CallsLowestTierFirst(t *testing.T) {
	repo := newFakeSOSRepository()
	caller := &fakeCaller{}
	h := newSOSTestHandler(repo, caller, nil)

	const wearerID = "wearer-multi-tier"
	repo.contacts[wearerID] = []ContactConfig{
		{FullName: "Carol", Phone: "+15553333333", Tier: 2, TimeoutSec: 60},
		{FullName: "Alice", Phone: "+15551111111", Tier: 1, TimeoutSec: 60},
	}

	req := httptest.NewRequest(http.MethodPost, "/sos", nil)
	req = withDeviceToken(req, wearerID)
	rr := httptest.NewRecorder()
	h.Trigger(rr, req)

	calls := caller.getCalls()
	if len(calls) != 1 || calls[0] != "+15551111111" {
		t.Errorf("expected tier-1 Alice's number, got %v", calls)
	}
}

// Behavior: POST /sos with no auth → 401.
func TestSOS_MissingAuth_Returns401(t *testing.T) {
	repo := newFakeSOSRepository()
	h := newSOSTestHandler(repo, &fakeCaller{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/sos", nil)
	rr := httptest.NewRecorder()
	h.Trigger(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

// Behavior 4: POST /sos/:id/cancel cancels the event.
func TestSOSCancel_Success(t *testing.T) {
	repo := newFakeSOSRepository()
	caller := &fakeCaller{}
	rdb, _ := newTestRedis(t)
	h := newSOSTestHandler(repo, caller, rdb)

	const wearerID = "wearer-cancel"
	repo.contacts[wearerID] = []ContactConfig{
		{FullName: "Dave", Phone: "+15554444444", Tier: 1, TimeoutSec: 60},
	}

	// Trigger SOS first.
	trigReq := httptest.NewRequest(http.MethodPost, "/sos", nil)
	trigReq = withDeviceToken(trigReq, wearerID)
	trigRR := httptest.NewRecorder()
	h.Trigger(trigRR, trigReq)
	if trigRR.Code != http.StatusAccepted {
		t.Fatalf("trigger: expected 202, got %d", trigRR.Code)
	}

	var trigResp map[string]json.RawMessage
	_ = json.NewDecoder(trigRR.Body).Decode(&trigResp)
	var sosID string
	_ = json.Unmarshal(trigResp["sos_id"], &sosID)

	// Now cancel it.
	cancelReq := httptest.NewRequest(http.MethodPost, "/sos/"+sosID+"/cancel", nil)
	cancelReq = withDeviceToken(cancelReq, wearerID)
	// Inject URL param manually (chi not active in unit test).
	cancelReq = injectURLParam(cancelReq, "id", sosID)
	cancelRR := httptest.NewRecorder()
	h.Cancel(cancelRR, cancelReq)

	if cancelRR.Code != http.StatusOK {
		t.Fatalf("cancel: expected 200, got %d: %s", cancelRR.Code, cancelRR.Body.String())
	}
	if repo.events[sosID].Status != "cancelled" {
		t.Errorf("expected event status cancelled, got %q", repo.events[sosID].Status)
	}
}

// POST /sos/:id/cancel with no auth → 401.
func TestSOSCancel_MissingAuth(t *testing.T) {
	repo := newFakeSOSRepository()
	h := newSOSTestHandler(repo, &fakeCaller{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/sos/sos-1/cancel", nil)
	req = injectURLParam(req, "id", "sos-1")
	rr := httptest.NewRecorder()
	h.Cancel(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

// POST /sos/:id/cancel for non-existent or wrong-wearer SOS → 404.
func TestSOSCancel_NotFound(t *testing.T) {
	repo := newFakeSOSRepository()
	h := newSOSTestHandler(repo, &fakeCaller{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/sos/no-such-sos/cancel", nil)
	req = withDeviceToken(req, "wearer-x")
	req = injectURLParam(req, "id", "no-such-sos")
	rr := httptest.NewRecorder()
	h.Cancel(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

// ─── Escalation poller tests ──────────────────────────────────────────────────

// Behavior 5: Poller calls tier-2 after tier-1 timeout expires.
func TestEscalationPoller_CallsTier2AfterTimeout(t *testing.T) {
	rdb, _ := newTestRedis(t)
	repo := newFakeSOSRepository()
	caller := &fakeCaller{}

	const wearerID = "wearer-poller-1"
	const sosID = "sos-poller-1"

	repo.contacts[wearerID] = []ContactConfig{
		{FullName: "Alice", Phone: "+15551111111", Tier: 1, TimeoutSec: 30},
		{FullName: "Bob", Phone: "+15552222222", Tier: 2, TimeoutSec: 30},
	}
	repo.events[sosID] = &SOSEvent{
		ID: sosID, WearerID: wearerID, Status: "active", TriggeredAt: time.Now(),
	}

	// Simulate an expired tier-1 deadline.
	pastScore := float64(time.Now().Add(-1 * time.Second).Unix())
	member := sosID + ":" + wearerID + ":1"
	rdb.ZAdd(context.Background(), "sos:escalations", redis.Z{Score: pastScore, Member: member})

	poller := NewEscalationPoller(rdb, repo, caller)
	poller.processExpired(context.Background())

	calls := caller.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call to tier-2, got %d", len(calls))
	}
	if calls[0] != "+15552222222" {
		t.Errorf("expected Bob's number +15552222222, got %q", calls[0])
	}
}

// Behavior 6: Poller calls 911 when all tiers exhausted and auto_911=true.
func TestEscalationPoller_Calls911WhenAuto911Enabled(t *testing.T) {
	rdb, _ := newTestRedis(t)
	repo := newFakeSOSRepository()
	caller := &fakeCaller{}

	const wearerID = "wearer-auto911"
	const sosID = "sos-auto911"

	// Only tier-1; no tier-2.
	repo.contacts[wearerID] = []ContactConfig{
		{FullName: "Alice", Phone: "+15551111111", Tier: 1, TimeoutSec: 30},
	}
	repo.events[sosID] = &SOSEvent{
		ID: sosID, WearerID: wearerID, Status: "active", TriggeredAt: time.Now(),
	}
	repo.settings[wearerID] = &SOSSettings{WearerID: wearerID, Auto911: true}

	// Tier-1 timed out.
	pastScore := float64(time.Now().Add(-1 * time.Second).Unix())
	member := sosID + ":" + wearerID + ":1"
	rdb.ZAdd(context.Background(), "sos:escalations", redis.Z{Score: pastScore, Member: member})

	poller := NewEscalationPoller(rdb, repo, caller)
	poller.processExpired(context.Background())

	calls := caller.getCalls()
	if len(calls) != 1 || calls[0] != "911" {
		t.Errorf("expected one call to 911, got %v", calls)
	}
}

// Behavior 7: Poller does NOT call 911 when auto_911=false.
func TestEscalationPoller_DoesNotCall911WhenDisabled(t *testing.T) {
	rdb, _ := newTestRedis(t)
	repo := newFakeSOSRepository()
	caller := &fakeCaller{}

	const wearerID = "wearer-no911"
	const sosID = "sos-no911"

	repo.contacts[wearerID] = []ContactConfig{
		{FullName: "Alice", Phone: "+15551111111", Tier: 1, TimeoutSec: 30},
	}
	repo.events[sosID] = &SOSEvent{
		ID: sosID, WearerID: wearerID, Status: "active", TriggeredAt: time.Now(),
	}
	repo.settings[wearerID] = &SOSSettings{WearerID: wearerID, Auto911: false}

	pastScore := float64(time.Now().Add(-1 * time.Second).Unix())
	member := sosID + ":" + wearerID + ":1"
	rdb.ZAdd(context.Background(), "sos:escalations", redis.Z{Score: pastScore, Member: member})

	poller := NewEscalationPoller(rdb, repo, caller)
	poller.processExpired(context.Background())

	if calls := caller.getCalls(); len(calls) != 0 {
		t.Errorf("expected no calls when auto_911=false, got %v", calls)
	}
}

// Behavior 8: Poller skips cancelled SOS events.
func TestEscalationPoller_SkipsCancelledSOS(t *testing.T) {
	rdb, _ := newTestRedis(t)
	repo := newFakeSOSRepository()
	caller := &fakeCaller{}

	const wearerID = "wearer-cancelled-sos"
	const sosID = "sos-cancelled"

	repo.contacts[wearerID] = []ContactConfig{
		{FullName: "Alice", Phone: "+15551111111", Tier: 1, TimeoutSec: 30},
		{FullName: "Bob", Phone: "+15552222222", Tier: 2, TimeoutSec: 30},
	}
	repo.events[sosID] = &SOSEvent{
		ID: sosID, WearerID: wearerID, Status: "active", TriggeredAt: time.Now(),
	}

	// Mark SOS as cancelled in Redis (simulating Cancel handler).
	rdb.Set(context.Background(), "sos:cancelled:"+sosID, "1", 24*time.Hour)

	pastScore := float64(time.Now().Add(-1 * time.Second).Unix())
	member := sosID + ":" + wearerID + ":1"
	rdb.ZAdd(context.Background(), "sos:escalations", redis.Z{Score: pastScore, Member: member})

	poller := NewEscalationPoller(rdb, repo, caller)
	poller.processExpired(context.Background())

	if calls := caller.getCalls(); len(calls) != 0 {
		t.Errorf("expected no calls for cancelled SOS, got %v", calls)
	}
}

// ─── chi URL param helper ─────────────────────────────────────────────────────

// injectURLParam injects a chi route parameter into the request context for unit tests.
// This avoids needing a running chi router.
func injectURLParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}
