package api

import (
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/shahprincea/leo/backend/internal/auth"
	"github.com/shahprincea/leo/backend/internal/config"
)

// NewRouter creates and returns a chi router with all application routes registered.
func NewRouter(db *pgxpool.Pool, rdb *redis.Client, cfg *config.Config) chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)

	// Health (public)
	h := &HealthHandler{}
	r.Get("/health", h.Health)

	// Auth (public)
	authHandler := NewAuthHandler(db, rdb, cfg)
	r.Post("/auth/register", authHandler.Register)
	r.Post("/auth/login", authHandler.Login)
	r.Post("/auth/refresh", authHandler.Refresh)
	r.Post("/auth/device", authHandler.DeviceAuth)

	// Wearers (requires auth)
	wearerHandler := NewWearerHandler(db, rdb, cfg)
	r.Route("/wearers", func(r chi.Router) {
		r.Use(auth.RequireAuth(cfg.JWTSecret))
		r.Post("/", wearerHandler.Create)
		r.Get("/{wearerID}", wearerHandler.Get)
		r.Patch("/{wearerID}", wearerHandler.Update)
		r.Post("/{wearerID}/members", wearerHandler.InviteMember)
		r.Get("/{wearerID}/members", wearerHandler.ListMembers)
	})

	// Wearer member management (requires auth, outside /wearers group)
	r.With(auth.RequireAuth(cfg.JWTSecret)).Patch("/wearer-members/{memberID}", wearerHandler.UpdateMember)

	return r
}
