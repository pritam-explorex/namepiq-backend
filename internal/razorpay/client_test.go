package razorpay

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestVerifyPaymentSignature(t *testing.T) {
	secret := "test_secret"
	orderID := "order_ABC"
	paymentID := "pay_XYZ"
	payload := orderID + "|" + paymentID
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))

	if !VerifyPaymentSignature(orderID, paymentID, sig, secret) {
		t.Fatal("valid signature rejected")
	}
	if VerifyPaymentSignature(orderID, paymentID, "deadbeef", secret) {
		t.Fatal("invalid signature accepted")
	}
}

func TestIsTestMode(t *testing.T) {
	c := New("rzp_test_abc", "secret", "")
	if !c.IsTestMode() {
		t.Fatal("expected test mode")
	}
	live := New("rzp_live_abc", "secret", "")
	if live.IsTestMode() {
		t.Fatal("expected live mode")
	}
}
