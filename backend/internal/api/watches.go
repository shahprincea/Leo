package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shahprincea/leo/backend/internal/auth"
)

// ─── Domain models ───────────────────────────────────────────────────────────

// Watch is the device registration record.
type Watch struct {
	ID           string
	WearerID     string
	DeviceID     string
	Model        string
	OSVersion    string
	Carrier      string
	IsSamsung    bool
	ConfigHash   *string
	LastSeenAt   *time.Time
	BatteryLevel *int
	RegisteredAt time.Time
}

// WatchConfig is the full remote configuration payload delivered to the watch.
type WatchConfig struct {
	ConfigHash         string               `json:"config_hash"`
	Geofences          []GeofenceConfig     `json:"geofences"`
	Medications        []MedicationConfig   `json:"medications"`
	EscalationContacts []ContactConfig      `json:"escalation_contacts"`
	PresetMessages     []PresetMessageConfig `json:"preset_messages"`
	EmergencyMedicalID EmergencyMedicalID   `json:"emergency_medical_id"`
	WellnessPromptTime string               `json:"wellness_prompt_time"` // "HH:MM"
	NightMode          NightModeConfig      `json:"night_mode"`
}

// GeofenceConfig is the watch-facing geofence representation.
type GeofenceConfig struct {
	ID        string  `json:"id"`
	Label     string  `json:"label"`
	CenterLat float64 `json:"center_lat"`
	CenterLng float64 `json:"center_lng"`
	RadiusM   int     `json:"radius_m"`
	SafeStart *string `json:"safe_start,omitempty"` // "HH:MM"
	SafeEnd   *string `json:"safe_end,omitempty"`
	Timezone  string  `json:"timezone"`
}

// MedicationConfig is the watch-facing medication + schedule.
type MedicationConfig struct {
	ID        string               `json:"id"`
	Name      string               `json:"name"`
	Dosage    string               `json:"dosage"`
	Schedules []MedScheduleConfig  `json:"schedules"`
}

// MedScheduleConfig is a single medication reminder time.
type MedScheduleConfig struct {
	ID         string  `json:"id"`
	TimeOfDay  string  `json:"time_of_day"` // "HH:MM"
	DaysOfWeek []int   `json:"days_of_week"` // nil = every day
}

// ContactConfig is the watch-facing emergency contact.
type ContactConfig struct {
	FullName   string `json:"full_name"`
	Phone      string `json:"phone"`
	Tier       int    `json:"tier"`
	TimeoutSec int    `json:"timeout_sec"`
}

// PresetMessageConfig is a configurable one-tap reply.
type PresetMessageConfig struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Position int    `json:"position"`
}

// EmergencyMedicalID is displayed on the watch lock screen.
type EmergencyMedicalID struct {
	FullName          string   `json:"full_name"`
	BloodType         *string  `json:"blood_type,omitempty"`
	MedicalConditions []string `json:"medical_conditions"`
	Allergies         []string `json:"allergies"`
	Notes             *string  `json:"notes,omitempty"`
}

// NightModeConfig defines quiet hours for the watch.
type NightModeConfig struct {
	Start    string `json:"start"`    // "HH:MM"
	End      string `json:"end"`      // "HH:MM"
	Timezone string `json:"timezone"`
}

// ─── Repository interface ─────────────────────────────────────────────────────

// WatchRepository abstracts DB access for watch endpoints.
type WatchRepository interface {
	// RegisterWatch upserts a watch record for the given wearer and returns it.
	// If a watch with the same device_id already exists it is updated.
	RegisterWatch(ctx context.Context, wearerID, deviceID, model, osVersion, carrier string) (*Watch, error)

	// FindWatchByWearerID returns the active watch for a wearer, or nil if none registered.
	FindWatchByWearerID(ctx context.Context, wearerID string) (*Watch, error)

	// UpdateConfigHash stores the hash of the last config delivered to the watch.
	UpdateConfigHash(ctx context.Context, watchID, hash string) error

	// GetWatchConfig assembles the full config payload for a wearer from all tables.
	GetWatchConfig(ctx context.Context, wearerID string) (*WatchConfig, error)
}

// ─── Handler ─────────────────────────────────────────────────────────────────

// WatchHandler handles watch registration and config endpoints.
type WatchHandler struct {
	db  WatchRepository
	hub Broadcaster
}

// NewWatchHandler creates a WatchHandler backed by real Postgres.
func NewWatchHandler(db *pgxpool.Pool, hub Broadcaster) *WatchHandler {
	return &WatchHandler{
		db:  NewPostgresWatchRepository(db),
		hub: hub,
	}
}

