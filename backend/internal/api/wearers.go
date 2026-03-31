package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shahprincea/leo/backend/internal/auth"
	"github.com/shahprincea/leo/backend/internal/config"
)

// errAlreadyMember is returned when a user is already a member of the wearer group.
var errAlreadyMember = errors.New("already a member")

// UUID is an alias for string used to indicate UUID-typed fields.
type UUID = string

// Wearer is the full domain model for a wearer profile.
type Wearer struct {
	ID                UUID
	OwnerUserID       UUID
	FullName          string
	DateOfBirth       *time.Time
	PhotoURL          *string
	BloodType         *string
	MedicalConditions []string
	Allergies         []string
	Notes             *string
	CreatedAt         time.Time
	DeletedAt         *time.Time
}

// WearerCreateOpts holds optional fields for creating a wearer.
type WearerCreateOpts struct {
	DateOfBirth       *time.Time
	BloodType         *string
	MedicalConditions []string
	Allergies         []string
	Notes             *string
}

// WearerUpdateOpts holds optional fields for updating a wearer.
type WearerUpdateOpts struct {
	FullName          *string
	DateOfBirth       *time.Time
	BloodType         *string
	MedicalConditions []string
	Allergies         []string
	Notes             *string
}

// WearerMember represents a user's membership in a wearer's family group.
type WearerMember struct {
	ID              string
	WearerID        string
	UserID          string
	Role            string // "admin" | "member"
	CanViewLocation bool
	InvitedAt       time.Time
	AcceptedAt      *time.Time
}

// WearerMemberWithUser embeds WearerMember and adds joined user info.
type WearerMemberWithUser struct {
	WearerMember
	User *MemberUser
}

// MemberUser holds the public user fields returned alongside membership data.
type MemberUser struct {
	ID       string
	Email    string
	FullName string
	Phone    *string
}

// MemberUpdateOpts holds optional fields for updating a wearer member.
type MemberUpdateOpts struct {
	Role            *string
	CanViewLocation *bool
}

// WearerRepository abstracts DB access for wearer handlers.
type WearerRepository interface {
	CreateWearer(ctx context.Context, ownerUserID, fullName, pinHash string, opts WearerCreateOpts) (*Wearer, error)
	FindWearerByID(ctx context.Context, id string) (*Wearer, error)
	UpdateWearer(ctx context.Context, id string, opts WearerUpdateOpts) (*Wearer, error)
	IsWearerAdmin(ctx context.Context, wearerID, userID string) (bool, error)
	GetWearerMembership(ctx context.Context, wearerID, userID string) (*WearerMember, error)
	InviteMember(ctx context.Context, wearerID, userID, role string, canViewLocation bool) (*WearerMember, error)
	ListMembers(ctx context.Context, wearerID string) ([]*WearerMemberWithUser, error)
	UpdateMember(ctx context.Context, memberID, wearerID string, opts MemberUpdateOpts) (*WearerMember, error)
	FindMemberByID(ctx context.Context, memberID string) (*WearerMember, error)
}

// WearerHandler handles wearer CRUD endpoints.
type WearerHandler struct {
	db     WearerRepository
	userDB UserRepository
	cfg    *config.Config
}

// NewWearerHandler creates a WearerHandler backed by real Postgres.
func NewWearerHandler(db *pgxpool.Pool, _ interface{}, cfg *config.Config) *WearerHandler {
	return &WearerHandler{
		db:     NewPostgresWearerRepository(db),
		userDB: NewPostgresUserRepository(db),
		cfg:    cfg,
	}
}

