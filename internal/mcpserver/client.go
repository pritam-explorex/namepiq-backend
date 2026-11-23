package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Client talks to the Namepiq HTTP API.
type Client struct {
	base   string
	http   *http.Client
	mu     sync.RWMutex
	token  string
}

func NewClient(baseURL, token string) *Client {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = "http://localhost:8080"
	}
	return &Client{
		base:  base,
		token: strings.TrimSpace(token),
		http:  &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *Client) SetToken(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = strings.TrimSpace(token)
}

func (c *Client) Token() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.token
}

func (c *Client) get(ctx context.Context, path string, q url.Values, auth bool) (json.RawMessage, error) {
	u := c.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, auth)
}

func (c *Client) sendJSON(ctx context.Context, method, path string, body any, auth bool) (json.RawMessage, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rdr)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.do(req, auth)
}

func (c *Client) do(req *http.Request, auth bool) (json.RawMessage, error) {
	if auth {
		tok := c.Token()
		if tok == "" {
			return nil, fmt.Errorf("not authenticated: call login or set NAMEPIQ_TOKEN")
		}
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 400 {
		msg := strings.TrimSpace(string(raw))
		if msg == "" {
			msg = res.Status
		}
		return nil, fmt.Errorf("API %s %s: %s", req.Method, req.URL.Path, msg)
	}
	if len(raw) == 0 {
		return json.RawMessage(`{}`), nil
	}
	if !json.Valid(raw) {
		return nil, fmt.Errorf("non-JSON response: %s", truncate(string(raw), 200))
	}
	return json.RawMessage(raw), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// --- API wrappers ---

func (c *Client) Search(ctx context.Context, q, currency string) (json.RawMessage, error) {
	v := url.Values{"q": {q}}
	if currency != "" {
		v.Set("currency", currency)
	}
	return c.get(ctx, "/v1/domains/search", v, false)
}

func (c *Client) Suggest(ctx context.Context, query, tlds string) (json.RawMessage, error) {
	v := url.Values{"q": {query}}
	if tlds != "" {
		v.Set("tlds", tlds)
	}
	return c.get(ctx, "/v1/domains/suggest", v, false)
}

func (c *Client) Quote(ctx context.Context, domain string, years int, currency string) (json.RawMessage, error) {
	v := url.Values{"domain": {domain}}
	if years > 0 {
		v.Set("years", fmt.Sprintf("%d", years))
	}
	if currency != "" {
		v.Set("currency", currency)
	}
	return c.get(ctx, "/v1/domains/quote", v, false)
}

func (c *Client) Login(ctx context.Context, email, password string) (json.RawMessage, error) {
	raw, err := c.sendJSON(ctx, http.MethodPost, "/v1/auth/login", map[string]string{
		"email":    email,
		"password": password,
	}, false)
	if err != nil {
		return nil, err
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(raw, &out); err == nil && out.AccessToken != "" {
		c.SetToken(out.AccessToken)
	}
	return raw, nil
}

func (c *Client) Me(ctx context.Context) (json.RawMessage, error) {
	return c.get(ctx, "/v1/me", nil, true)
}

func (c *Client) UpsertContact(ctx context.Context, body any) (json.RawMessage, error) {
	return c.sendJSON(ctx, http.MethodPut, "/v1/me/contact", body, true)
}

func (c *Client) ListDomains(ctx context.Context) (json.RawMessage, error) {
	return c.get(ctx, "/v1/domains", nil, true)
}

func (c *Client) GetDomain(ctx context.Context, id string) (json.RawMessage, error) {
	return c.get(ctx, "/v1/domains/"+url.PathEscape(id), nil, true)
}

func (c *Client) RegisterDomain(ctx context.Context, domain string, years int, autoRenew bool) (json.RawMessage, error) {
	return c.sendJSON(ctx, http.MethodPost, "/v1/domains/register", map[string]any{
		"domain":     domain,
		"years":      years,
		"auto_renew": autoRenew,
	}, true)
}

func (c *Client) RenewDomain(ctx context.Context, id string, years int) (json.RawMessage, error) {
	body := map[string]any{}
	if years > 0 {
		body["years"] = years
	}
	return c.sendJSON(ctx, http.MethodPost, "/v1/domains/"+url.PathEscape(id)+"/renew", body, true)
}

func (c *Client) PatchDomain(ctx context.Context, id string, body map[string]any) (json.RawMessage, error) {
	return c.sendJSON(ctx, http.MethodPatch, "/v1/domains/"+url.PathEscape(id), body, true)
}

func (c *Client) PushContacts(ctx context.Context, id string) (json.RawMessage, error) {
	return c.sendJSON(ctx, http.MethodPut, "/v1/domains/"+url.PathEscape(id)+"/contacts", map[string]any{}, true)
}

func (c *Client) GetNameservers(ctx context.Context, id string) (json.RawMessage, error) {
	return c.get(ctx, "/v1/domains/"+url.PathEscape(id)+"/nameservers", nil, true)
}

func (c *Client) SetNameservers(ctx context.Context, id string, nameservers []string) (json.RawMessage, error) {
	return c.sendJSON(ctx, http.MethodPut, "/v1/domains/"+url.PathEscape(id)+"/nameservers", map[string]any{
		"nameservers": nameservers,
	}, true)
}

func (c *Client) GetDNS(ctx context.Context, id string) (json.RawMessage, error) {
	return c.get(ctx, "/v1/domains/"+url.PathEscape(id)+"/dns", nil, true)
}

func (c *Client) SetDNS(ctx context.Context, id string, records any) (json.RawMessage, error) {
	return c.sendJSON(ctx, http.MethodPut, "/v1/domains/"+url.PathEscape(id)+"/dns", map[string]any{
		"records": records,
	}, true)
}
