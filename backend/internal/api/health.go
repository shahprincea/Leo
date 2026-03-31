package api

import (
	"encoding/json"
	"net/http"
)

// healthResponse is the JSON payload returned by the health endpoint.
type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

// HealthHandler handles the health check endpoint.
type HealthHandler struct{}

// Health handles GET /health.
// Returns {"status":"ok","version":"0.1.0"} with HTTP 200.
func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(healthResponse{
		Status:  "ok",
		Version: "0.1.0",
	})
}
