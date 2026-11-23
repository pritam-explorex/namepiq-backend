package opensrs

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DefaultMaxConcurrent is the stage/Horizon-safe pool size.
const DefaultMaxConcurrent = 2

// AbsoluteMaxConcurrent caps OPENSRS_MAX_CONCURRENT (prod target: 50).
const AbsoluteMaxConcurrent = 50

type Client struct {
	username string
	apiKey   string
	endpoint string
	http     *http.Client
	sem      chan struct{}
}

func New(username, apiKey, endpoint string) *Client {
	return NewWithConcurrency(username, apiKey, endpoint, DefaultMaxConcurrent)
}

func NewWithConcurrency(username, apiKey, endpoint string, maxConcurrent int) *Client {
	if endpoint == "" {
		endpoint = "https://horizon.opensrs.net:55443"
	}
	if maxConcurrent < 1 {
		maxConcurrent = DefaultMaxConcurrent
	}
	if maxConcurrent > AbsoluteMaxConcurrent {
		maxConcurrent = AbsoluteMaxConcurrent
	}
	return &Client{
		username: username,
		apiKey:   apiKey,
		endpoint: endpoint,
		sem:      make(chan struct{}, maxConcurrent),
		http: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        maxConcurrent * 2,
				MaxIdleConnsPerHost: maxConcurrent,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

func (c *Client) Configured() bool {
	return c != nil && c.username != "" && c.apiKey != ""
}

type LookupResult struct {
	Domain    string
	Available bool
	Code      int
	Text      string
}

type PriceResult struct {
	Domain  string
	PriceUSD float64
	Premium bool
	Text    string
}

func (c *Client) Lookup(ctx context.Context, domain string) (LookupResult, error) {
	domain = NormalizeDomain(domain)
	xmlBody := lookupXML(domain)
	var last LookupResult
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			if err := sleepCtx(ctx, time.Duration(attempt)*400*time.Millisecond); err != nil {
				return LookupResult{}, err
			}
		}
		raw, err := c.post(ctx, xmlBody)
		if err != nil {
			return LookupResult{}, err
		}
		code, text, ok := parseReplyMeta(raw)
		if !ok {
			return LookupResult{}, fmt.Errorf("opensrs lookup: unparseable response")
		}
		last = LookupResult{Domain: domain, Code: code, Text: text}
		if code == 310 || isConnLimit(text) {
			continue
		}
		switch code {
		case 210:
			last.Available = true
		case 211:
			last.Available = false
		default:
			if !strings.Contains(strings.ToLower(text), "success") && code != 200 {
				return last, fmt.Errorf("opensrs lookup %d: %s", code, text)
			}
		}
		return last, nil
	}
	return last, fmt.Errorf("opensrs lookup %d: %s", last.Code, last.Text)
}

func (c *Client) GetPrice(ctx context.Context, domain string, years int) (PriceResult, error) {
	domain = NormalizeDomain(domain)
	if years < 1 {
		years = 1
	}
	xmlBody := getPriceXML(domain, years)
	var lastCode int
	var lastText string
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			if err := sleepCtx(ctx, time.Duration(attempt)*400*time.Millisecond); err != nil {
				return PriceResult{}, err
			}
		}
		raw, err := c.post(ctx, xmlBody)
		if err != nil {
			return PriceResult{}, err
		}
		code, text, ok := parseReplyMeta(raw)
		if !ok {
			return PriceResult{}, fmt.Errorf("opensrs get_price: unparseable response")
		}
		lastCode, lastText = code, text
		if code == 310 || isConnLimit(text) {
			continue
		}
		if code != 200 {
			return PriceResult{}, fmt.Errorf("opensrs get_price %d: %s", code, text)
		}
		price, premium := parsePriceAttrs(raw)
		return PriceResult{Domain: domain, PriceUSD: price, Premium: premium, Text: text}, nil
	}
	return PriceResult{}, fmt.Errorf("opensrs get_price %d: %s", lastCode, lastText)
}

