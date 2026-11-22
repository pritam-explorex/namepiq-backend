package httpapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/namepiq/api/internal/config"
	"github.com/namepiq/api/internal/store"
)

type Server struct {
	cfg   config.Config
	store *store.Store
	mux   *chi.Mux
}

func NewServer(cfg config.Config, st *store.Store) *Server {
	s := &Server{cfg: cfg, store: st, mux: chi.NewRouter()}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) routes() {
	r := s.mux
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   s.cfg.CORSOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Get("/healthz", s.handleHealth)

	r.Route("/v1", func(r chi.Router) {
		r.Get("/domains/search", s.handleDomainSearch)

		r.Route("/auth", func(r chi.Router) {
			r.Post("/register", s.handleRegister)
			r.Post("/login", s.handleLogin)
			r.Post("/logout", s.handleLogout)
			r.Post("/verify-email", s.handleVerifyEmail)
		})

		r.Get("/me", s.handleMe)
		r.Put("/me/contact", s.handleUpsertContact)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db := "ok"
	if err := s.store.Pool.Ping(ctx); err != nil {
		db = "error"
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "degraded",
			"db":     db,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": "namepiq-api",
		"db":      db,
	})
}

// Placeholders until auth/domain packages are fully wired.
func (s *Server) handleDomainSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "query parameter q is required")
		return
	}
	// Stub response — OpenSRS adapter comes next.
	writeJSON(w, http.StatusOK, map[string]any{
		"query": q,
		"results": []map[string]any{
			{
				"domain":           q,
				"available":        nil,
				"price_paise":      nil,
				"currency":         "INR",
				"provider_status":  "not_configured",
				"message":          "OpenSRS search not wired yet",
			},
		},
	})
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "not_implemented", "auth register coming next")
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "not_implemented", "auth login coming next")
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleVerifyEmail(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "not_implemented", "email verify coming next")
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
}

func (s *Server) handleUpsertContact(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
}
