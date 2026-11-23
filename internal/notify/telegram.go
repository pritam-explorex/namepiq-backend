package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Notifier sends ops alerts (best-effort; never fails the main request).
type Notifier interface {
	Notify(ctx context.Context, text string)
}

type Noop struct{}

func (Noop) Notify(context.Context, string) {}

type Telegram struct {
	token  string
	chatID string
	client *http.Client
}

func NewTelegram(token, chatID string) Notifier {
	token = strings.TrimSpace(token)
	chatID = strings.TrimSpace(chatID)
	if token == "" || chatID == "" {
		return Noop{}
	}
	return &Telegram{
		token:  token,
		chatID: chatID,
		client: &http.Client{Timeout: 8 * time.Second},
	}
}

func (t *Telegram) Notify(ctx context.Context, text string) {
	if t == nil || text == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_ = t.send(ctx, text)
	}()
}

func (t *Telegram) send(ctx context.Context, text string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.token)
	body, _ := json.Marshal(map[string]any{
		"chat_id":    t.chatID,
		"text":       text,
		"parse_mode": "HTML",
		"disable_web_page_preview": true,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		return fmt.Errorf("telegram %d: %s", res.StatusCode, string(raw))
	}
	return nil
}
