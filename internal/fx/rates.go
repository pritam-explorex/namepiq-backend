package fx

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Supported shopper currencies (curated; Razorpay International covers these).
var Supported = []string{"INR", "USD", "EUR", "GBP", "AUD", "CAD", "SGD", "AED", "JPY"}

// Exponent is Razorpay minor-unit digits (JPY=0, most=2).
func Exponent(currency string) int {
	switch strings.ToUpper(currency) {
	case "JPY", "KRW", "VND", "CLP", "ISK", "UGX", "XAF", "XOF":
		return 0
	case "BHD", "JOD", "KWD", "OMR", "TND":
		return 3
	default:
		return 2
	}
}

type Rates struct {
	Base      string
	PerUSD    map[string]float64 // quote per 1 USD
	FetchedAt time.Time
	Source    string
}

type Client struct {
	http     *http.Client
	ttl      time.Duration
	mu       sync.Mutex
	cached   *Rates
	fallback map[string]float64
}

func NewClient(fallbackUSD map[string]float64) *Client {
	fb := map[string]float64{"USD": 1}
	for k, v := range fallbackUSD {
		fb[strings.ToUpper(k)] = v
	}
	if _, ok := fb["INR"]; !ok {
		fb["INR"] = 84
	}
	return &Client{
		http:     &http.Client{Timeout: 8 * time.Second},
		ttl:      12 * time.Hour,
		fallback: fb,
	}
}

// NewStaticClient never calls Frankfurter — for tests and offline mode.
func NewStaticClient(rates map[string]float64) *Client {
	c := NewClient(rates)
	c.ttl = time.Hour * 24 * 365
	c.cached = c.fallbackRatesLocked()
	c.cached.Source = "static"
	return c
}

func (c *Client) Rate(ctx context.Context, currency string) (float64, string, error) {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" {
		currency = "INR"
	}
	if currency == "USD" {
		return 1, "identity", nil
	}
	rates, err := c.get(ctx)
	if err != nil {
		if r, ok := c.fallback[currency]; ok && r > 0 {
			return r, "fallback", nil
		}
		return 0, "", err
	}
	if r, ok := rates.PerUSD[currency]; ok && r > 0 {
		return r, rates.Source, nil
	}
	if r, ok := c.fallback[currency]; ok && r > 0 {
		return r, "fallback", nil
	}
	return 0, "", fmt.Errorf("unsupported currency %s", currency)
}

func (c *Client) Snapshot(ctx context.Context) (*Rates, error) {
	return c.get(ctx)
}

func (c *Client) get(ctx context.Context) (*Rates, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cached != nil && time.Since(c.cached.FetchedAt) < c.ttl {
		return c.cached, nil
	}
	quotes := make([]string, 0, len(Supported))
	for _, ccy := range Supported {
		if ccy != "USD" {
			quotes = append(quotes, strings.ToLower(ccy))
		}
	}
	url := "https://api.frankfurter.dev/v2/rates?base=usd&quotes=" + strings.Join(quotes, ",")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return c.fallbackRatesLocked(), nil
	}
	res, err := c.http.Do(req)
	if err != nil {
		return c.fallbackRatesLocked(), nil
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return c.fallbackRatesLocked(), nil
	}
	var rows []struct {
		Base  string  `json:"base"`
		Quote string  `json:"quote"`
		Rate  float64 `json:"rate"`
	}
	if err := json.NewDecoder(res.Body).Decode(&rows); err != nil || len(rows) == 0 {
		return c.fallbackRatesLocked(), nil
	}
	per := map[string]float64{"USD": 1}
	for _, row := range rows {
		q := strings.ToUpper(row.Quote)
		if q != "" && row.Rate > 0 {
			per[q] = row.Rate
		}
	}
	c.cached = &Rates{
		Base:      "USD",
		PerUSD:    per,
		FetchedAt: time.Now().UTC(),
		Source:    "frankfurter",
	}
	return c.cached, nil
}

func (c *Client) fallbackRatesLocked() *Rates {
	per := map[string]float64{"USD": 1}
	for k, v := range c.fallback {
		per[k] = v
	}
	c.cached = &Rates{
		Base:      "USD",
		PerUSD:    per,
		FetchedAt: time.Now().UTC(),
		Source:    "fallback",
	}
	return c.cached
}

func NormalizeCurrency(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	for _, s := range Supported {
		if s == code {
			return code
		}
	}
	return ""
}

func IsSupported(code string) bool {
	return NormalizeCurrency(code) != ""
}

// LocaleCurrency maps BCP-47 / country hints to a supported currency.
func LocaleCurrency(acceptLanguage, cfCountry string) string {
	cc := strings.ToUpper(strings.TrimSpace(cfCountry))
	switch cc {
	case "IN":
		return "INR"
	case "US", "PR", "GU", "VI":
		return "USD"
	case "GB", "UK":
		return "GBP"
	case "AU":
		return "AUD"
	case "CA":
		return "CAD"
	case "SG":
		return "SGD"
	case "AE":
		return "AED"
	case "JP":
		return "JPY"
	case "DE", "FR", "IT", "ES", "NL", "IE", "AT", "BE", "PT", "FI", "GR":
		return "EUR"
	}
	lang := strings.ToLower(acceptLanguage)
	switch {
	case strings.Contains(lang, "en-in"), strings.HasPrefix(lang, "hi"):
		return "INR"
	case strings.Contains(lang, "en-gb"):
		return "GBP"
	case strings.Contains(lang, "en-au"):
		return "AUD"
	case strings.Contains(lang, "en-ca"), strings.Contains(lang, "fr-ca"):
		return "CAD"
	case strings.Contains(lang, "ja"):
		return "JPY"
	case strings.HasPrefix(lang, "de"), strings.HasPrefix(lang, "fr"), strings.HasPrefix(lang, "es"), strings.HasPrefix(lang, "it"), strings.HasPrefix(lang, "nl"):
		return "EUR"
	case strings.Contains(lang, "en-us"), strings.HasPrefix(lang, "en"):
		return "USD"
	}
	return "USD"
}
