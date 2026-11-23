//go:build stage

package stage_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/namepiq/api/internal/config"
	"github.com/namepiq/api/internal/domain"
	"github.com/namepiq/api/internal/fx"
	httpapi "github.com/namepiq/api/internal/http"
	"github.com/namepiq/api/internal/mail"
	"github.com/namepiq/api/internal/notify"
	"github.com/namepiq/api/internal/opensrs"
	"github.com/namepiq/api/internal/razorpay"
	"github.com/namepiq/api/internal/store"
)

var (
	stageSrv    *httpapi.Server
	stageStore  *store.Store
	stageMail   *mail.Capture
	stagePay    *razorpay.Client
	stageCfg    config.Config
	verifyToken = regexp.MustCompile(`token=([A-Za-z0-9\-_]+)`)
)

func TestMain(m *testing.M) {
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	_ = godotenv.Load(filepath.Join(root, ".env"))
	_ = godotenv.Load(filepath.Join(root, ".env.stage"))
	_ = os.Setenv("E2E_MAIL_CAPTURE", "1")
	if os.Getenv("APP_ENV") == "" {
		_ = os.Setenv("APP_ENV", "stage")
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "stage config: %v\n", err)
		os.Exit(1)
	}
	cfg.E2EMailCapture = true
	if cfg.Env == "production" {
		fmt.Fprintln(os.Stderr, "refusing stage tests against production APP_ENV")
		os.Exit(1)
	}
	if cfg.OpenSRSUsername == "" || cfg.OpenSRSAPIKey == "" {
		fmt.Fprintln(os.Stderr, "skip stage suite: OPENSRS_USERNAME/API_KEY required")
		os.Exit(0)
	}
	if cfg.RazorpayKeyID == "" || cfg.RazorpayKeySecret == "" {
		fmt.Fprintln(os.Stderr, "skip stage suite: RAZORPAY_KEY_ID/SECRET required")
		os.Exit(0)
	}
	if !razorpay.New(cfg.RazorpayKeyID, cfg.RazorpayKeySecret, "").IsTestMode() {
		fmt.Fprintln(os.Stderr, "refusing stage suite with live Razorpay keys")
		os.Exit(1)
	}

	ctx := context.Background()
	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "stage store: %v\n", err)
		os.Exit(1)
	}
	mig := filepath.Join(root, "migrations")
	if err := st.Migrate(ctx, mig); err != nil {
		fmt.Fprintf(os.Stderr, "stage migrate: %v\n", err)
		os.Exit(1)
	}
	if _, err := st.PromoteAdminsByEmail(ctx, cfg.AdminEmails); err != nil {
		fmt.Fprintf(os.Stderr, "stage promote admins: %v\n", err)
		os.Exit(1)
	}

	capture := mail.NewCapture(mail.LogSender{})
	reg := opensrs.NewWithConcurrency(cfg.OpenSRSUsername, cfg.OpenSRSAPIKey, cfg.OpenSRSEndpoint, cfg.OpenSRSMaxConcurrent)
	rates := fx.NewClient(map[string]float64{"INR": cfg.USDINRRate, "USD": 1})
	search := domain.NewSearchService(reg, domain.Pricing{
		MarkupPercent:          cfg.RetailMarkupPercent,
		DisplayDiscountPercent: cfg.DisplayDiscountPercent,
		FallbackUSD:            map[string]float64{"INR": cfg.USDINRRate, "USD": 1},
	}, rates)
	pay := razorpay.New(cfg.RazorpayKeyID, cfg.RazorpayKeySecret, cfg.RazorpayWebhookSecret)

	srv := httpapi.NewServer(cfg, st, capture, search, reg, pay, rates, notify.Noop{})
	stageSrv, stageStore, stageMail, stagePay, stageCfg = srv, st, capture, pay, cfg
	code := m.Run()
	st.Close()
	os.Exit(code)
}

func handler() http.Handler { return stageSrv.Handler() }

func doJSON(t *testing.T, method, path string, body any, token string) (int, map[string]any, []byte) {
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
	handler().ServeHTTP(rr, req)
	raw := rr.Body.Bytes()
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return rr.Code, out, raw
}

func uniqueEmail(prefix string) string {
	return fmt.Sprintf("%s-%d@e2e.namepiq.test", prefix, time.Now().UnixNano())
}

func registerVerified(t *testing.T, email, password string) string {
	t.Helper()
	code, body, _ := doJSON(t, http.MethodPost, "/v1/auth/register", map[string]string{
		"email": email, "password": password,
	}, "")
	if code != http.StatusCreated {
		t.Fatalf("register %d %#v", code, body)
	}
	token, _ := body["access_token"].(string)
	msg, ok := stageMail.LastFor(email)
	if !ok {
		msg, ok = stageMail.Last()
	}
	if !ok {
		t.Fatal("no verification email captured")
	}
	m := verifyToken.FindStringSubmatch(msg.Text)
	if len(m) < 2 {
		m = verifyToken.FindStringSubmatch(msg.HTML)
	}
	if len(m) < 2 {
		t.Fatalf("no token in email: %q", msg.Text)
	}
	code, body, _ = doJSON(t, http.MethodGet, "/v1/auth/verify-email?token="+m[1], nil, "")
	if code != http.StatusOK {
		t.Fatalf("verify %d %#v", code, body)
	}
	return token
}

func upsertContact(t *testing.T, token string) {
	t.Helper()
	code, body, _ := doJSON(t, http.MethodPut, "/v1/me/contact", map[string]any{
		"first_name":   "E2E",
		"last_name":    "Tester",
		"organization": "Namepiq E2E",
		"phone":        "+919876543210",
		"address1":     "1 Test Street",
		"city":         "Pune",
		"state":        "MH",
		"postal_code": "411001",
		"country":      "IN",
	}, token)
	if code != http.StatusOK {
		t.Fatalf("contact %d %#v", code, body)
	}
}

func signWebhook(body []byte) string {
	mac := hmac.New(sha256.New, []byte(stageCfg.RazorpayWebhookSecret))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func signPayment(orderID, paymentID string) string {
	mac := hmac.New(sha256.New, []byte(stageCfg.RazorpayKeySecret))
	_, _ = mac.Write([]byte(orderID + "|" + paymentID))
	return hex.EncodeToString(mac.Sum(nil))
}
