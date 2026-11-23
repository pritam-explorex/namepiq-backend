package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/namepiq/api/internal/opensrs"
	"github.com/namepiq/api/internal/store"
)

const maxCommentLen = 500

func (s *Server) handleCommentList(w http.ResponseWriter, r *http.Request) {
	domainName := opensrs.NormalizeDomain(chi.URLParam(r, "domain"))
	if domainName == "" {
		writeError(w, http.StatusBadRequest, "invalid_domain", "domain required")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	list, err := s.store.ListCommentsByDomain(r.Context(), domainName, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "could not list comments")
		return
	}
	if list == nil {
		list = []store.DomainComment{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"comments": list, "domain": domainName})
}

func (s *Server) handleCommentCreate(w http.ResponseWriter, r *http.Request) {
	u, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	if u.EmailVerifiedAt == nil {
		writeError(w, http.StatusForbidden, "email_unverified", "verify your email before commenting")
		return
	}
	domainName := opensrs.NormalizeDomain(chi.URLParam(r, "domain"))
	if domainName == "" {
		writeError(w, http.StatusBadRequest, "invalid_domain", "domain required")
		return
	}
	since := time.Now().UTC().Add(-10 * time.Minute)
	n, _ := s.store.CountRecentCommentsByUser(r.Context(), u.ID, since)
	if n >= 5 {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many comments — try again shortly")
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	body := sanitizeCommentBody(req.Body)
	if body == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "comment body required")
		return
	}
	if utf8.RuneCountInString(body) > maxCommentLen {
		writeError(w, http.StatusBadRequest, "invalid_request", "comment max 500 characters")
		return
	}
	c, err := s.store.CreateComment(r.Context(), domainName, u.ID, body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "could not post comment")
		return
	}
	c.Author = displayHandleFromEmail(u.Email)
	writeJSON(w, http.StatusCreated, map[string]any{"comment": c})
}

func (s *Server) handleCommentDelete(w http.ResponseWriter, r *http.Request) {
	u, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid comment id")
		return
	}
	if err := s.store.SoftDeleteComment(r.Context(), id, u.ID, u.IsAdmin()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "comment not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "could not delete comment")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func sanitizeCommentBody(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "<", "")
	s = strings.ReplaceAll(s, ">", "")
	return s
}

func displayHandleFromEmail(email string) string {
	at := strings.IndexByte(email, '@')
	if at <= 0 {
		return "user"
	}
	local := email[:at]
	if len(local) > 16 {
		local = local[:16]
	}
	return local + "@…"
}
