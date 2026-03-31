package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/shahprincea/leo/backend/internal/auth"
	"github.com/shahprincea/leo/backend/internal/config"
)

// ---- fakeWearerRepo ----

type fakeWearerRepo struct {
	mu      sync.Mutex
	wearers map[string]*Wearer        // keyed by ID
	members map[string]*WearerMember  // keyed by ID
}

func newFakeWearerRepo() *fakeWearerRepo {
	return &fakeWearerRepo{
		wearers: make(map[string]*Wearer),
		members: make(map[string]*WearerMember),
	}
}

func (f *fakeWearerRepo) CreateWearer(_ context.Context, ownerUserID, fullName, _ string, opts WearerCreateOpts) (*Wearer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Use a slug (no spaces) so the ID is safe to embed in URL paths.
	slug := strings.ReplaceAll(strings.ToLower(fullName), " ", "-")
	id := "wearer-" + slug
	w := &Wearer{
		ID:                id,
		OwnerUserID:       ownerUserID,
		FullName:          fullName,
		DateOfBirth:       opts.DateOfBirth,
		BloodType:         opts.BloodType,
		MedicalConditions: opts.MedicalConditions,
		Allergies:         opts.Allergies,
		Notes:             opts.Notes,
		CreatedAt:         time.Now(),
	}
	f.wearers[id] = w

	// Auto-add owner as admin member (mirrors Postgres behaviour).
	memberID := "member-owner-" + id
	f.members[memberID] = &WearerMember{
		ID:              memberID,
		WearerID:        id,
		UserID:          ownerUserID,
		Role:            "admin",
		CanViewLocation: true,
		InvitedAt:       time.Now(),
	}
	return w, nil
}

func (f *fakeWearerRepo) FindWearerByID(_ context.Context, id string) (*Wearer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w, ok := f.wearers[id]
	if !ok {
		return nil, pgx.ErrNoRows
	}
	return w, nil
}

func (f *fakeWearerRepo) UpdateWearer(_ context.Context, id string, opts WearerUpdateOpts) (*Wearer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w, ok := f.wearers[id]
	if !ok {
		return nil, pgx.ErrNoRows
	}
	if opts.FullName != nil {
		w.FullName = *opts.FullName
	}
	if opts.DateOfBirth != nil {
		w.DateOfBirth = opts.DateOfBirth
	}
	if opts.BloodType != nil {
		w.BloodType = opts.BloodType
	}
	if opts.MedicalConditions != nil {
		w.MedicalConditions = opts.MedicalConditions
	}
	if opts.Allergies != nil {
		w.Allergies = opts.Allergies
	}
	if opts.Notes != nil {
		w.Notes = opts.Notes
	}
	return w, nil
}

