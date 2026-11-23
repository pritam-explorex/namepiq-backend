package mail

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResendSendSuccess(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"email_123"}`))
	}))
	defer srv.Close()

	// Monkey-patch by using custom client transport through temporary replacement:
	// Resend hardcodes API URL, so we test via a thin wrapper by temporarily
	// overriding through a local transport redirect isn't possible without refactor.
	// Instead, validate LogSender and error path; add sendVia for testability.
	s := NewResend("re_test", "Namepiq <noreply@namepiq.com>")
	s.client = srv.Client()
	s.apiURL = srv.URL // needs field — add below

	if err := s.Send(context.Background(), "user@example.com", "Hi", "<p>Hi</p>", "Hi"); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer re_test" {
		t.Fatalf("auth header: %q", gotAuth)
	}
	if gotBody["from"] != "Namepiq <noreply@namepiq.com>" {
		t.Fatalf("from: %#v", gotBody["from"])
	}
	to, _ := gotBody["to"].([]any)
	if len(to) != 1 || to[0] != "user@example.com" {
		t.Fatalf("to: %#v", gotBody["to"])
	}
}

func TestResendSendError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"invalid api key"}`))
	}))
	defer srv.Close()

	s := NewResend("bad", "from@namepiq.com")
	s.client = srv.Client()
	s.apiURL = srv.URL
	err := s.Send(context.Background(), "a@b.com", "x", "h", "t")
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected 401 error, got %v", err)
	}
}

func TestResendMissingKey(t *testing.T) {
	s := NewResend("", "from@namepiq.com")
	if err := s.Send(context.Background(), "a@b.com", "x", "h", "t"); err == nil {
		t.Fatal("expected missing key error")
	}
}

func TestLogSender(t *testing.T) {
	if err := (LogSender{}).Send(context.Background(), "a@b.com", "subj", "h", "t"); err != nil {
		t.Fatal(err)
	}
}
