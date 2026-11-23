//go:build stage

package stage_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestStageFreeWatchAndComments(t *testing.T) {
	email := uniqueEmail("watch")
	token := registerVerified(t, email, "password12345")
	upsertContact(t, token)

	// google.com is taken on horizon/public DNS
	code, body, _ := doJSON(t, http.MethodPost, "/v1/watches", map[string]any{
		"domain": "google.com",
	}, token)
	if code != http.StatusCreated && code != http.StatusOK {
		// may be 409 if already watching from prior run — try list
		if code != http.StatusConflict {
			t.Fatalf("free watch %d %#v", code, body)
		}
	}

	code, body, _ = doJSON(t, http.MethodGet, "/v1/watches", nil, token)
	if code != http.StatusOK {
		t.Fatalf("list watches %d %#v", code, body)
	}

	code, body, _ = doJSON(t, http.MethodPost, "/v1/domains/google.com/comments", map[string]any{
		"body": "e2e comment " + fmt.Sprint(time.Now().UnixNano()),
	}, token)
	if code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("comment %d %#v", code, body)
	}
	code, body, _ = doJSON(t, http.MethodGet, "/v1/domains/google.com/comments", nil, "")
	if code != http.StatusOK {
		t.Fatalf("list comments %d %#v", code, body)
	}
}

func TestStageCheckoutRealRazorpayPay(t *testing.T) {
	email := uniqueEmail("pay")
	token := registerVerified(t, email, "password12345")
	upsertContact(t, token)

	// Find an available cheap-ish domain via search
	q := fmt.Sprintf("npqe2e%d", time.Now().Unix()%1_000_000)
	code, body, _ := doJSON(t, http.MethodGet, "/v1/domains/search?q="+q+"&currency=INR", nil, token)
	if code != http.StatusOK {
		t.Fatalf("search %d %#v", code, body)
	}
	results, _ := body["results"].([]any)
	var domain string
	var amount float64
	var currency string
	for _, raw := range results {
		r, _ := raw.(map[string]any)
		if r["available"] == true {
			domain, _ = r["domain"].(string)
			if v, ok := r["amount_minor"].(float64); ok {
				amount = v
			} else if v, ok := r["price_paise"].(float64); ok {
				amount = v
			}
			currency, _ = r["currency"].(string)
			break
		}
	}
	if domain == "" {
		t.Skip("no available domain in search results for checkout e2e")
	}
	if amount < 1 {
		t.Fatalf("missing price for %s %#v", domain, body)
	}
	if currency == "" {
		currency = "INR"
	}

	code, body, _ = doJSON(t, http.MethodPost, "/v1/checkout", map[string]any{
		"currency":     currency,
		"tos_accepted": true,
		"items": []map[string]any{
			{"domain": domain, "years": 1},
		},
	}, token)
	if code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("checkout create %d %#v", code, body)
	}
	orderID, _ := body["razorpay_order_id"].(string)
	amt, _ := body["amount"].(float64)
	if orderID == "" {
		t.Fatalf("no razorpay_order_id %#v", body)
	}
	if amt > 0 {
		amount = amt
	}

	paymentID := createTestCardPayment(t, orderID, int64(amount), currency)
	sig := signPayment(orderID, paymentID)
	code, body, _ = doJSON(t, http.MethodPost, "/v1/checkout/verify", map[string]string{
		"razorpay_order_id":   orderID,
		"razorpay_payment_id": paymentID,
		"razorpay_signature":  sig,
	}, token)
	if code != http.StatusOK {
		t.Fatalf("checkout verify %d %#v", code, body)
	}
}
