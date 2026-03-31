package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shahprincea/leo/backend/internal/auth"
)

// ─── Domain models ────────────────────────────────────────────────────────────

// FallEvent records a detected fall and its resolution.
type FallEvent struct {
	ID          string
	WearerID    string
	FallType    string // "hard" | "soft"
	Status      string // "detected" | "confirmed" | "false_alarm"
	SOSEventID  string
	DetectedAt  time.Time
	ResolvedAt  *time.Time
}

// confirmationWindowSec is the default countdown shown to the wearer after a
// fall is detected.  The watch uses this value from the config; the value
// returned here is the backend default (configurable via WatchConfig).
const confirmationWindowSec = 10

// ─── Repository interface ─────────────────────────────────────────────────────

// FallRepository abstracts DB access for fall endpoints.
type FallRepository interface {
	// CreateFallEvent inserts a new detected fall event and returns it.
	CreateFallEvent(ctx context.Context, wearerID, fallType string) (*FallEvent, error)

	// GetActiveFallEvent returns the fall event with the given ID if it is still
	// in "detected" state, or nil if it doesn't exist or has already resolved.
	GetActiveFallEvent(ctx context.Context, fallID string) (*FallEvent, error)

	// CancelFallEvent marks the fall as a false alarm ("user tapped I'm OK").
	CancelFallEvent(ctx context.Context, fallID, wearerID string) error

	// ConfirmFallEvent marks the fall as confirmed (SOS was triggered).
	ConfirmFallEvent(ctx context.Context, fallID string) error
}

// ─── Handler ─────────────────────────────────────────────────────────────────

// FallHandler handles fall detection event endpoints.
type FallHandler struct {
	db FallRepository
}

// NewFallHandler creates a FallHandler backed by real Postgres.
func NewFallHandler(db *pgxpool.Pool) *FallHandler {
	return &FallHandler{db: NewPostgresFallRepository(db)}
}

// Report handles POST /falls.
//
// The watch calls this immediately when a fall threshold is crossed, before
// showing the confirmation UI.  The response tells the watch how long to wait
// before auto-triggering SOS.
func (h *FallHandler) Report(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	wearerID, ok := auth.WatchIDFromContext(ctx)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req struct {
		FallType string `json:"fall_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.FallType != "hard" && req.FallType != "soft" {
		writeError(w, http.StatusBadRequest, "fall_type must be 'hard' or 'soft'")
		return
	}

	event, err := h.db.CreateFallEvent(ctx, wearerID, req.FallType)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"fall_id":                event.ID,
		"confirmation_window_sec": confirmationWindowSec,
	})
}

// Cancel handles POST /falls/{id}/cancel.
//
// The watch calls this when the wearer taps "I'm OK" during the countdown.
// The fall event is marked as a false alarm.
func (h *FallHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	wearerID, ok := auth.WatchIDFromContext(ctx)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	fallID := chi.URLParam(r, "id")
	if fallID == "" {
		writeError(w, http.StatusBadRequest, "fall id required")
		return
	}

	event, err := h.db.GetActiveFallEvent(ctx, fallID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if event == nil || event.WearerID != wearerID {
		writeError(w, http.StatusNotFound, "fall event not found")
		return
	}

	if err := h.db.CancelFallEvent(ctx, fallID, wearerID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "false_alarm"})
}

// ─── Postgres repository ──────────────────────────────────────────────────────

// PostgresFallRepository implements FallRepository using pgxpool.
type PostgresFallRepository struct {
	db *pgxpool.Pool
}

// NewPostgresFallRepository creates a PostgresFallRepository.
func NewPostgresFallRepository(db *pgxpool.Pool) *PostgresFallRepository {
	return &PostgresFallRepository{db: db}
}

func (r *PostgresFallRepository) CreateFallEvent(ctx context.Context, wearerID, fallType string) (*FallEvent, error) {
	ev := &FallEvent{}
	err := r.db.QueryRow(ctx, `
		INSERT INTO fall_events (wearer_id, fall_type)
		VALUES ($1, $2)
		RETURNING id, wearer_id, fall_type, status, COALESCE(sos_event_id::text, ''),
		          detected_at, resolved_at`,
		wearerID, fallType,
	).Scan(&ev.ID, &ev.WearerID, &ev.FallType, &ev.Status, &ev.SOSEventID,
		&ev.DetectedAt, &ev.ResolvedAt)
	if err != nil {
		return nil, err
	}
	return ev, nil
}

func (r *PostgresFallRepository) GetActiveFallEvent(ctx context.Context, fallID string) (*FallEvent, error) {
	ev := &FallEvent{}
	err := r.db.QueryRow(ctx, `
		SELECT id, wearer_id, fall_type, status, COALESCE(sos_event_id::text, ''),
		       detected_at, resolved_at
		FROM fall_events
		WHERE id = $1 AND status = 'detected'`,
		fallID,
	).Scan(&ev.ID, &ev.WearerID, &ev.FallType, &ev.Status, &ev.SOSEventID,
		&ev.DetectedAt, &ev.ResolvedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ev, nil
}

func (r *PostgresFallRepository) CancelFallEvent(ctx context.Context, fallID, wearerID string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE fall_events
		SET status = 'false_alarm', resolved_at = now()
		WHERE id = $1 AND wearer_id = $2 AND status = 'detected'`,
		fallID, wearerID,
	)
	return err
}

func (r *PostgresFallRepository) ConfirmFallEvent(ctx context.Context, fallID string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE fall_events
		SET status = 'confirmed', resolved_at = now()
		WHERE id = $1 AND status = 'detected'`,
		fallID,
	)
	return err
}
