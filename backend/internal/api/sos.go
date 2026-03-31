package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/shahprincea/leo/backend/internal/auth"
)

// ─── Domain models ────────────────────────────────────────────────────────────

// SOSEvent represents a wearer's SOS activation.
type SOSEvent struct {
	ID          string
	WearerID    string
	Status      string // "active" | "cancelled" | "resolved"
	TriggeredAt time.Time
	CancelledAt *time.Time
	ResolvedAt  *time.Time
}

// SOSSettings holds per-wearer SOS configuration.
type SOSSettings struct {
	WearerID string
	Auto911  bool
}

// ─── Caller interface (Twilio abstraction) ────────────────────────────────────

// Caller initiates a phone call to the given number.
// The real implementation uses Twilio; NoopCaller is the development stub.
type Caller interface {
	Call(ctx context.Context, to string) error
}

// NoopCaller is a no-op stub used in development and as a fallback.
type NoopCaller struct{}

func (NoopCaller) Call(_ context.Context, _ string) error { return nil }

// ─── Repository interface ─────────────────────────────────────────────────────

// SOSRepository abstracts DB access for SOS endpoints.
type SOSRepository interface {
	// GetOnCallContact returns the current on-call contact for the wearer
	// (the lowest-tier escalation contact, optionally filtered by on-call schedule).
	GetOnCallContact(ctx context.Context, wearerID string) (*ContactConfig, error)

	// GetContactByTier returns the escalation contact at the given tier, or nil if none.
	GetContactByTier(ctx context.Context, wearerID string, tier int) (*ContactConfig, error)

	// CreateSOSEvent inserts a new active SOS event and returns it.
	CreateSOSEvent(ctx context.Context, wearerID string) (*SOSEvent, error)

	// GetActiveSOSEvent returns the active SOS event with the given ID, or nil if
	// it doesn't exist or is no longer active.
	GetActiveSOSEvent(ctx context.Context, sosID string) (*SOSEvent, error)

	// CancelSOSEvent marks the SOS event as cancelled.
	CancelSOSEvent(ctx context.Context, sosID, wearerID string) error

	// LogEscalation records one call attempt in the escalation audit log.
	LogEscalation(ctx context.Context, sosID, contactPhone string, tier int) error

	// GetSOSSettings returns SOS settings for the wearer (auto_911 flag, etc.).
	GetSOSSettings(ctx context.Context, wearerID string) (*SOSSettings, error)
}

// ─── Handler ─────────────────────────────────────────────────────────────────

// SOSHandler handles the SOS trigger and cancel endpoints.
type SOSHandler struct {
	db     SOSRepository
	caller Caller
	rdb    *redis.Client // nil in tests that don't exercise escalation timers
}

// NewSOSHandler creates a SOSHandler backed by real Postgres and Redis.
func NewSOSHandler(db *pgxpool.Pool, rdb *redis.Client, caller Caller) *SOSHandler {
	return &SOSHandler{
		db:     NewPostgresSOSRepository(db),
		caller: caller,
		rdb:    rdb,
	}
}

