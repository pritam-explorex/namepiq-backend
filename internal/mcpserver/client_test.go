package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientSearchAndLogin(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/domains/search", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "acme" {
			t.Fatalf("q=%s", r.URL.Query().Get("q"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
	})
	mux.HandleFunc("/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok-123", "user": map[string]any{}})
	})
	mux.HandleFunc("/v1/me", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok-123" {
			http.Error(w, "unauthorized", 401)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"email": "a@b.com"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL, "")
	raw, err := c.Search(context.Background(), "acme", "USD")
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(raw) {
		t.Fatal("invalid json")
	}

	if _, err := c.Login(context.Background(), "a@b.com", "x"); err != nil {
		t.Fatal(err)
	}
	if c.Token() != "tok-123" {
		t.Fatalf("token=%q", c.Token())
	}
	if _, err := c.Me(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestNewServerRegistersTools(t *testing.T) {
	s := New(NewClient("http://example.invalid", ""))
	if s == nil {
		t.Fatal("nil server")
	}
}
