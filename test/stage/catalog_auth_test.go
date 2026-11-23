//go:build stage

package stage_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStageHealthAndCatalog(t *testing.T) {
	code, body, _ := doJSON(t, http.MethodGet, "/healthz", nil, "")
	if code != http.StatusOK || body["status"] != "ok" {
		t.Fatalf("healthz %d %#v", code, body)
	}
	code, body, _ = doJSON(t, http.MethodGet, "/v1/agent", nil, "")
	if code != http.StatusOK {
		t.Fatalf("agent %d %#v", code, body)
	}
	code, body, _ = doJSON(t, http.MethodGet, "/v1/geo", nil, "")
	if code != http.StatusOK {
		t.Fatalf("geo %d %#v", code, body)
	}
	code, body, _ = doJSON(t, http.MethodGet, "/v1/domains/search?q=google.com", nil, "")
	if code != http.StatusOK {
		t.Fatalf("search %d %#v", code, body)
	}
	code, body, _ = doJSON(t, http.MethodGet, "/v1/domains/suggest?q=namepiq", nil, "")
	if code != http.StatusOK {
		t.Fatalf("suggest %d %#v", code, body)
	}
	code, body, _ = doJSON(t, http.MethodGet, "/v1/watches/top", nil, "")
	if code != http.StatusOK {
		t.Fatalf("watches/top %d %#v", code, body)
	}
	code, body, _ = doJSON(t, http.MethodGet, "/v1/watches/prices", nil, "")
	if code != http.StatusOK {
		t.Fatalf("watches/prices %d %#v", code, body)
	}
}

func TestStageAuthVerifyMailCapture(t *testing.T) {
	email := uniqueEmail("auth")
	token := registerVerified(t, email, "password12345")
	code, body, _ := doJSON(t, http.MethodGet, "/v1/me", nil, token)
	if code != http.StatusOK {
		t.Fatalf("me %d %#v", code, body)
	}
	user, _ := body["user"].(map[string]any)
	if user["email_verified"] != true {
		t.Fatalf("expected verified %#v", user)
	}
	code, body, _ = doJSON(t, http.MethodGet, "/v1/test/last-email?to="+email, nil, "")
	if code != http.StatusOK {
		t.Fatalf("last-email %d %#v", code, body)
	}
	tok, _ := body["verify_token"].(string)
	if tok == "" {
		t.Fatalf("missing verify_token %#v", body)
	}
}

func TestStageWebhookRefundDisputeSigned(t *testing.T) {
	if stageCfg.RazorpayWebhookSecret == "" {
		t.Skip("RAZORPAY_WEBHOOK_SECRET not set")
	}
	for _, evt := range []map[string]any{
		{
			"event": "refund.processed",
			"payload": map[string]any{
				"refund":  map[string]any{"entity": map[string]any{"id": "rfnd_e2e", "amount": 10000, "currency": "INR", "payment_id": "pay_e2e", "status": "processed"}},
				"payment": map[string]any{"entity": map[string]any{"id": "pay_e2e", "order_id": "order_e2e", "amount": 10000, "currency": "INR", "captured": true, "email": "e2e@test"}},
			},
		},
		{
			"event": "payment.dispute.created",
			"payload": map[string]any{
				"dispute": map[string]any{"entity": map[string]any{"id": "disp_e2e", "amount": 10000, "currency": "INR", "payment_id": "pay_e2e", "status": "open", "phase": "chargeback", "reason_code": "fraud"}},
				"payment": map[string]any{"entity": map[string]any{"id": "pay_e2e", "order_id": "order_e2e", "amount": 10000, "currency": "INR", "captured": true}},
			},
		},
	} {
		raw, _ := json.Marshal(evt)
		req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/razorpay", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Razorpay-Signature", signWebhook(raw))
		rr := httptest.NewRecorder()
		handler().ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("webhook %v -> %d %s", evt["event"], rr.Code, rr.Body.String())
		}
	}
}
