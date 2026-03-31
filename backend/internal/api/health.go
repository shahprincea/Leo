package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// healthResponse is the JSON payload returned by the health endpoint.
type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

// NewRouter creates and returns a chi router with all application routes registered.
func NewRouter() *chi.Mux {
	r := chi.NewRouter()
	r.Get("/health", HealthHandler)
	return r
}

// HealthHandler handles GET /health.
// Returns {"status":"ok","version":"0.1.0"} with HTTP 200.
func HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(healthResponse{
		Status:  "ok",
		Version: "0.1.0",
	})
}
