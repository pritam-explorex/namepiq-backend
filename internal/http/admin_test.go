package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/namepiq/api/internal/auth"
	"github.com/namepiq/api/internal/config"
	"github.com/namepiq/api/internal/domain"
	"github.com/namepiq/api/internal/mail"
	"github.com/namepiq/api/internal/store"
)

func TestAdminRequiresRole(t *testing.T) {
	mem := store.NewMemory()
	cfg := config.Config{JWTSecret: "test-secret", AccessTokenTTL: time.Hour, CORSOrigins: []string{"*"}}
	srv := NewServer(cfg, mem, mail.LogSender{}, domain.NewSearchService(nil, domain.Pricing{MarkupPercent: 30}, nil), nil, nil, nil, nil)

	ctx := context.Background()
	u, err := mem.CreateUser(ctx, "user@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	tok, err := auth.NewAccessToken(cfg.JWTSecret, cfg.AccessTokenTTL, u.ID, u.Email)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/stats", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	admin, err := mem.CreateUser(ctx, "admin@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mem.PatchUserAdmin(ctx, admin.ID, ptr("admin"), true); err != nil {
		t.Fatal(err)
	}
	atok, err := auth.NewAccessToken(cfg.JWTSecret, cfg.AccessTokenTTL, admin.ID, admin.Email)
	if err != nil {
		t.Fatal(err)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/admin/stats", nil)
	req.Header.Set("Authorization", "Bearer "+atok)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/admin/users", nil)
	req.Header.Set("Authorization", "Bearer "+atok)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("users status=%d", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	users, _ := body["users"].([]any)
	if len(users) < 2 {
		t.Fatalf("expected users, got %#v", body)
	}

	// promote regular user via admin patch
	payload, _ := json.Marshal(map[string]any{"role": "admin", "force_email_verify": true})
	req = httptest.NewRequest(http.MethodPatch, "/v1/admin/users/"+u.ID.String(), bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+atok)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func ptr[T any](v T) *T { return &v }