// Trigger handles POST /sos.
//
// The watch sends this when the wearer presses the SOS button.  The handler
// determines the current on-call contact, initiates a call, stores the event,
// and schedules an escalation timer in Redis.
func (h *SOSHandler) Trigger(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	wearerID, ok := auth.WatchIDFromContext(ctx)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	contact, err := h.db.GetOnCallContact(ctx, wearerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if contact == nil {
		writeError(w, http.StatusUnprocessableEntity, "no escalation contacts configured")
		return
	}

	event, err := h.db.CreateSOSEvent(ctx, wearerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	_ = h.caller.Call(ctx, contact.Phone)
	_ = h.db.LogEscalation(ctx, event.ID, contact.Phone, contact.Tier)

	// Schedule escalation in Redis if a timeout is configured.
	if h.rdb != nil && contact.TimeoutSec > 0 {
		deadline := float64(time.Now().Add(time.Duration(contact.TimeoutSec) * time.Second).Unix())
		member := event.ID + ":" + wearerID + ":" + itoa(contact.Tier)
		h.rdb.ZAdd(ctx, "sos:escalations", redis.Z{Score: deadline, Member: member})
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"sos_id":          event.ID,
		"calling_contact": contact,
	})
}

// Cancel handles POST /sos/{id}/cancel.
//
// The wearer or companion app sends this when the wearer is safe ("I'm OK").
// Sets status to cancelled and removes the escalation timer from Redis.
func (h *SOSHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	wearerID, ok := auth.WatchIDFromContext(ctx)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	sosID := chi.URLParam(r, "id")
	if sosID == "" {
		writeError(w, http.StatusBadRequest, "sos id required")
		return
	}

	event, err := h.db.GetActiveSOSEvent(ctx, sosID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if event == nil || event.WearerID != wearerID {
		writeError(w, http.StatusNotFound, "sos event not found")
		return
	}

	if err := h.db.CancelSOSEvent(ctx, sosID, wearerID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Mark as cancelled in Redis so the escalation poller stops.
	if h.rdb != nil {
		h.rdb.Set(ctx, "sos:cancelled:"+sosID, "1", 24*time.Hour)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

// ─── Postgres repository ──────────────────────────────────────────────────────

// PostgresSOSRepository implements SOSRepository using pgxpool.
type PostgresSOSRepository struct {
	db *pgxpool.Pool
}

// NewPostgresSOSRepository creates a PostgresSOSRepository.
func NewPostgresSOSRepository(db *pgxpool.Pool) *PostgresSOSRepository {
	return &PostgresSOSRepository{db: db}
}

func (r *PostgresSOSRepository) GetOnCallContact(ctx context.Context, wearerID string) (*ContactConfig, error) {
	c := &ContactConfig{}
	err := r.db.QueryRow(ctx, `
		SELECT full_name, phone, tier, timeout_sec
		FROM emergency_contacts
		WHERE wearer_id = $1
		ORDER BY tier
		LIMIT 1`,
		wearerID,
	).Scan(&c.FullName, &c.Phone, &c.Tier, &c.TimeoutSec)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (r *PostgresSOSRepository) GetContactByTier(ctx context.Context, wearerID string, tier int) (*ContactConfig, error) {
	c := &ContactConfig{}
	err := r.db.QueryRow(ctx, `
		SELECT full_name, phone, tier, timeout_sec
		FROM emergency_contacts
		WHERE wearer_id = $1 AND tier = $2
		LIMIT 1`,
		wearerID, tier,
	).Scan(&c.FullName, &c.Phone, &c.Tier, &c.TimeoutSec)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (r *PostgresSOSRepository) CreateSOSEvent(ctx context.Context, wearerID string) (*SOSEvent, error) {
	ev := &SOSEvent{}
	err := r.db.QueryRow(ctx, `
		INSERT INTO sos_events (wearer_id)
		VALUES ($1)
		RETURNING id, wearer_id, status, triggered_at, cancelled_at, resolved_at`,
		wearerID,
	).Scan(&ev.ID, &ev.WearerID, &ev.Status, &ev.TriggeredAt, &ev.CancelledAt, &ev.ResolvedAt)
	if err != nil {
		return nil, err
	}
	return ev, nil
}

func (r *PostgresSOSRepository) GetActiveSOSEvent(ctx context.Context, sosID string) (*SOSEvent, error) {
	ev := &SOSEvent{}
	err := r.db.QueryRow(ctx, `
		SELECT id, wearer_id, status, triggered_at, cancelled_at, resolved_at
		FROM sos_events
		WHERE id = $1 AND status = 'active'`,
		sosID,
	).Scan(&ev.ID, &ev.WearerID, &ev.Status, &ev.TriggeredAt, &ev.CancelledAt, &ev.ResolvedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ev, nil
}

func (r *PostgresSOSRepository) CancelSOSEvent(ctx context.Context, sosID, wearerID string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE sos_events
		SET status = 'cancelled', cancelled_at = now()
		WHERE id = $1 AND wearer_id = $2 AND status = 'active'`,
		sosID, wearerID,
	)
	return err
}

func (r *PostgresSOSRepository) LogEscalation(ctx context.Context, sosID, contactPhone string, tier int) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO sos_escalation_log (sos_event_id, tier, contact_phone)
		VALUES ($1, $2, $3)`,
		sosID, tier, contactPhone,
	)
	return err
}

func (r *PostgresSOSRepository) GetSOSSettings(ctx context.Context, wearerID string) (*SOSSettings, error) {
	s := &SOSSettings{WearerID: wearerID}
	err := r.db.QueryRow(ctx, `
		SELECT auto_911 FROM sos_settings WHERE wearer_id = $1`,
		wearerID,
	).Scan(&s.Auto911)
	if err == pgx.ErrNoRows {
		return &SOSSettings{WearerID: wearerID, Auto911: false}, nil
	}
	if err != nil {
		return nil, err
	}
	return s, nil
}
