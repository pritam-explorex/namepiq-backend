package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/namepiq/api/internal/opensrs"
	"github.com/namepiq/api/internal/store"
)

func (s *Server) handleListDomains(w http.ResponseWriter, r *http.Request) {
	u, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	domains, err := s.store.ListDomainsByUser(r.Context(), u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "could not list domains")
		return
	}
	if domains == nil {
		domains = []store.Domain{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"domains": domains})
}

func (s *Server) handleGetDomain(w http.ResponseWriter, r *http.Request) {
	u, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid domain id")
		return
	}
	d, err := s.store.GetDomainForUser(r.Context(), u.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "domain not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "could not load domain")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"domain": d})
}

func (s *Server) handlePatchDomain(w http.ResponseWriter, r *http.Request) {
	u, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid domain id")
		return
	}
	var req struct {
		Locked    *bool `json:"locked"`
		AutoRenew *bool `json:"auto_renew"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	d, err := s.store.UpdateDomainFlags(r.Context(), u.ID, id, req.Locked, req.AutoRenew)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "domain not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "could not update domain")
		return
	}
	if err := s.syncOpenSRSFlags(r, d, req.Locked, req.AutoRenew); err != nil {
		writeError(w, http.StatusBadGateway, "opensrs_error", "saved locally but OpenSRS sync failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"domain": d})
}

func (s *Server) handleGetNameservers(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadOwnedDomain(w, r)
	if !ok {
		return
	}
	if s.reg == nil || !s.reg.Configured() {
		writeError(w, http.StatusServiceUnavailable, "opensrs_unavailable", "OpenSRS not configured")
		return
	}
	ns, err := s.reg.GetNameservers(r.Context(), d.Name)
	if err != nil {
		writeError(w, http.StatusBadGateway, "opensrs_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"nameservers": ns})
}

func (s *Server) handlePutNameservers(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadOwnedDomain(w, r)
	if !ok {
		return
	}
	if s.reg == nil || !s.reg.Configured() {
		writeError(w, http.StatusServiceUnavailable, "opensrs_unavailable", "OpenSRS not configured")
		return
	}
	var req struct {
		Nameservers []string `json:"nameservers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Nameservers) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "nameservers array required")
		return
	}
	if err := s.reg.SetNameservers(r.Context(), d.Name, req.Nameservers); err != nil {
		writeError(w, http.StatusBadGateway, "opensrs_error", err.Error())
		return
	}
	uid := d.UserID
	_ = s.store.InsertAudit(r.Context(), &uid, clientIP(r), "domain.set_nameservers", map[string]any{
		"domain": d.Name, "nameservers": req.Nameservers,
	})
	writeJSON(w, http.StatusOK, map[string]any{"nameservers": req.Nameservers})
}

func (s *Server) handleGetDNS(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadOwnedDomain(w, r)
	if !ok {
		return
	}
	if s.reg == nil || !s.reg.Configured() {
		writeError(w, http.StatusServiceUnavailable, "opensrs_unavailable", "OpenSRS not configured")
		return
	}
	zone, err := s.reg.GetDNSZone(r.Context(), d.Name)
	if err != nil {
		writeError(w, http.StatusBadGateway, "opensrs_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"records":         zone.Records,
		"nameservers_ok":  zone.NameserversOK,
		"enabled":         zone.Enabled,
	})
}

func (s *Server) handlePutDNS(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadOwnedDomain(w, r)
	if !ok {
		return
	}
	if s.reg == nil || !s.reg.Configured() {
		writeError(w, http.StatusServiceUnavailable, "opensrs_unavailable", "OpenSRS not configured")
		return
	}
	var req struct {
		Records []opensrs.DNSRecord `json:"records"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	zone, err := s.reg.SetDNSZone(r.Context(), d.Name, req.Records)
	if err != nil {
		writeError(w, http.StatusBadGateway, "opensrs_error", err.Error())
		return
	}
	uid := d.UserID
	_ = s.store.InsertAudit(r.Context(), &uid, clientIP(r), "domain.set_dns", map[string]any{"domain": d.Name})
	writeJSON(w, http.StatusOK, map[string]any{"records": zone.Records})
}

func (s *Server) loadOwnedDomain(w http.ResponseWriter, r *http.Request) (store.Domain, bool) {
	u, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return store.Domain{}, false
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid domain id")
		return store.Domain{}, false
	}
	d, err := s.store.GetDomainForUser(r.Context(), u.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "domain not found")
		return store.Domain{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "could not load domain")
		return store.Domain{}, false
	}
	return d, true
}
