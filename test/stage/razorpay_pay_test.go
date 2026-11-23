//go:build stage

package stage_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// createTestCardPayment pays a Razorpay test-mode order via S2S JSON card API.
// Requires S2S / create/json enabled on the Razorpay test account; otherwise t.Skip.
func createTestCardPayment(t *testing.T, orderID string, amount int64, currency string) (paymentID string) {
	t.Helper()
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" {
		currency = "INR"
	}
	year := fmt.Sprintf("%02d", (time.Now().Year()+3)%100)
	payload := map[string]any{
		"amount":   amount,
		"currency": currency,
		"order_id": orderID,
		"email":    "e2e@namepiq.test",
		"contact":  "9876543210",
		"method":   "card",
		"card": map[string]any{
			"number":       "4111111111111111",
			"expiry_month": "12",
			"expiry_year":  year,
			"cvv":          "123",
			"name":         "E2E Tester",
		},
		"authentication": map[string]any{
			"authentication_channel": "browser",
		},
		"browser": map[string]any{
			"java_enabled":       false,
			"javascript_enabled": true,
			"timezone_offset":    -330,
			"color_depth":        24,
			"screen_width":       1280,
			"screen_height":      800,
		},
		"ip":         "127.0.0.1",
		"referer":    "http://127.0.0.1:3001/cart",
		"user_agent": "Namepiq-Stage-E2E/1.0",
	}
	raw, _ := json.Marshal(payload)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.razorpay.com/v1/payments/create/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("pay req: %v", err)
	}
	req.SetBasicAuth(stageCfg.RazorpayKeyID, stageCfg.RazorpayKeySecret)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("pay do: %v", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode == http.StatusNotFound || strings.Contains(string(body), "not found on the server") {
		t.Skip("Razorpay S2S create/json not enabled on this test account — use Playwright Checkout for real card pay")
	}
	if res.StatusCode >= 300 {
		t.Fatalf("razorpay pay %d: %s", res.StatusCode, string(body))
	}
	var out map[string]any
	_ = json.Unmarshal(body, &out)
	paymentID = stringField(out, "razorpay_payment_id")
	if paymentID == "" {
		paymentID = stringField(out, "payment_id")
	}
	if paymentID == "" {
		paymentID = stringField(out, "id")
	}
	if paymentID == "" {
		if nested, ok := out["razorpay_payment"].(map[string]any); ok {
			paymentID = stringField(nested, "id")
		}
	}
	if paymentID == "" {
		t.Fatalf("no payment id in response: %s", string(body))
	}
	return paymentID
}

func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}