func (f *fakeWearerRepo) IsWearerAdmin(_ context.Context, wearerID, userID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.members {
		if m.WearerID == wearerID && m.UserID == userID && m.Role == "admin" {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeWearerRepo) GetWearerMembership(_ context.Context, wearerID, userID string) (*WearerMember, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.members {
		if m.WearerID == wearerID && m.UserID == userID {
			return m, nil
		}
	}
	return nil, nil
}

func (f *fakeWearerRepo) InviteMember(_ context.Context, wearerID, userID, role string, canViewLocation bool) (*WearerMember, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Check for duplicate.
	for _, m := range f.members {
		if m.WearerID == wearerID && m.UserID == userID {
			return nil, errAlreadyMember
		}
	}
	id := "member-" + wearerID + "-" + userID
	m := &WearerMember{
		ID:              id,
		WearerID:        wearerID,
		UserID:          userID,
		Role:            role,
		CanViewLocation: canViewLocation,
		InvitedAt:       time.Now(),
	}
	f.members[id] = m
	return m, nil
}

func (f *fakeWearerRepo) ListMembers(_ context.Context, wearerID string) ([]*WearerMemberWithUser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var result []*WearerMemberWithUser
	for _, m := range f.members {
		if m.WearerID == wearerID {
			result = append(result, &WearerMemberWithUser{
				WearerMember: *m,
				User: &MemberUser{
					ID:       m.UserID,
					Email:    m.UserID + "@example.com",
					FullName: "User " + m.UserID,
				},
			})
		}
	}
	return result, nil
}

func (f *fakeWearerRepo) UpdateMember(_ context.Context, memberID, wearerID string, opts MemberUpdateOpts) (*WearerMember, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.members[memberID]
	if !ok || m.WearerID != wearerID {
		return nil, pgx.ErrNoRows
	}
	if opts.Role != nil {
		m.Role = *opts.Role
	}
	if opts.CanViewLocation != nil {
		m.CanViewLocation = *opts.CanViewLocation
	}
	return m, nil
}

func (f *fakeWearerRepo) FindMemberByID(_ context.Context, memberID string) (*WearerMember, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.members[memberID]
	if !ok {
		return nil, pgx.ErrNoRows
	}
	return m, nil
}

// ---- fakeUserRepo (UserRepository for wearers tests) ----

type fakeUserRepoWearer struct {
	mu    sync.Mutex
	users map[string]*User // keyed by email
}

func newFakeUserRepoWearer() *fakeUserRepoWearer {
	return &fakeUserRepoWearer{users: make(map[string]*User)}
}

func (f *fakeUserRepoWearer) CreateUser(_ context.Context, email, passwordHash, fullName string, phone *string) (*User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u := &User{ID: "uid-" + email, Email: email, FullName: fullName, PasswordHash: passwordHash}
	f.users[email] = u
	return u, nil
}

func (f *fakeUserRepoWearer) FindUserByEmail(_ context.Context, email string) (*User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[email]
	if !ok {
		return nil, pgx.ErrNoRows
	}
	return u, nil
}

func (f *fakeUserRepoWearer) FindUserByID(_ context.Context, id string) (*User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, u := range f.users {
		if u.ID == id {
			return u, nil
		}
	}
	return nil, pgx.ErrNoRows
}

func (f *fakeUserRepoWearer) FindWearerByID(_ context.Context, _ string) (*WearerAuth, error) {
	return nil, pgx.ErrNoRows
}

// ---- test helpers ----

const wearerTestJWTSecret = "wearer-test-secret"

// newTestWearerHandler wires fake repos and returns the handler plus the repos
// so tests can pre-seed state.
func newTestWearerHandler() (*WearerHandler, *fakeWearerRepo, *fakeUserRepoWearer) {
	wRepo := newFakeWearerRepo()
	uRepo := newFakeUserRepoWearer()
	h := &WearerHandler{
		db:     wRepo,
		userDB: uRepo,
		cfg:    &config.Config{JWTSecret: wearerTestJWTSecret},
	}
	return h, wRepo, uRepo
}

// withUserID injects a *auth.Claims into the request context so that
// auth.UserIDFromContext returns the supplied userID.
func withUserID(r *http.Request, userID string) *http.Request {
	claims := &auth.Claims{UserID: userID, Email: userID + "@example.com"}
	// Use auth.RequireAuth middleware approach: store Claims under the same
	// unexported key by going through a signed token round-trip.
	token, err := auth.SignAccessToken(userID, userID+"@example.com", wearerTestJWTSecret)
	if err != nil {
		panic("withUserID: SignAccessToken: " + err.Error())
	}
	_ = claims // already embedded in the token
	// Apply the middleware inline.
	var ctx context.Context
	mw := auth.RequireAuth(wearerTestJWTSecret)
	var captured context.Context
	inner := http.HandlerFunc(func(_ http.ResponseWriter, r2 *http.Request) {
		captured = r2.Context()
	})
	rec := httptest.NewRecorder()
	r.Header.Set("Authorization", "Bearer "+token)
	mw(inner).ServeHTTP(rec, r)
	ctx = captured
	if ctx == nil {
		panic("withUserID: middleware did not call inner handler")
	}
	return r.WithContext(ctx)
}

// withWearerID sets the chi route context param "wearerID".
func withWearerID(r *http.Request, id string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("wearerID", id)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// withMemberID sets the chi route context param "memberID".
func withMemberID(r *http.Request, id string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("memberID", id)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func wearerPostJSON(handler http.HandlerFunc, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler(rr, req)
	return rr
}

func wearerPostJSONAuth(handler http.HandlerFunc, userID string, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req = withUserID(req, userID)
	rr := httptest.NewRecorder()
	handler(rr, req)
	return rr
}

func decodeWearerBody(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&m); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	return m
}

// ---- TestCreateWearer ----

func TestCreateWearer_Success(t *testing.T) {
	h, _, _ := newTestWearerHandler()

	rr := wearerPostJSONAuth(h.Create, "user-1", map[string]any{
		"full_name": "Alice Wearer",
		"pin":       "1234",
	})

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d — body: %s", rr.Code, rr.Body)
	}
	m := decodeWearerBody(t, rr)
	wearer, ok := m["wearer"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'wearer' key in response, got: %v", m)
	}
	if wearer["full_name"] != "Alice Wearer" {
		t.Errorf("expected full_name 'Alice Wearer', got %v", wearer["full_name"])
	}
	if _, hasPin := wearer["pin_hash"]; hasPin {
		t.Error("pin_hash must not be present in response")
	}
	if wearer["id"] == nil || wearer["id"] == "" {
		t.Error("expected non-empty id")
	}
}

func TestCreateWearer_MissingFullName(t *testing.T) {
	h, _, _ := newTestWearerHandler()

	rr := wearerPostJSONAuth(h.Create, "user-1", map[string]any{
		"pin": "1234",
	})

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d — body: %s", rr.Code, rr.Body)
	}
}

func TestCreateWearer_InvalidPin(t *testing.T) {
	h, _, _ := newTestWearerHandler()

	rr := wearerPostJSONAuth(h.Create, "user-1", map[string]any{
		"full_name": "Bob",
		"pin":       "abc",
	})

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d — body: %s", rr.Code, rr.Body)
	}
}