// wearerResponse is the public JSON representation of a Wearer (no pin_hash).
type wearerResponse struct {
	ID                string     `json:"id"`
	OwnerUserID       string     `json:"owner_user_id"`
	FullName          string     `json:"full_name"`
	DateOfBirth       *string    `json:"date_of_birth,omitempty"`
	PhotoURL          *string    `json:"photo_url,omitempty"`
	BloodType         *string    `json:"blood_type,omitempty"`
	MedicalConditions []string   `json:"medical_conditions"`
	Allergies         []string   `json:"allergies"`
	Notes             *string    `json:"notes,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
}

func toWearerResponse(w *Wearer) wearerResponse {
	resp := wearerResponse{
		ID:                w.ID,
		OwnerUserID:       w.OwnerUserID,
		FullName:          w.FullName,
		PhotoURL:          w.PhotoURL,
		BloodType:         w.BloodType,
		MedicalConditions: w.MedicalConditions,
		Allergies:         w.Allergies,
		Notes:             w.Notes,
		CreatedAt:         w.CreatedAt,
	}
	if resp.MedicalConditions == nil {
		resp.MedicalConditions = []string{}
	}
	if resp.Allergies == nil {
		resp.Allergies = []string{}
	}
	if w.DateOfBirth != nil {
		s := w.DateOfBirth.Format("2006-01-02")
		resp.DateOfBirth = &s
	}
	return resp
}

var pinRegexp = regexp.MustCompile(`^\d{4}$`)

// Create handles POST /wearers.
func (h *WearerHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		FullName          string   `json:"full_name"`
		PIN               string   `json:"pin"`
		DateOfBirth       string   `json:"date_of_birth"`
		BloodType         string   `json:"blood_type"`
		MedicalConditions []string `json:"medical_conditions"`
		Allergies         []string `json:"allergies"`
		Notes             string   `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.FullName == "" {
		writeError(w, http.StatusBadRequest, "full_name is required")
		return
	}
	if !pinRegexp.MatchString(req.PIN) {
		writeError(w, http.StatusBadRequest, "pin must be exactly 4 digits")
		return
	}

	ctx := r.Context()
	userID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	pinHash, err := auth.HashPassword(req.PIN)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	opts := WearerCreateOpts{
		MedicalConditions: req.MedicalConditions,
		Allergies:         req.Allergies,
	}
	if req.DateOfBirth != "" {
		dob, err := time.Parse("2006-01-02", req.DateOfBirth)
		if err != nil {
			writeError(w, http.StatusBadRequest, "date_of_birth must be in YYYY-MM-DD format")
			return
		}
		opts.DateOfBirth = &dob
	}
	if req.BloodType != "" {
		opts.BloodType = &req.BloodType
	}
	if req.Notes != "" {
		opts.Notes = &req.Notes
	}

	wearer, err := h.db.CreateWearer(ctx, userID, req.FullName, pinHash, opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"wearer": toWearerResponse(wearer)})
}

