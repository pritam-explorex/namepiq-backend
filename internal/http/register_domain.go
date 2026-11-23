package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/namepiq/api/internal/opensrs"
	"github.com/namepiq/api/internal/store"
)

func (s *Server) handleRegisterDomain(w http.ResponseWriter, r *http.Request) {
	u, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	if u.EmailVerifiedAt == nil {
		writeError(w, http.StatusForbidden, "email_unverified", "verify your email before registering a domain")
		return
	}

	var req struct {
		Domain    string `json:"domain"`
		Years     int    `json:"years"`
		AutoRenew *bool  `json:"auto_renew"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Domain) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "domain is required")
		return
	}
	years := req.Years
	if years < 1 {
		years = 1
	}
	autoRenew := true
	if req.AutoRenew != nil {
		autoRenew = *req.AutoRenew
	}

	d, err := s.registerDomainForUser(r, u, req.Domain, years, autoRenew)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrConflict):
			writeError(w, http.StatusConflict, "domain_exists", "domain already in your account")
		case strings.Contains(err.Error(), "contact"):
			writeError(w, http.StatusBadRequest, "contact_required", err.Error())
		case strings.Contains(err.Error(), "OpenSRS not configured"):
			writeError(w, http.StatusServiceUnavailable, "opensrs_unavailable", err.Error())
		case strings.HasPrefix(err.Error(), "opensrs:"):
			writeError(w, http.StatusBadGateway, "opensrs_error", strings.TrimPrefix(err.Error(), "opensrs: "))
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		}
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"domain":  d,
		"message": "Domain registered",
	})
}

func (s *Server) registerDomainForUser(r *http.Request, u store.User, domainName string, years int, autoRenew bool) (store.Domain, error) {
	if u.EmailVerifiedAt == nil {
		return store.Domain{}, fmt.Errorf("verify your email before registering a domain")
	}
	if s.reg == nil || !s.reg.Configured() {
		return store.Domain{}, fmt.Errorf("OpenSRS not configured")
	}
	domainName = opensrs.NormalizeDomain(domainName)
	if years < 1 {
		years = 1
	}

	c, err := s.store.GetContact(r.Context(), u.ID)
	if err != nil || strings.TrimSpace(c.FirstName) == "" || strings.TrimSpace(c.Phone) == "" || strings.TrimSpace(c.Address1) == "" {
		return store.Domain{}, fmt.Errorf("save registrant contact before registering a domain")
	}
	email := c.Email
	if email == "" {
		email = u.Email
	}

	regUser := "np" + strings.ReplaceAll(u.ID.String()[:8], "-", "")
	regPass := fmt.Sprintf("Np%s!", u.ID.String()[:8])

	result, err := s.reg.Register(r.Context(), opensrs.RegisterRequest{
		Domain:    domainName,
		Years:     years,
		AutoRenew: autoRenew,
		Locked:    true,
		Username:  regUser,
		Password:  regPass,
		Contact: opensrs.ContactInfo{
			FirstName:    c.FirstName,
			LastName:     c.LastName,
			Organization: c.Organization,
			Email:        email,
			Phone:        c.Phone,
			Address1:     c.Address1,
			Address2:     c.Address2,
			City:         c.City,
			State:        c.State,
			PostalCode:   c.PostalCode,
			Country:      c.Country,
		},
	})
	if err != nil {
		return store.Domain{}, fmt.Errorf("opensrs: %w", err)
	}

	expires := time.Now().UTC().AddDate(years, 0, 0)
	var orderID *string
	if result.OrderID != "" {
		orderID = &result.OrderID
	}
	d, err := s.store.CreateDomain(r.Context(), store.Domain{
		UserID:         u.ID,
		Name:           domainName,
		OpenSRSOrderID: orderID,
		Status:         "active",
		Locked:         true,
		AutoRenew:      autoRenew,
		ExpiresAt:      &expires,
	})
	if err != nil {
		return store.Domain{}, err
	}

	uid := u.ID
	_ = s.store.InsertAudit(r.Context(), &uid, clientIP(r), "domain.register", map[string]any{
		"domain": domainName, "order_id": result.OrderID, "years": years,
	})
	return d, nil
}