func TestCreateWearer_InvalidPin_TooShort(t *testing.T) {
	h, _, _ := newTestWearerHandler()

	rr := wearerPostJSONAuth(h.Create, "user-1", map[string]any{
		"full_name": "Bob",
		"pin":       "123",
	})

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d — body: %s", rr.Code, rr.Body)
	}
}

// ---- TestGetWearer ----

// seedWearer creates a wearer via the repo and adds the given userID as an admin member.
func seedWearer(t *testing.T, wRepo *fakeWearerRepo, ownerID, fullName string) *Wearer {
	t.Helper()
	w, err := wRepo.CreateWearer(context.Background(), ownerID, fullName, "hash", WearerCreateOpts{})
	if err != nil {
		t.Fatalf("seedWearer: %v", err)
	}
	return w
}

func TestGetWearer_Success(t *testing.T) {
	h, wRepo, _ := newTestWearerHandler()
	w := seedWearer(t, wRepo, "admin-user", "Test Wearer")

	req := httptest.NewRequest(http.MethodGet, "/wearers/test", nil)
	req = withUserID(req, "admin-user")
	req = withWearerID(req, w.ID)

	rr := httptest.NewRecorder()
	h.Get(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", rr.Code, rr.Body)
	}
	m := decodeWearerBody(t, rr)
	wearer, ok := m["wearer"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'wearer' key in response")
	}
	if wearer["id"] != w.ID {
		t.Errorf("expected id %q, got %v", w.ID, wearer["id"])
	}
}

func TestGetWearer_NotMember(t *testing.T) {
	h, wRepo, _ := newTestWearerHandler()
	w := seedWearer(t, wRepo, "admin-user", "Test Wearer2")

	req := httptest.NewRequest(http.MethodGet, "/wearers/test", nil)
	req = withUserID(req, "non-member-user")
	req = withWearerID(req, w.ID)

	rr := httptest.NewRecorder()
	h.Get(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d — body: %s", rr.Code, rr.Body)
	}
}

func TestGetWearer_NotFound(t *testing.T) {
	h, wRepo, _ := newTestWearerHandler()
	// Add user as a member of a wearer that doesn't exist in the wearers map.
	wRepo.mu.Lock()
	wRepo.members["m-ghost"] = &WearerMember{
		ID:       "m-ghost",
		WearerID: "ghost-wearer-id",
		UserID:   "user-x",
		Role:     "admin",
	}
	wRepo.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/wearers/ghost-wearer-id", nil)
	req = withUserID(req, "user-x")
	req = withWearerID(req, "ghost-wearer-id")

	rr := httptest.NewRecorder()
	h.Get(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d — body: %s", rr.Code, rr.Body)
	}
}

// ---- TestUpdateWearer ----