// Get handles GET /wearers/:wearerID.
func (h *WearerHandler) Get(w http.ResponseWriter, r *http.Request) {
	wearerID := chi.URLParam(r, "wearerID")
	ctx := r.Context()

	userID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	membership, err := h.db.GetWearerMembership(ctx, wearerID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if membership == nil {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	wearer, err := h.db.FindWearerByID(ctx, wearerID)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "wearer not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"wearer": toWearerResponse(wearer)})
}

// Update handles PATCH /wearers/:wearerID.
func (h *WearerHandler) Update(w http.ResponseWriter, r *http.Request) {
	wearerID := chi.URLParam(r, "wearerID")
	ctx := r.Context()

	userID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	isAdmin, err := h.db.IsWearerAdmin(ctx, wearerID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if !isAdmin {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	// Use a raw map to distinguish missing fields from null/empty.
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	opts := WearerUpdateOpts{}

	if v, ok := raw["full_name"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err == nil && s != "" {
			opts.FullName = &s
		}
	}
	if v, ok := raw["date_of_birth"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err == nil && s != "" {
			dob, err := time.Parse("2006-01-02", s)
			if err != nil {
				writeError(w, http.StatusBadRequest, "date_of_birth must be in YYYY-MM-DD format")
				return
			}
			opts.DateOfBirth = &dob
		}
	}
	if v, ok := raw["blood_type"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err == nil && s != "" {
			opts.BloodType = &s
		}
	}
	if v, ok := raw["medical_conditions"]; ok {
		var arr []string
		if err := json.Unmarshal(v, &arr); err == nil {
			opts.MedicalConditions = arr
		}
	}
	if v, ok := raw["allergies"]; ok {
		var arr []string
		if err := json.Unmarshal(v, &arr); err == nil {
			opts.Allergies = arr
		}
	}
	if v, ok := raw["notes"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err == nil {
			opts.Notes = &s
		}
	}

	wearer, err := h.db.UpdateWearer(ctx, wearerID, opts)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "wearer not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"wearer": toWearerResponse(wearer)})
}

// memberResponse is the JSON shape for a WearerMember.
type memberResponse struct {
	ID              string     `json:"id"`
	WearerID        string     `json:"wearer_id"`
	UserID          string     `json:"user_id"`
	Role            string     `json:"role"`
	CanViewLocation bool       `json:"can_view_location"`
	InvitedAt       time.Time  `json:"invited_at"`
	AcceptedAt      *time.Time `json:"accepted_at"`
}

// memberWithUserResponse adds user info to memberResponse.
type memberWithUserResponse struct {
	memberResponse
	User *memberUserResponse `json:"user,omitempty"`
}

type memberUserResponse struct {
	ID       string  `json:"id"`
	Email    string  `json:"email"`
	FullName string  `json:"full_name"`
	Phone    *string `json:"phone,omitempty"`
}

func toMemberResponse(m *WearerMember) memberResponse {
	return memberResponse{
		ID:              m.ID,
		WearerID:        m.WearerID,
		UserID:          m.UserID,
		Role:            m.Role,
		CanViewLocation: m.CanViewLocation,
		InvitedAt:       m.InvitedAt,
		AcceptedAt:      m.AcceptedAt,
	}
}

func toMemberWithUserResponse(m *WearerMemberWithUser) memberWithUserResponse {
	r := memberWithUserResponse{
		memberResponse: toMemberResponse(&m.WearerMember),
	}
	if m.User != nil {
		r.User = &memberUserResponse{
			ID:       m.User.ID,
			Email:    m.User.Email,
			FullName: m.User.FullName,
			Phone:    m.User.Phone,
		}
	}
	return r
}

// InviteMember handles POST /wearers/:wearerID/members.
func (h *WearerHandler) InviteMember(w http.ResponseWriter, r *http.Request) {
	wearerID := chi.URLParam(r, "wearerID")
	ctx := r.Context()

	userID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req struct {
		Email           string `json:"email"`
		Role            string `json:"role"`
		CanViewLocation *bool  `json:"can_view_location"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}
	if req.Role == "" {
		req.Role = "member"
	}
	if req.Role != "admin" && req.Role != "member" {
		writeError(w, http.StatusBadRequest, `role must be "admin" or "member"`)
		return
	}
	canViewLocation := true
	if req.CanViewLocation != nil {
		canViewLocation = *req.CanViewLocation
	}

	isAdmin, err := h.db.IsWearerAdmin(ctx, wearerID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if !isAdmin {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	target, err := h.userDB.FindUserByEmail(ctx, req.Email)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "user not found — they must register first")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	member, err := h.db.InviteMember(ctx, wearerID, target.ID, req.Role, canViewLocation)
	if errors.Is(err, errAlreadyMember) {
		writeError(w, http.StatusConflict, "already a member")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"member": toMemberResponse(member)})
}

// ListMembers handles GET /wearers/:wearerID/members.
func (h *WearerHandler) ListMembers(w http.ResponseWriter, r *http.Request) {
	wearerID := chi.URLParam(r, "wearerID")
	ctx := r.Context()

	userID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	membership, err := h.db.GetWearerMembership(ctx, wearerID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if membership == nil {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	members, err := h.db.ListMembers(ctx, wearerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	resp := make([]memberWithUserResponse, 0, len(members))
	for _, m := range members {
		resp = append(resp, toMemberWithUserResponse(m))
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": resp})
}

// UpdateMember handles PATCH /wearer-members/:memberID.
func (h *WearerHandler) UpdateMember(w http.ResponseWriter, r *http.Request) {
	memberID := chi.URLParam(r, "memberID")
	ctx := r.Context()

	userID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	existing, err := h.db.FindMemberByID(ctx, memberID)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	isAdmin, err := h.db.IsWearerAdmin(ctx, existing.WearerID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if !isAdmin {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	opts := MemberUpdateOpts{}
	if v, ok := raw["role"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err == nil {
			if s != "admin" && s != "member" {
				writeError(w, http.StatusBadRequest, `role must be "admin" or "member"`)
				return
			}
			opts.Role = &s
		}
	}
	if v, ok := raw["can_view_location"]; ok {
		var b bool
		if err := json.Unmarshal(v, &b); err == nil {
			opts.CanViewLocation = &b
		}
	}

	updated, err := h.db.UpdateMember(ctx, memberID, existing.WearerID, opts)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"member": toMemberResponse(updated)})
}

// --- PostgresWearerRepository ---

// PostgresWearerRepository implements WearerRepository using pgxpool.Pool.
type PostgresWearerRepository struct {
	db *pgxpool.Pool
}

// NewPostgresWearerRepository creates a PostgresWearerRepository.
func NewPostgresWearerRepository(db *pgxpool.Pool) *PostgresWearerRepository {
	return &PostgresWearerRepository{db: db}
}

func (r *PostgresWearerRepository) CreateWearer(ctx context.Context, ownerUserID, fullName, pinHash string, opts WearerCreateOpts) (*Wearer, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	w := &Wearer{}
	err = tx.QueryRow(ctx,
		`INSERT INTO wearers (owner_user_id, full_name, pin_hash, date_of_birth, blood_type, medical_conditions, allergies, notes)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, owner_user_id, full_name, date_of_birth, photo_url, blood_type, medical_conditions, allergies, notes, created_at, deleted_at`,
		ownerUserID, fullName, pinHash,
		opts.DateOfBirth, opts.BloodType, opts.MedicalConditions, opts.Allergies, opts.Notes,
	).Scan(
		&w.ID, &w.OwnerUserID, &w.FullName,
		&w.DateOfBirth, &w.PhotoURL, &w.BloodType,
		&w.MedicalConditions, &w.Allergies, &w.Notes,
		&w.CreatedAt, &w.DeletedAt,
	)
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO wearer_members (wearer_id, user_id, role) VALUES ($1, $2, 'admin')`,
		w.ID, ownerUserID,
	)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return w, nil
}

func (r *PostgresWearerRepository) FindWearerByID(ctx context.Context, id string) (*Wearer, error) {
	w := &Wearer{}
	err := r.db.QueryRow(ctx,
		`SELECT id, owner_user_id, full_name, date_of_birth, photo_url, blood_type, medical_conditions, allergies, notes, created_at, deleted_at
		 FROM wearers
		 WHERE id = $1 AND deleted_at IS NULL`,
		id,
	).Scan(
		&w.ID, &w.OwnerUserID, &w.FullName,
		&w.DateOfBirth, &w.PhotoURL, &w.BloodType,
		&w.MedicalConditions, &w.Allergies, &w.Notes,
		&w.CreatedAt, &w.DeletedAt,
	)
	if err != nil {
		return nil, err
	}
	return w, nil
}

func (r *PostgresWearerRepository) UpdateWearer(ctx context.Context, id string, opts WearerUpdateOpts) (*Wearer, error) {
	// Build SET clause dynamically for only provided fields.
	setClauses := []string{}
	args := []interface{}{}
	argIdx := 1

	if opts.FullName != nil {
		setClauses = append(setClauses, "full_name = $"+itoa(argIdx))
		args = append(args, *opts.FullName)
		argIdx++
	}
	if opts.DateOfBirth != nil {
		setClauses = append(setClauses, "date_of_birth = $"+itoa(argIdx))
		args = append(args, *opts.DateOfBirth)
		argIdx++
	}
	if opts.BloodType != nil {
		setClauses = append(setClauses, "blood_type = $"+itoa(argIdx))
		args = append(args, *opts.BloodType)
		argIdx++
	}
	if opts.MedicalConditions != nil {
		setClauses = append(setClauses, "medical_conditions = $"+itoa(argIdx))
		args = append(args, opts.MedicalConditions)
		argIdx++
	}
	if opts.Allergies != nil {
		setClauses = append(setClauses, "allergies = $"+itoa(argIdx))
		args = append(args, opts.Allergies)
		argIdx++
	}
	if opts.Notes != nil {
		setClauses = append(setClauses, "notes = $"+itoa(argIdx))
		args = append(args, *opts.Notes)
		argIdx++
	}

	// If nothing to update, just fetch and return current state.
	if len(setClauses) == 0 {
		return r.FindWearerByID(ctx, id)
	}

	args = append(args, id)
	query := "UPDATE wearers SET " + joinStrings(setClauses, ", ") +
		" WHERE id = $" + itoa(argIdx) + " AND deleted_at IS NULL" +
		" RETURNING id, owner_user_id, full_name, date_of_birth, photo_url, blood_type, medical_conditions, allergies, notes, created_at, deleted_at"

	w := &Wearer{}
	err := r.db.QueryRow(ctx, query, args...).Scan(
		&w.ID, &w.OwnerUserID, &w.FullName,
		&w.DateOfBirth, &w.PhotoURL, &w.BloodType,
		&w.MedicalConditions, &w.Allergies, &w.Notes,
		&w.CreatedAt, &w.DeletedAt,
	)
	if err != nil {
		return nil, err
	}
	return w, nil
}

func (r *PostgresWearerRepository) IsWearerAdmin(ctx context.Context, wearerID, userID string) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM wearer_members
			WHERE wearer_id = $1 AND user_id = $2 AND role = 'admin'
		)`,
		wearerID, userID,
	).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func (r *PostgresWearerRepository) GetWearerMembership(ctx context.Context, wearerID, userID string) (*WearerMember, error) {
	m := &WearerMember{}
	err := r.db.QueryRow(ctx,
		`SELECT id, wearer_id, user_id, role, can_view_location, invited_at, accepted_at
		 FROM wearer_members
		 WHERE wearer_id = $1 AND user_id = $2`,
		wearerID, userID,
	).Scan(&m.ID, &m.WearerID, &m.UserID, &m.Role, &m.CanViewLocation, &m.InvitedAt, &m.AcceptedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return m, nil
}

func (r *PostgresWearerRepository) InviteMember(ctx context.Context, wearerID, userID, role string, canViewLocation bool) (*WearerMember, error) {
	m := &WearerMember{}
	err := r.db.QueryRow(ctx,
		`INSERT INTO wearer_members (wearer_id, user_id, role, can_view_location)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, wearer_id, user_id, role, can_view_location, invited_at, accepted_at`,
		wearerID, userID, role, canViewLocation,
	).Scan(&m.ID, &m.WearerID, &m.UserID, &m.Role, &m.CanViewLocation, &m.InvitedAt, &m.AcceptedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, errAlreadyMember
		}
		return nil, err
	}
	return m, nil
}

func (r *PostgresWearerRepository) ListMembers(ctx context.Context, wearerID string) ([]*WearerMemberWithUser, error) {
	rows, err := r.db.Query(ctx,
		`SELECT wm.id, wm.wearer_id, wm.user_id, wm.role, wm.can_view_location, wm.invited_at, wm.accepted_at,
		        u.id, u.email, u.full_name, u.phone
		 FROM wearer_members wm
		 JOIN users u ON u.id = wm.user_id
		 WHERE wm.wearer_id = $1
		 ORDER BY wm.invited_at`,
		wearerID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*WearerMemberWithUser
	for rows.Next() {
		m := &WearerMemberWithUser{
			User: &MemberUser{},
		}
		if err := rows.Scan(
			&m.WearerMember.ID, &m.WearerMember.WearerID, &m.WearerMember.UserID,
			&m.WearerMember.Role, &m.WearerMember.CanViewLocation,
			&m.WearerMember.InvitedAt, &m.WearerMember.AcceptedAt,
			&m.User.ID, &m.User.Email, &m.User.FullName, &m.User.Phone,
		); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

func (r *PostgresWearerRepository) UpdateMember(ctx context.Context, memberID, wearerID string, opts MemberUpdateOpts) (*WearerMember, error) {
	setClauses := []string{}
	args := []interface{}{}
	argIdx := 1

	if opts.Role != nil {
		setClauses = append(setClauses, "role = $"+itoa(argIdx))
		args = append(args, *opts.Role)
		argIdx++
	}
	if opts.CanViewLocation != nil {
		setClauses = append(setClauses, "can_view_location = $"+itoa(argIdx))
		args = append(args, *opts.CanViewLocation)
		argIdx++
	}

	if len(setClauses) == 0 {
		return r.FindMemberByID(ctx, memberID)
	}

	args = append(args, memberID, wearerID)
	query := "UPDATE wearer_members SET " + joinStrings(setClauses, ", ") +
		" WHERE id = $" + itoa(argIdx) + " AND wearer_id = $" + itoa(argIdx+1) +
		" RETURNING id, wearer_id, user_id, role, can_view_location, invited_at, accepted_at"

	m := &WearerMember{}
	err := r.db.QueryRow(ctx, query, args...).Scan(
		&m.ID, &m.WearerID, &m.UserID, &m.Role, &m.CanViewLocation, &m.InvitedAt, &m.AcceptedAt,
	)
	if err != nil {
		return nil, err
	}
	return m, nil
}

func (r *PostgresWearerRepository) FindMemberByID(ctx context.Context, memberID string) (*WearerMember, error) {
	m := &WearerMember{}
	err := r.db.QueryRow(ctx,
		`SELECT id, wearer_id, user_id, role, can_view_location, invited_at, accepted_at
		 FROM wearer_members
		 WHERE id = $1`,
		memberID,
	).Scan(&m.ID, &m.WearerID, &m.UserID, &m.Role, &m.CanViewLocation, &m.InvitedAt, &m.AcceptedAt)
	if err != nil {
		return nil, err
	}
	return m, nil
}

// --- helpers ---

func itoa(i int) string {
	// Simple integer-to-string for query parameter indices.
	const digits = "0123456789"
	if i < 10 {
		return string(digits[i])
	}
	return string(digits[i/10]) + string(digits[i%10])
}

func joinStrings(ss []string, sep string) string {
	if len(ss) == 0 {
		return ""
	}
	result := ss[0]
	for _, s := range ss[1:] {
		result += sep + s
	}
	return result
}
