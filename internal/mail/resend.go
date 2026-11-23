package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Sender interface {
	Send(ctx context.Context, to, subject, html, text string) error
}

type Resend struct {
	apiKey string
	from   string
	apiURL string
	client *http.Client
}

func NewResend(apiKey, from string) *Resend {
	return &Resend{
		apiKey: apiKey,
		from:   from,
		apiURL: "https://api.resend.com/emails",
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

type LogSender struct{}

func (LogSender) Send(ctx context.Context, to, subject, html, text string) error {
	fmt.Printf("[mail:dev] to=%s subject=%q text=%s\n", to, subject, text)
	return nil
}

func (r *Resend) Send(ctx context.Context, to, subject, html, text string) error {
	if r.apiKey == "" {
		return fmt.Errorf("RESEND_API_KEY not configured")
	}
	body := map[string]any{
		"from":    r.from,
		"to":      []string{to},
		"subject": subject,
		"html":    html,
		"text":    text,
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.apiURL, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.apiKey)
	req.Header.Set("Content-Type", "application/json")

	res, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	respBody, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		return fmt.Errorf("resend %d: %s", res.StatusCode, string(respBody))
	}
	return nil
}
