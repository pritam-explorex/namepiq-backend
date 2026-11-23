package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/namepiq/api/internal/auth"
	"github.com/namepiq/api/internal/fx"
	"github.com/namepiq/api/internal/store"
	"github.com/namepiq/api/internal/validate"
)

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type verifyRequest struct {
	Token string `json:"token"`
}

type contactRequest struct {
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	Organization string `json:"organization"`
	Email        string `json:"email"`
	Phone        string `json:"phone"`
	Address1     string `json:"address1"`
	Address2     string `json:"address2"`
	City         string `json:"city"`
	State        string `json:"state"`
	PostalCode   string `json:"postal_code"`
	Country      string `json:"country"`
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	email := strings.TrimSpace(strings.ToLower(req.Email))
	if email == "" || !strings.Contains(email, "@") {
		writeError(w, http.StatusBadRequest, "invalid_request", "valid email is required")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	u, err := s.store.CreateUser(r.Context(), email, hash)
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "email_taken", "an account with this email already exists")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "could not create account")
		return
	}

	if err := s.sendVerification(r, u); err != nil {
		writeError(w, http.StatusInternalServerError, "email_failed", "account created but verification email failed: "+err.Error())
		return
	}

	uid := u.ID
	_ = s.store.InsertAudit(r.Context(), &uid, clientIP(r), "auth.register", map[string]any{"email": email})

	token, err := auth.NewAccessToken(s.cfg.JWTSecret, s.cfg.AccessTokenTTL, u.ID, u.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "could not issue token")
		return
	}
	setAuthCookie(w, token, s.cfg.AccessTokenTTL, s.cfg.Env == "production")
	writeJSON(w, http.StatusCreated, map[string]any{
		"user":         publicUser(u),
		"access_token": token,
		"message":      "Check your email to verify your account",
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	email := strings.TrimSpace(strings.ToLower(req.Email))
	u, err := s.store.GetUserByEmail(r.Context(), email)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid email or password")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "login failed")
		return
	}
	if !auth.CheckPassword(u.PasswordHash, req.Password) {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid email or password")
		return
	}

	token, err := auth.NewAccessToken(s.cfg.JWTSecret, s.cfg.AccessTokenTTL, u.ID, u.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "could not issue token")
		return
	}
	uid := u.ID
	_ = s.store.InsertAudit(r.Context(), &uid, clientIP(r), "auth.login", nil)
	setAuthCookie(w, token, s.cfg.AccessTokenTTL, s.cfg.Env == "production")
	writeJSON(w, http.StatusOK, map[string]any{
		"user":         publicUser(u),
		"access_token": token,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	clearAuthCookie(w, s.cfg.Env == "production")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleVerifyEmail(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		var req verifyRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		token = req.Token
	}
	if token == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "token is required")
		return
	}
	userID, err := s.store.ConsumeEmailVerification(r.Context(), auth.HashToken(token))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusBadRequest, "invalid_token", "verification link is invalid or expired")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "verification failed")
		return
	}
	_ = s.store.InsertAudit(r.Context(), &userID, clientIP(r), "auth.verify_email", nil)
	u, err := s.store.GetUserByID(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"status": "verified"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "verified",
		"user":   publicUser(u),
	})
}

func (s *Server) handleResendVerification(w http.ResponseWriter, r *http.Request) {
	u, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	if u.EmailVerifiedAt != nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "already_verified"})
		return
	}
	if err := s.sendVerification(r, u); err != nil {
		writeError(w, http.StatusInternalServerError, "email_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "sent"})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	c, _ := s.store.GetContact(r.Context(), u.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"user":    publicUser(u),
		"contact": c,
	})
}

func (s *Server) handlePatchMe(w http.ResponseWriter, r *http.Request) {
	u, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req struct {
		PreferredCurrency *string `json:"preferred_currency"`
		Country           *string `json:"country"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	currency := u.PreferredCurrency
	country := u.Country
	if req.PreferredCurrency != nil {
		c := strings.ToUpper(strings.TrimSpace(*req.PreferredCurrency))
		if c != "" && !fx.IsSupported(c) {
			writeError(w, http.StatusBadRequest, "invalid_request", "unsupported currency")
			return
		}
		currency = c
	}
	if req.Country != nil {
		raw := strings.TrimSpace(*req.Country)
		if raw == "" {
			country = ""
		} else {
			cc, err := validate.NormalizeCountry(raw)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
				return
			}
			country = cc
		}
	}
	updated, err := s.store.UpdateUserLocale(r.Context(), u.ID, currency, country)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": publicUser(updated)})
}

func (s *Server) handleUpsertContact(w http.ResponseWriter, r *http.Request) {
	u, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req contactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	country, err := validate.NormalizeCountry(req.Country)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	phone, err := validate.NormalizePhoneToE164(req.Phone, country)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "phone: "+err.Error())
		return
	}
	postal := validate.NormalizePostal(req.PostalCode, country)
	if err := validate.ValidatePostal(postal, country); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	c := store.Contact{
		UserID:       u.ID,
		FirstName:    strings.TrimSpace(req.FirstName),
		LastName:     strings.TrimSpace(req.LastName),
		Organization: strings.TrimSpace(req.Organization),
		Email:        strings.TrimSpace(req.Email),
		Phone:        phone,
		Address1:     strings.TrimSpace(req.Address1),
		Address2:     strings.TrimSpace(req.Address2),
		City:         strings.TrimSpace(req.City),
		State:        strings.TrimSpace(req.State),
		PostalCode:   postal,
		Country:      country,
	}
	if c.FirstName == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "first_name is required")
		return
	}
	if c.Address1 == "" || c.City == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "address1 and city are required")
		return
	}
	if c.Email == "" {
		c.Email = u.Email
	}
	if err := s.store.UpsertContact(r.Context(), c); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "could not save contact")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"contact": c})
}

func (s *Server) sendVerification(r *http.Request, u store.User) error {
	raw, hash, err := auth.NewVerificationToken()
	if err != nil {
		return err
	}
	expires := time.Now().UTC().Add(24 * time.Hour)
	if err := s.store.CreateEmailVerification(r.Context(), u.ID, hash, expires); err != nil {
		return err
	}
	link := fmt.Sprintf("%s/verify-email?token=%s", strings.TrimRight(s.cfg.AppBaseURL, "/"), raw)
	subject := "Verify your Namepiq account"
	text := fmt.Sprintf("Welcome to Namepiq.\n\nVerify your email:\n%s\n\nThis link expires in 24 hours.", link)
	html := fmt.Sprintf(`<p>Welcome to Namepiq.</p><p><a href="%s">Verify your email</a></p><p>This link expires in 24 hours.</p>`, link)
	return s.mailer.Send(r.Context(), u.Email, subject, html, text)
}

func publicUser(u store.User) map[string]any {
	role := u.Role
	if role == "" {
		role = store.RoleUser
	}
	ccy := u.PreferredCurrency
	if ccy == "" {
		ccy = "USD"
	}
	return map[string]any{
		"id":                  u.ID,
		"email":               u.Email,
		"role":                role,
		"preferred_currency":  ccy,
		"country":             u.Country,
		"email_verified":      u.EmailVerifiedAt != nil,
		"email_verified_at":   u.EmailVerifiedAt,
		"created_at":          u.CreatedAt,
	}
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	return strings.Split(r.RemoteAddr, ":")[0]
}

func setAuthCookie(w http.ResponseWriter, token string, ttl time.Duration, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     "namepiq_token",
		Value:    token,
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearAuthCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     "namepiq_token",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}