func (c *Client) post(ctx context.Context, xmlBody string) ([]byte, error) {
	if c.sem != nil {
		select {
		case c.sem <- struct{}{}:
			defer func() { <-c.sem }()
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	sig := Sign(xmlBody, c.apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewBufferString(xmlBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "text/xml")
	req.Header.Set("X-Username", c.username)
	req.Header.Set("X-Signature", sig)

	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("opensrs http %d: %s", res.StatusCode, truncate(string(body), 200))
	}
	return body, nil
}

func isConnLimit(text string) bool {
	t := strings.ToLower(text)
	return strings.Contains(t, "connection limit") || strings.Contains(t, "maximum client")
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func Sign(xmlBody, apiKey string) string {
	first := md5.Sum([]byte(xmlBody + apiKey))
	firstHex := hex.EncodeToString(first[:])
	second := md5.Sum([]byte(firstHex + apiKey))
	return hex.EncodeToString(second[:])
}

func lookupXML(domain string) string {
	return fmt.Sprintf(`<?xml version='1.0' encoding='UTF-8' standalone='no' ?>
<!DOCTYPE OPS_envelope SYSTEM 'ops.dtd'>
<OPS_envelope>
<header>
    <version>0.9</version>
</header>
<body>
<data_block>
    <dt_assoc>
        <item key="protocol">XCP</item>
        <item key="action">LOOKUP</item>
        <item key="object">DOMAIN</item>
        <item key="attributes">
         <dt_assoc>
                <item key="domain">%s</item>
         </dt_assoc>
        </item>
    </dt_assoc>
</data_block>
</body>
</OPS_envelope>
`, xmlEscape(domain))
}

func getPriceXML(domain string, years int) string {
	return fmt.Sprintf(`<?xml version='1.0' encoding='UTF-8' standalone='no'?>
<OPS_envelope>
    <header>
        <version>0.9</version>
    </header>
    <body>
        <data_block>
            <dt_assoc>
                <item key="protocol">XCP</item>
                <item key="action">GET_PRICE</item>
                <item key="object">DOMAIN</item>
                <item key="attributes">
                    <dt_assoc>
                        <item key="domain">%s</item>
                        <item key="period">%d</item>
                        <item key="reg_type">new</item>
                    </dt_assoc>
                </item>
            </dt_assoc>
        </data_block>
    </body>
</OPS_envelope>
`, xmlEscape(domain), years)
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

var (
	reCode = regexp.MustCompile(`(?i)<item key="response_code">(\d+)</item>`)
	reText = regexp.MustCompile(`(?i)<item key="response_text">([^<]*)</item>`)
	rePrice = regexp.MustCompile(`(?i)<item key="price">([^<]+)</item>`)
	rePremium = regexp.MustCompile(`(?i)<item key="is_registry_premium">([^<]+)</item>`)
)

func parseReplyMeta(raw []byte) (code int, text string, ok bool) {
	s := string(raw)
	m := reCode.FindStringSubmatch(s)
	if len(m) < 2 {
		return 0, "", false
	}
	code, _ = strconv.Atoi(m[1])
	if t := reText.FindStringSubmatch(s); len(t) > 1 {
		text = t[1]
	}
	return code, text, true
}

func parsePriceAttrs(raw []byte) (float64, bool) {
	s := string(raw)
	price := 0.0
	if m := rePrice.FindStringSubmatch(s); len(m) > 1 {
		price, _ = strconv.ParseFloat(strings.TrimSpace(m[1]), 64)
	}
	premium := false
	if m := rePremium.FindStringSubmatch(s); len(m) > 1 {
		v := strings.TrimSpace(strings.ToLower(m[1]))
		premium = v == "1" || v == "true" || v == "yes"
	}
	return price, premium
}

func NormalizeDomain(q string) string {
	q = strings.TrimSpace(strings.ToLower(q))
	q = strings.TrimPrefix(q, "http://")
	q = strings.TrimPrefix(q, "https://")
	q = strings.TrimPrefix(q, "www.")
	if i := strings.IndexAny(q, "/?"); i >= 0 {
		q = q[:i]
	}
	return q
}

var defaultTLDs = []string{"com", "net", "org", "in", "co", "io", "ai", "dev", "app", "me", "xyz"}

func ExpandCandidates(q string) []string {
	q = NormalizeDomain(q)
	if q == "" {
		return nil
	}
	label := q
	preferred := ""
	if i := strings.LastIndex(q, "."); i > 0 {
		label = q[:i]
		preferred = q[i+1:]
		// multi-dot like co.uk → treat whole as exact only
		if strings.Contains(label, ".") {
			return []string{q}
		}
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(defaultTLDs)+1)
	add := func(d string) {
		if d == "" || seen[d] {
			return
		}
		seen[d] = true
		out = append(out, d)
	}
	if preferred != "" {
		add(label + "." + preferred)
	}
	for _, tld := range defaultTLDs {
		add(label + "." + tld)
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
