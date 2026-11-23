package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/namepiq/api/internal/opensrs"
	"github.com/namepiq/api/internal/store"
)

func (s *Server) handleRenewDomain(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadOwnedDomain(w, r)
	if !ok {
		return
	}
	if s.reg == nil || !s.reg.Configured() {
		writeError(w, http.StatusServiceUnavailable, "opensrs_unavailable", "OpenSRS not configured")
		return
	}
	var req struct {
		Years     int   `json:"years"`
		AutoRenew *bool `json:"auto_renew"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Years < 1 {
		req.Years = 1
	}
	autoRenew := d.AutoRenew
	if req.AutoRenew != nil {
		autoRenew = *req.AutoRenew
	}
	res, err := s.reg.Renew(r.Context(), d.Name, req.Years, autoRenew)
	if err != nil {
		writeError(w, http.StatusBadGateway, "opensrs_error", err.Error())
		return
	}
	updated, err := s.store.UpdateDomainExpiry(r.Context(), d.UserID, d.ID, res.ExpiresAt, &autoRenew)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "renewed at OpenSRS but failed to update local record")
		return
	}
	uid := d.UserID
	_ = s.store.InsertAudit(r.Context(), &uid, clientIP(r), "domain.renew", map[string]any{
		"domain": d.Name, "years": req.Years, "order_id": res.OrderID,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"domain":  updated,
		"opensrs": res,
		"message": "Domain renewed",
	})
}

func (s *Server) handlePushDomainContacts(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadOwnedDomain(w, r)
	if !ok {
		return
	}
	if s.reg == nil || !s.reg.Configured() {
		writeError(w, http.StatusServiceUnavailable, "opensrs_unavailable", "OpenSRS not configured")
		return
	}
	u, _ := UserFromContext(r.Context())
	c, err := s.store.GetContact(r.Context(), u.ID)
	if err != nil || strings.TrimSpace(c.FirstName) == "" {
		writeError(w, http.StatusBadRequest, "contact_required", "save registrant contact first")
		return
	}
	email := c.Email
	if email == "" {
		email = u.Email
	}
	if err := s.reg.UpdateContacts(r.Context(), d.Name, opensrs.ContactInfo{
		FirstName: c.FirstName, LastName: c.LastName, Organization: c.Organization,
		Email: email, Phone: c.Phone, Address1: c.Address1, Address2: c.Address2,
		City: c.City, State: c.State, PostalCode: c.PostalCode, Country: c.Country,
	}); err != nil {
		writeError(w, http.StatusBadGateway, "opensrs_error", err.Error())
		return
	}
	uid := d.UserID
	_ = s.store.InsertAudit(r.Context(), &uid, clientIP(r), "domain.update_contacts", map[string]any{"domain": d.Name})
	writeJSON(w, http.StatusOK, map[string]any{"status": "updated", "domain": d.Name})
}

func (s *Server) handleNameSuggest(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "query parameter q is required")
		return
	}
	if s.reg == nil || !s.reg.Configured() {
		writeError(w, http.StatusServiceUnavailable, "opensrs_unavailable", "OpenSRS not configured")
		return
	}
	res, err := s.reg.NameSuggest(r.Context(), q, nil)
	if err != nil {
		writeError(w, http.StatusBadGateway, "opensrs_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"query":   q,
		"results": res,
	})
}

// syncOpenSRSFlags pushes lock / auto-renew changes to the registrar after local update.
func (s *Server) syncOpenSRSFlags(r *http.Request, d store.Domain, locked, autoRenew *bool) error {
	if s.reg == nil || !s.reg.Configured() {
		return nil
	}
	if locked != nil {
		if err := s.reg.SetDomainLock(r.Context(), d.Name, *locked); err != nil {
			return err
		}
	}
	if autoRenew != nil {
		if err := s.reg.SetAutoRenew(r.Context(), d.Name, *autoRenew); err != nil {
			return err
		}
	}
	return nil
}