func TestUpdateWearer_Success(t *testing.T) {
	h, wRepo, _ := newTestWearerHandler()
	w := seedWearer(t, wRepo, "admin-user", "Original Name")

	body, _ := json.Marshal(map[string]any{"full_name": "Updated Name"})
	req := httptest.NewRequest(http.MethodPatch, "/wearers/"+w.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUserID(req, "admin-user")
	req = withWearerID(req, w.ID)

	rr := httptest.NewRecorder()
	h.Update(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", rr.Code, rr.Body)
	}
	m := decodeWearerBody(t, rr)
	wearer, ok := m["wearer"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'wearer' key in response")
	}
	if wearer["full_name"] != "Updated Name" {
		t.Errorf("expected full_name 'Updated Name', got %v", wearer["full_name"])
	}
}

func TestUpdateWearer_NotAdmin(t *testing.T) {
	h, wRepo, _ := newTestWearerHandler()
	w := seedWearer(t, wRepo, "admin-user", "Another Wearer")

	// Add non-admin member.
	wRepo.mu.Lock()
	wRepo.members["member-non-admin"] = &WearerMember{
		ID:       "member-non-admin",
		WearerID: w.ID,
		UserID:   "regular-member",
		Role:     "member",
	}
	wRepo.mu.Unlock()

	body, _ := json.Marshal(map[string]any{"full_name": "Hacked Name"})
	req := httptest.NewRequest(http.MethodPatch, "/wearers/"+w.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUserID(req, "regular-member")
	req = withWearerID(req, w.ID)

	rr := httptest.NewRecorder()
	h.Update(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d — body: %s", rr.Code, rr.Body)
	}
}

// ---- TestInviteMember ----

func TestInviteMember_Success(t *testing.T) {
	h, wRepo, uRepo := newTestWearerHandler()
	w := seedWearer(t, wRepo, "admin-user", "Family Wearer")

	// Pre-register the invitee in the user repo.
	uRepo.users["invite@example.com"] = &User{
		ID:       "uid-invite@example.com",
		Email:    "invite@example.com",
		FullName: "Invite User",
	}

	body, _ := json.Marshal(map[string]any{
		"email": "invite@example.com",
		"role":  "member",
	})
	req := httptest.NewRequest(http.MethodPost, "/wearers/"+w.ID+"/members", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUserID(req, "admin-user")
	req = withWearerID(req, w.ID)

	rr := httptest.NewRecorder()
	h.InviteMember(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d — body: %s", rr.Code, rr.Body)
	}
	m := decodeWearerBody(t, rr)
	if m["member"] == nil {
		t.Error("expected 'member' key in response")
	}
}

func TestInviteMember_NotAdmin(t *testing.T) {
	h, wRepo, uRepo := newTestWearerHandler()
	w := seedWearer(t, wRepo, "admin-user", "Family Wearer2")

	// Add non-admin member.
	wRepo.mu.Lock()
	wRepo.members["member-regular"] = &WearerMember{
		ID:       "member-regular",
		WearerID: w.ID,
		UserID:   "regular-user",
		Role:     "member",
	}
	wRepo.mu.Unlock()

	uRepo.users["other@example.com"] = &User{ID: "uid-other", Email: "other@example.com", FullName: "Other"}

	body, _ := json.Marshal(map[string]any{"email": "other@example.com", "role": "member"})
	req := httptest.NewRequest(http.MethodPost, "/wearers/"+w.ID+"/members", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUserID(req, "regular-user")
	req = withWearerID(req, w.ID)

	rr := httptest.NewRecorder()
	h.InviteMember(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d — body: %s", rr.Code, rr.Body)
	}
}

func TestInviteMember_AlreadyMember(t *testing.T) {
	h, wRepo, uRepo := newTestWearerHandler()
	w := seedWearer(t, wRepo, "admin-user", "Family Wearer3")

	// Pre-register the invitee and add them as a member already.
	inviteeEmail := "already@example.com"
	inviteeID := "uid-already@example.com"
	uRepo.users[inviteeEmail] = &User{ID: inviteeID, Email: inviteeEmail, FullName: "Already Member"}

	wRepo.mu.Lock()
	wRepo.members["member-already"] = &WearerMember{
		ID:       "member-already",
		WearerID: w.ID,
		UserID:   inviteeID,
		Role:     "member",
	}
	wRepo.mu.Unlock()

	body, _ := json.Marshal(map[string]any{"email": inviteeEmail, "role": "member"})
	req := httptest.NewRequest(http.MethodPost, "/wearers/"+w.ID+"/members", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUserID(req, "admin-user")
	req = withWearerID(req, w.ID)

	rr := httptest.NewRecorder()
	h.InviteMember(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d — body: %s", rr.Code, rr.Body)
	}
}

func TestInviteMember_UserNotFound(t *testing.T) {
	h, wRepo, _ := newTestWearerHandler()
	w := seedWearer(t, wRepo, "admin-user", "Family Wearer4")

	body, _ := json.Marshal(map[string]any{"email": "unknown@example.com", "role": "member"})
	req := httptest.NewRequest(http.MethodPost, "/wearers/"+w.ID+"/members", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUserID(req, "admin-user")
	req = withWearerID(req, w.ID)

	rr := httptest.NewRecorder()
	h.InviteMember(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d — body: %s", rr.Code, rr.Body)
	}
}

// ---- TestListMembers ----

func TestListMembers_Success(t *testing.T) {
	h, wRepo, _ := newTestWearerHandler()
	w := seedWearer(t, wRepo, "admin-user", "List Wearer")

	req := httptest.NewRequest(http.MethodGet, "/wearers/"+w.ID+"/members", nil)
	req = withUserID(req, "admin-user")
	req = withWearerID(req, w.ID)

	rr := httptest.NewRecorder()
	h.ListMembers(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", rr.Code, rr.Body)
	}
	m := decodeWearerBody(t, rr)
	members, ok := m["members"].([]any)
	if !ok {
		t.Fatalf("expected 'members' array in response, got: %v", m)
	}
	// seedWearer adds the owner as a member, so there should be at least 1.
	if len(members) < 1 {
		t.Errorf("expected at least 1 member, got %d", len(members))
	}
}

// ---- TestUpdateMember ----

func TestUpdateMember_Success(t *testing.T) {
	h, wRepo, _ := newTestWearerHandler()
	w := seedWearer(t, wRepo, "admin-user", "Update Member Wearer")

	// Add a member to update.
	memberID := "member-to-update"
	wRepo.mu.Lock()
	wRepo.members[memberID] = &WearerMember{
		ID:              memberID,
		WearerID:        w.ID,
		UserID:          "target-user",
		Role:            "member",
		CanViewLocation: false,
	}
	wRepo.mu.Unlock()

	body, _ := json.Marshal(map[string]any{"role": "admin", "can_view_location": true})
	req := httptest.NewRequest(http.MethodPatch, "/wearer-members/"+memberID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUserID(req, "admin-user")
	req = withMemberID(req, memberID)

	rr := httptest.NewRecorder()
	h.UpdateMember(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", rr.Code, rr.Body)
	}
	m := decodeWearerBody(t, rr)
	member, ok := m["member"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'member' key in response, got: %v", m)
	}
	if member["role"] != "admin" {
		t.Errorf("expected role 'admin', got %v", member["role"])
	}
	if member["can_view_location"] != true {
		t.Errorf("expected can_view_location true, got %v", member["can_view_location"])
	}
}

func TestUpdateMember_NotAdmin(t *testing.T) {
	h, wRepo, _ := newTestWearerHandler()
	w := seedWearer(t, wRepo, "admin-user", "Update Member Wearer2")

	// Add two non-admin members.
	memberID := "member-non-admin-2"
	wRepo.mu.Lock()
	wRepo.members[memberID] = &WearerMember{
		ID:       memberID,
		WearerID: w.ID,
		UserID:   "target-user-2",
		Role:     "member",
	}
	wRepo.members["member-requester"] = &WearerMember{
		ID:       "member-requester",
		WearerID: w.ID,
		UserID:   "non-admin-requester",
		Role:     "member",
	}
	wRepo.mu.Unlock()

	body, _ := json.Marshal(map[string]any{"role": "admin"})
	req := httptest.NewRequest(http.MethodPatch, "/wearer-members/"+memberID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUserID(req, "non-admin-requester")
	req = withMemberID(req, memberID)

	rr := httptest.NewRecorder()
	h.UpdateMember(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d — body: %s", rr.Code, rr.Body)
	}
}
