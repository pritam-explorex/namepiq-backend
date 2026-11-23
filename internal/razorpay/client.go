package razorpay

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
	"strings"
	"time"
)

const apiBase = "https://api.razorpay.com/v1"

type Client struct {
	keyID     string
	keySecret string
	webhook   string
	http      *http.Client
}

func New(keyID, keySecret, webhookSecret string) *Client {
	return &Client{
		keyID:     strings.TrimSpace(keyID),
		keySecret: strings.TrimSpace(keySecret),
		webhook:   strings.TrimSpace(webhookSecret),
		http:      &http.Client{Timeout: 20 * time.Second},
	}
}

func (c *Client) Configured() bool {
	return c != nil && c.keyID != "" && c.keySecret != ""
}

func (c *Client) KeyID() string {
	if c == nil {
		return ""
	}
	return c.keyID
}

func (c *Client) IsTestMode() bool {
	return strings.HasPrefix(c.keyID, "rzp_test_")
}

type CreateOrderRequest struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
	Receipt  string `json:"receipt,omitempty"`
	Notes    map[string]string `json:"notes,omitempty"`
}

type Order struct {
	ID       string `json:"id"`
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
	Receipt  string `json:"receipt"`
	Status   string `json:"status"`
}

func (c *Client) CreateOrder(ctx context.Context, req CreateOrderRequest) (Order, error) {
	if !c.Configured() {
		return Order{}, fmt.Errorf("razorpay not configured")
	}
	req.Currency = strings.ToUpper(strings.TrimSpace(req.Currency))
	if req.Amount < 1 {
		return Order{}, fmt.Errorf("amount must be >= 1")
	}
	if req.Currency == "" {
		req.Currency = "INR"
	}
	var out Order
	if err := c.do(ctx, http.MethodPost, "/orders", req, &out); err != nil {
		return Order{}, err
	}
	return out, nil
}

func VerifyPaymentSignature(orderID, paymentID, signature, keySecret string) bool {
	if orderID == "" || paymentID == "" || signature == "" || keySecret == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(keySecret))
	_, _ = mac.Write([]byte(orderID + "|" + paymentID))
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

func (c *Client) VerifyPaymentSignature(orderID, paymentID, signature string) bool {
	if c == nil {
		return false
	}
	return VerifyPaymentSignature(orderID, paymentID, signature, c.keySecret)
}

func (c *Client) VerifyWebhookSignature(body []byte, signature string) bool {
	if c == nil || c.webhook == "" || signature == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(c.webhook))
	_, _ = mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

func (c *Client) do(ctx context.Context, method, path string, in any, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, apiBase+path, body)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.keyID, c.keySecret)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if res.StatusCode >= 300 {
		return fmt.Errorf("razorpay %s %s: %s", method, path, strings.TrimSpace(string(raw)))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}
