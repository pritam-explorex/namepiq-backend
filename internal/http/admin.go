package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/namepiq/api/internal/store"
)

func (s *Server) handleAdminStats(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.AdminStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleAdminListUsers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	limit, offset := parsePage(r)
	users, err := s.store.ListUsersAdmin(r.Context(), q, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if users == nil {
		users = []store.AdminUserRow{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func (s *Server) handleAdminPatchUser(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid user id")
		return
	}
	var req struct {
		Role              *string `json:"role"`
		ForceEmailVerify  *bool   `json:"force_email_verify"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	force := req.ForceEmailVerify != nil && *req.ForceEmailVerify
	if req.Role == nil && !force {
		writeError(w, http.StatusBadRequest, "invalid_request", "provide role and/or force_email_verify")
		return
	}
	u, err := s.store.PatchUserAdmin(r.Context(), id, req.Role, force)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "user not found")
			return
		}
		if strings.Contains(err.Error(), "invalid role") {
			writeError(w, http.StatusBadRequest, "invalid_request", "role must be user or admin")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	actor, _ := UserFromContext(r.Context())
	uid := actor.ID
	_ = s.store.InsertAudit(r.Context(), &uid, clientIP(r), "admin.patch_user", map[string]any{
		"target_user_id": id.String(),
		"role":           u.Role,
		"force_verify":   force,
	})
	writeJSON(w, http.StatusOK, map[string]any{"user": publicUser(u)})
}

func (s *Server) handleAdminListDomains(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	status := r.URL.Query().Get("status")
	limit, offset := parsePage(r)
	domains, err := s.store.ListDomainsAdmin(r.Context(), q, status, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if domains == nil {
		domains = []store.AdminDomainRow{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"domains": domains})
}

func (s *Server) handleAdminListOrders(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	limit, offset := parsePage(r)
	orders, err := s.store.ListOrdersAdmin(r.Context(), status, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if orders == nil {
		orders = []store.Order{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"orders": orders})
}

func (s *Server) handleAdminListAudit(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePage(r)
	events, err := s.store.ListAuditAdmin(r.Context(), limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if events == nil {
		events = []store.AuditEvent{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func parsePage(r *http.Request) (limit, offset int) {
	limit = 50
	offset = 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			offset = n
		}
	}
	return limit, offset
}