// watchResponse is the public JSON representation of a Watch.
type watchResponse struct {
	ID           string     `json:"id"`
	WearerID     string     `json:"wearer_id"`
	DeviceID     string     `json:"device_id"`
	Model        string     `json:"model"`
	OSVersion    string     `json:"os_version,omitempty"`
	Carrier      string     `json:"carrier,omitempty"`
	IsSamsung    bool       `json:"is_samsung"`
	RegisteredAt time.Time  `json:"registered_at"`
	LastSeenAt   *time.Time `json:"last_seen_at,omitempty"`
}

func toWatchResponse(w *Watch) watchResponse {
	return watchResponse{
		ID:           w.ID,
		WearerID:     w.WearerID,
		DeviceID:     w.DeviceID,
		Model:        w.Model,
		OSVersion:    w.OSVersion,
		Carrier:      w.Carrier,
		IsSamsung:    w.IsSamsung,
		RegisteredAt: w.RegisteredAt,
		LastSeenAt:   w.LastSeenAt,
	}
}

// Register handles POST /watches/register.
//
// The request must carry a device_token in Authorization: Bearer, issued by
// POST /auth/device.  Before watch registration the token maps to a wearerID.
func (h *WatchHandler) Register(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Device token carries the wearerID at this stage (pre-registration).
	wearerID, ok := auth.WatchIDFromContext(ctx)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req struct {
		DeviceID  string `json:"device_id"`
		Model     string `json:"model"`
		OSVersion string `json:"os_version"`
		Carrier   string `json:"carrier"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.DeviceID == "" {
		writeError(w, http.StatusBadRequest, "device_id is required")
		return
	}
	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "model is required")
		return
	}

	watch, err := h.db.RegisterWatch(ctx, wearerID, req.DeviceID, req.Model, req.OSVersion, req.Carrier)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Notify companion app clients subscribed to this wearer.
	h.hub.Broadcast(wearerID, map[string]any{
		"type":     "watch_online",
		"wearer_id": wearerID,
		"watch":    toWatchResponse(watch),
	})

	writeJSON(w, http.StatusCreated, map[string]any{"watch": toWatchResponse(watch)})
}

// Config handles GET /watches/config.
//
// The watch sends its current config hash via the If-None-Match header.
// If the hash is unchanged the handler returns 304 Not Modified, saving bandwidth.
// Otherwise it returns the full config payload and updates the stored hash.
func (h *WatchHandler) Config(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	wearerID, ok := auth.WatchIDFromContext(ctx)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	cfg, err := h.db.GetWatchConfig(ctx, wearerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// If the watch already has the current config, skip the download.
	if clientHash := r.Header.Get("If-None-Match"); clientHash != "" && clientHash == cfg.ConfigHash {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	// Persist the delivered hash so companion-app config updates can compare.
	watch, err := h.db.FindWatchByWearerID(ctx, wearerID)
	if err == nil && watch != nil {
		_ = h.db.UpdateConfigHash(ctx, watch.ID, cfg.ConfigHash)
	}

	writeJSON(w, http.StatusOK, cfg)
}

// ─── Postgres repository ──────────────────────────────────────────────────────

// PostgresWatchRepository implements WatchRepository using pgxpool.
type PostgresWatchRepository struct {
	db *pgxpool.Pool
}

// NewPostgresWatchRepository creates a PostgresWatchRepository.
func NewPostgresWatchRepository(db *pgxpool.Pool) *PostgresWatchRepository {
	return &PostgresWatchRepository{db: db}
}

func (r *PostgresWatchRepository) RegisterWatch(ctx context.Context, wearerID, deviceID, model, osVersion, carrier string) (*Watch, error) {
	isSamsung := isSamsungModel(model)
	now := time.Now()

	w := &Watch{}
	err := r.db.QueryRow(ctx, `
		INSERT INTO watches (wearer_id, device_id, model, os_version, carrier, is_samsung, last_seen_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (device_id) DO UPDATE SET
			wearer_id    = EXCLUDED.wearer_id,
			model        = EXCLUDED.model,
			os_version   = EXCLUDED.os_version,
			carrier      = EXCLUDED.carrier,
			is_samsung   = EXCLUDED.is_samsung,
			last_seen_at = EXCLUDED.last_seen_at,
			deactivated_at = NULL
		RETURNING id, wearer_id, device_id, model, os_version, carrier, is_samsung,
		          config_hash, last_seen_at, battery_level, registered_at`,
		wearerID, deviceID, model, osVersion, carrier, isSamsung, now,
	).Scan(
		&w.ID, &w.WearerID, &w.DeviceID, &w.Model, &w.OSVersion, &w.Carrier, &w.IsSamsung,
		&w.ConfigHash, &w.LastSeenAt, &w.BatteryLevel, &w.RegisteredAt,
	)
	if err != nil {
		return nil, err
	}
	return w, nil
}

func (r *PostgresWatchRepository) FindWatchByWearerID(ctx context.Context, wearerID string) (*Watch, error) {
	w := &Watch{}
	err := r.db.QueryRow(ctx, `
		SELECT id, wearer_id, device_id, model, os_version, carrier, is_samsung,
		       config_hash, last_seen_at, battery_level, registered_at
		FROM watches
		WHERE wearer_id = $1 AND deactivated_at IS NULL
		ORDER BY registered_at DESC
		LIMIT 1`,
		wearerID,
	).Scan(
		&w.ID, &w.WearerID, &w.DeviceID, &w.Model, &w.OSVersion, &w.Carrier, &w.IsSamsung,
		&w.ConfigHash, &w.LastSeenAt, &w.BatteryLevel, &w.RegisteredAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return w, nil
}

func (r *PostgresWatchRepository) UpdateConfigHash(ctx context.Context, watchID, hash string) error {
	_, err := r.db.Exec(ctx, `UPDATE watches SET config_hash = $1 WHERE id = $2`, hash, watchID)
	return err
}

// GetWatchConfig assembles the full config payload for a wearer from all tables.
func (r *PostgresWatchRepository) GetWatchConfig(ctx context.Context, wearerID string) (*WatchConfig, error) {
	cfg := &WatchConfig{}

	// Fetch all data concurrently via parallel queries in a single connection context.
	// Errors from any sub-query bubble up immediately.

	geofences, err := r.fetchGeofences(ctx, wearerID)
	if err != nil {
		return nil, err
	}
	cfg.Geofences = geofences

	medications, err := r.fetchMedications(ctx, wearerID)
	if err != nil {
		return nil, err
	}
	cfg.Medications = medications

	contacts, err := r.fetchEscalationContacts(ctx, wearerID)
	if err != nil {
		return nil, err
	}
	cfg.EscalationContacts = contacts

	presets, err := r.fetchPresetMessages(ctx, wearerID)
	if err != nil {
		return nil, err
	}
	cfg.PresetMessages = presets

	medID, err := r.fetchEmergencyMedicalID(ctx, wearerID)
	if err != nil {
		return nil, err
	}
	cfg.EmergencyMedicalID = *medID

	wellness, nightMode, err := r.fetchSettings(ctx, wearerID)
	if err != nil {
		return nil, err
	}
	cfg.WellnessPromptTime = wellness
	cfg.NightMode = nightMode

	// Compute hash over the config content (excluding the hash field itself).
	cfg.ConfigHash = computeConfigHash(cfg)

	return cfg, nil
}

// computeConfigHash returns a stable SHA-256 hex digest of the config payload.
// The ConfigHash field is zeroed before hashing to avoid circular dependency.
func computeConfigHash(cfg *WatchConfig) string {
	saved := cfg.ConfigHash
	cfg.ConfigHash = ""
	data, _ := json.Marshal(cfg)
	cfg.ConfigHash = saved
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}

func (r *PostgresWatchRepository) fetchGeofences(ctx context.Context, wearerID string) ([]GeofenceConfig, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, label, center_lat, center_lng, radius_m,
		       TO_CHAR(safe_start, 'HH24:MI'), TO_CHAR(safe_end, 'HH24:MI'), timezone
		FROM geofences
		WHERE wearer_id = $1 AND is_active = true`,
		wearerID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []GeofenceConfig
	for rows.Next() {
		g := GeofenceConfig{}
		if err := rows.Scan(
			&g.ID, &g.Label, &g.CenterLat, &g.CenterLng, &g.RadiusM,
			&g.SafeStart, &g.SafeEnd, &g.Timezone,
		); err != nil {
			return nil, err
		}
		result = append(result, g)
	}
	if result == nil {
		result = []GeofenceConfig{}
	}
	return result, rows.Err()
}

func (r *PostgresWatchRepository) fetchMedications(ctx context.Context, wearerID string) ([]MedicationConfig, error) {
	rows, err := r.db.Query(ctx, `
		SELECT m.id, m.name, m.dosage,
		       ms.id, TO_CHAR(ms.time_of_day, 'HH24:MI'), ms.days_of_week
		FROM medications m
		JOIN medication_schedules ms ON ms.medication_id = m.id
		WHERE m.wearer_id = $1 AND m.is_active = true
		ORDER BY m.id, ms.time_of_day`,
		wearerID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	medMap := map[string]*MedicationConfig{}
	var order []string

	for rows.Next() {
		var (
			medID, medName, medDosage string
			schedID, timeOfDay        string
			daysOfWeek                []int
		)
		if err := rows.Scan(&medID, &medName, &medDosage, &schedID, &timeOfDay, &daysOfWeek); err != nil {
			return nil, err
		}
		if _, seen := medMap[medID]; !seen {
			medMap[medID] = &MedicationConfig{ID: medID, Name: medName, Dosage: medDosage, Schedules: []MedScheduleConfig{}}
			order = append(order, medID)
		}
		medMap[medID].Schedules = append(medMap[medID].Schedules, MedScheduleConfig{
			ID:         schedID,
			TimeOfDay:  timeOfDay,
			DaysOfWeek: daysOfWeek,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	result := make([]MedicationConfig, 0, len(order))
	for _, id := range order {
		result = append(result, *medMap[id])
	}
	return result, nil
}

func (r *PostgresWatchRepository) fetchEscalationContacts(ctx context.Context, wearerID string) ([]ContactConfig, error) {
	rows, err := r.db.Query(ctx, `
		SELECT full_name, phone, tier, timeout_sec
		FROM emergency_contacts
		WHERE wearer_id = $1
		ORDER BY tier`,
		wearerID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ContactConfig
	for rows.Next() {
		c := ContactConfig{}
		if err := rows.Scan(&c.FullName, &c.Phone, &c.Tier, &c.TimeoutSec); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	if result == nil {
		result = []ContactConfig{}
	}
	return result, rows.Err()
}

func (r *PostgresWatchRepository) fetchPresetMessages(ctx context.Context, wearerID string) ([]PresetMessageConfig, error) {
	rows, err := r.db.Query(ctx, `
		SELECT key, label, position
		FROM preset_messages
		WHERE wearer_id = $1
		ORDER BY position`,
		wearerID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []PresetMessageConfig
	for rows.Next() {
		p := PresetMessageConfig{}
		if err := rows.Scan(&p.Key, &p.Label, &p.Position); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	if result == nil {
		result = []PresetMessageConfig{}
	}
	return result, rows.Err()
}

func (r *PostgresWatchRepository) fetchEmergencyMedicalID(ctx context.Context, wearerID string) (*EmergencyMedicalID, error) {
	id := &EmergencyMedicalID{}
	err := r.db.QueryRow(ctx, `
		SELECT full_name, blood_type, medical_conditions, allergies, notes
		FROM wearers
		WHERE id = $1 AND deleted_at IS NULL`,
		wearerID,
	).Scan(&id.FullName, &id.BloodType, &id.MedicalConditions, &id.Allergies, &id.Notes)
	if err != nil {
		return nil, err
	}
	if id.MedicalConditions == nil {
		id.MedicalConditions = []string{}
	}
	if id.Allergies == nil {
		id.Allergies = []string{}
	}
	return id, nil
}

// fetchSettings returns wellness prompt time and night mode config for the wearer.
// Returns safe defaults if no settings rows exist yet.
func (r *PostgresWatchRepository) fetchSettings(ctx context.Context, wearerID string) (wellnessPromptTime string, nightMode NightModeConfig, err error) {
	var promptTime, alertTime, wsTimezone string
	wellnessErr := r.db.QueryRow(ctx, `
		SELECT TO_CHAR(prompt_time, 'HH24:MI'), TO_CHAR(alert_time, 'HH24:MI'), timezone
		FROM wellness_settings
		WHERE wearer_id = $1`,
		wearerID,
	).Scan(&promptTime, &alertTime, &wsTimezone)
	if wellnessErr != nil && wellnessErr != pgx.ErrNoRows {
		err = wellnessErr
		return
	}
	if wellnessErr == pgx.ErrNoRows {
		promptTime = "08:00"
	}
	wellnessPromptTime = promptTime

	var nmStart, nmEnd, nmTimezone string
	nightErr := r.db.QueryRow(ctx, `
		SELECT TO_CHAR(night_mode_start, 'HH24:MI'), TO_CHAR(night_mode_end, 'HH24:MI'), timezone
		FROM watch_settings
		WHERE wearer_id = $1`,
		wearerID,
	).Scan(&nmStart, &nmEnd, &nmTimezone)
	if nightErr != nil && nightErr != pgx.ErrNoRows {
		err = nightErr
		return
	}
	if nightErr == pgx.ErrNoRows {
		nmStart, nmEnd, nmTimezone = "22:00", "07:00", "UTC"
	}
	nightMode = NightModeConfig{Start: nmStart, End: nmEnd, Timezone: nmTimezone}
	return
}

// isSamsungModel returns true when the model string identifies a Samsung Galaxy Watch.
func isSamsungModel(model string) bool {
	for i := 0; i+7 <= len(model); i++ {
		if model[i:i+7] == "samsung" {
			return true
		}
	}
	return false
}
