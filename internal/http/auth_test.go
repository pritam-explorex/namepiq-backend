package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/namepiq/api/internal/config"
	"github.com/namepiq/api/internal/domain"
	"github.com/namepiq/api/internal/store"
)

type recordingMailer struct {
	texts []string
}

func (m *recordingMailer) Send(ctx context.Context, to, subject, html, text string) error {
	m.texts = append(m.texts, text)
	return nil
}

func (m *recordingMailer) lastToken(t *testing.T) string {
	t.Helper()
	if len(m.texts) == 0 {
		t.Fatal("no emails sent")
	}
	re := regexp.MustCompile(`token=([A-Za-z0-9\-_]+)`)
	match := re.FindStringSubmatch(m.texts[len(m.texts)-1])
	if len(match) < 2 {
		t.Fatalf("token not found in email: %q", m.texts[len(m.texts)-1])
	}
	return match[1]
}

func testServer(t *testing.T) (*Server, *store.Memory, *recordingMailer) {
	t.Helper()
	cfg := config.Config{
		Env:            "test",
		CORSOrigins:    []string{"http://localhost:3000"},
		JWTSecret:      "test-jwt-secret",
		AccessTokenTTL: time.Hour,
		AppBaseURL:     "http://localhost:3000",
	}
	mem := store.NewMemory()
	mailer := &recordingMailer{}
	search := domain.NewSearchService(nil, domain.Pricing{MarkupPercent: 30, FallbackUSD: map[string]float64{"INR": 84}}, nil)
	return NewServer(cfg, mem, mailer, search, nil, nil, nil, nil), mem, mailer
}

func doJSON(t *testing.T, h http.Handler, method, path string, body any, token string) (int, map[string]any) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rdr = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	var out map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &out)
	return rr.Code, out
}

func TestHealthz(t *testing.T) {
	srv, _, _ := testServer(t)
	code, body := doJSON(t, srv.Handler(), http.MethodGet, "/healthz", nil, "")
	if code != http.StatusOK {
		t.Fatalf("status %d body %#v", code, body)
	}
	if body["status"] != "ok" {
		t.Fatalf("body %#v", body)
	}
}

func TestRegisterLoginVerifyMeContact(t *testing.T) {
	srv, mem, mailer := testServer(t)
	h := srv.Handler()

	code, body := doJSON(t, h, http.MethodPost, "/v1/auth/register", map[string]string{
		"email":    "Ada@Example.com",
		"password": "password123",
	}, "")
	if code != http.StatusCreated {
		t.Fatalf("register status %d %#v", code, body)
	}
	token, _ := body["access_token"].(string)
	if token == "" {
		t.Fatal("missing access token")
	}
	user, _ := body["user"].(map[string]any)
	if user["email"] != "ada@example.com" {
		t.Fatalf("email not normalized: %#v", user)
	}
	if user["email_verified"] != false {
		t.Fatal("expected unverified")
	}

	code, _ = doJSON(t, h, http.MethodPost, "/v1/auth/register", map[string]string{
		"email":    "ada@example.com",
		"password": "password123",
	}, "")
	if code != http.StatusConflict {
		t.Fatalf("expected conflict, got %d", code)
	}

	verifyToken := mailer.lastToken(t)
	code, body = doJSON(t, h, http.MethodGet, "/v1/auth/verify-email?token="+verifyToken, nil, "")
	if code != http.StatusOK || body["status"] != "verified" {
		t.Fatalf("verify: %d %#v", code, body)
	}

	code, body = doJSON(t, h, http.MethodPost, "/v1/auth/login", map[string]string{
		"email":    "ada@example.com",
		"password": "password123",
	}, "")
	if code != http.StatusOK {
		t.Fatalf("login %d %#v", code, body)
	}
	token, _ = body["access_token"].(string)

	code, body = doJSON(t, h, http.MethodGet, "/v1/me", nil, token)
	if code != http.StatusOK {
		t.Fatalf("me %d %#v", code, body)
	}
	user, _ = body["user"].(map[string]any)
	if user["email_verified"] != true {
		t.Fatalf("expected verified after link: %#v", user)
	}

	code, body = doJSON(t, h, http.MethodPut, "/v1/me/contact", map[string]string{
		"first_name": "Ada",
		"last_name":  "Lovelace",
		"country":    "IN",
		"city":       "Pune",
	}, token)
	if code != http.StatusOK {
		t.Fatalf("contact %d %#v", code, body)
	}

	actions := mem.AuditActions()
	joined := strings.Join(actions, ",")
	for _, want := range []string{"auth.register", "auth.verify_email", "auth.login"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing audit %s in %v", want, actions)
		}
	}
}

func TestLoginInvalidCredentials(t *testing.T) {
	srv, _, _ := testServer(t)
	h := srv.Handler()
	_, _ = doJSON(t, h, http.MethodPost, "/v1/auth/register", map[string]string{
		"email": "x@y.com", "password": "password123",
	}, "")

	code, _ := doJSON(t, h, http.MethodPost, "/v1/auth/login", map[string]string{
		"email": "x@y.com", "password": "wrong-password",
	}, "")
	if code != http.StatusUnauthorized {
		t.Fatalf("got %d", code)
	}
}

func TestMeUnauthorized(t *testing.T) {
	srv, _, _ := testServer(t)
	code, _ := doJSON(t, srv.Handler(), http.MethodGet, "/v1/me", nil, "")
	if code != http.StatusUnauthorized {
		t.Fatalf("got %d", code)
	}
}

func TestDomainSearchValidation(t *testing.T) {
	srv, _, _ := testServer(t)
	code, _ := doJSON(t, srv.Handler(), http.MethodGet, "/v1/domains/search", nil, "")
	if code != http.StatusBadRequest {
		t.Fatalf("got %d", code)
	}
	code, body := doJSON(t, srv.Handler(), http.MethodGet, "/v1/domains/search?q=example.com", nil, "")
	if code != http.StatusOK {
		t.Fatalf("got %d %#v", code, body)
	}
}

func TestRegisterValidation(t *testing.T) {
	srv, _, _ := testServer(t)
	h := srv.Handler()
	code, _ := doJSON(t, h, http.MethodPost, "/v1/auth/register", map[string]string{
		"email": "bad", "password": "password123",
	}, "")
	if code != http.StatusBadRequest {
		t.Fatalf("bad email got %d", code)
	}
	code, _ = doJSON(t, h, http.MethodPost, "/v1/auth/register", map[string]string{
		"email": "ok@x.com", "password": "short",
	}, "")
	if code != http.StatusBadRequest {
		t.Fatalf("short password got %d", code)
	}
}
