package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/namepiq/api/internal/auth"
	"github.com/namepiq/api/internal/store"
)

type ctxKey int

const userCtxKey ctxKey = 1

func UserFromContext(ctx context.Context) (store.User, bool) {
	u, ok := ctx.Value(userCtxKey).(store.User)
	return u, ok
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			if c, err := r.Cookie("namepiq_token"); err == nil {
				token = c.Value
			}
		}
		if token == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}
		userID, _, err := auth.ParseAccessToken(s.cfg.JWTSecret, token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or expired token")
			return
		}
		u, err := s.store.GetUserByID(r.Context(), userID)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "user not found")
			return
		}
		ctx := context.WithValue(r.Context(), userCtxKey, u)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := UserFromContext(r.Context())
		if !ok || !u.IsAdmin() {
			writeError(w, http.StatusForbidden, "forbidden", "admin access required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	parts := strings.SplitN(h, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}
