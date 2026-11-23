package httpapi

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/namepiq/api/internal/store"
)

func TestListDomainsEmptyAndOwned(t *testing.T) {
	srv, mem, _ := testServer(t)
	h := srv.Handler()
	ctx := context.Background()

	_, body := doJSON(t, h, http.MethodPost, "/v1/auth/register", map[string]string{
		"email": "dash@example.com", "password": "password123",
	}, "")
	token, _ := body["access_token"].(string)

	code, body := doJSON(t, h, http.MethodGet, "/v1/domains", nil, token)
	if code != http.StatusOK {
		t.Fatalf("%d %#v", code, body)
	}
	domains, _ := body["domains"].([]any)
	if len(domains) != 0 {
		t.Fatalf("expected empty, got %#v", domains)
	}

	u, err := mem.GetUserByEmail(ctx, "dash@example.com")
	if err != nil {
		t.Fatal(err)
	}
	d, err := mem.CreateDomain(ctx, store.Domain{
		UserID: u.ID,
		Name:   "example-dash.com",
		Status: "active",
		Locked: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	code, body = doJSON(t, h, http.MethodGet, "/v1/domains", nil, token)
	if code != http.StatusOK {
		t.Fatalf("%d %#v", code, body)
	}
	domains, _ = body["domains"].([]any)
	if len(domains) != 1 {
		t.Fatalf("expected 1 domain, got %#v", body)
	}

	code, body = doJSON(t, h, http.MethodGet, "/v1/domains/"+d.ID.String(), nil, token)
	if code != http.StatusOK {
		t.Fatalf("get %d %#v", code, body)
	}

	code, _ = doJSON(t, h, http.MethodGet, "/v1/domains/"+uuid.New().String(), nil, token)
	if code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", code)
	}

	code, body = doJSON(t, h, http.MethodPatch, "/v1/domains/"+d.ID.String(), map[string]any{
		"auto_renew": false,
	}, token)
	if code != http.StatusOK {
		t.Fatalf("patch %d %#v", code, body)
	}
}

func TestNameserversWithoutOpenSRS(t *testing.T) {
	srv, mem, _ := testServer(t)
	h := srv.Handler()
	ctx := context.Background()
	_, body := doJSON(t, h, http.MethodPost, "/v1/auth/register", map[string]string{
		"email": "ns@example.com", "password": "password123",
	}, "")
	token, _ := body["access_token"].(string)
	u, err := mem.GetUserByEmail(ctx, "ns@example.com")
	if err != nil {
		t.Fatal(err)
	}
	d, err := mem.CreateDomain(ctx, store.Domain{UserID: u.ID, Name: "ns-test.com", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}

	code, body := doJSON(t, h, http.MethodGet, "/v1/domains/"+d.ID.String()+"/nameservers", nil, token)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("expected opensrs unavailable, got %d %#v", code, body)
	}
}
