package domain

import (
	"context"
	"log"
	"strings"
	"sync"

	"github.com/namepiq/api/internal/fx"
	"github.com/namepiq/api/internal/opensrs"
)

type Registrar interface {
	Configured() bool
	Lookup(ctx context.Context, domain string) (opensrs.LookupResult, error)
	GetPrice(ctx context.Context, domain string, years int) (opensrs.PriceResult, error)
}

type SearchResult struct {
	Domain           string   `json:"domain"`
	Available        *bool    `json:"available"`
	WholesaleUSD     *float64 `json:"wholesale_usd,omitempty"`
	AmountMinor      *int64   `json:"amount_minor"`
	CompareAtMinor   *int64   `json:"compare_at_minor,omitempty"`
	DiscountPercent  *float64 `json:"discount_percent,omitempty"`
	PricePaise       *int64   `json:"price_paise"` // alias of amount_minor (compat)
	Currency         string   `json:"currency"`
	Premium          bool     `json:"premium,omitempty"`
	ProviderStatus   string   `json:"provider_status"`
	Message          string   `json:"message,omitempty"`
	FXSource         string   `json:"fx_source,omitempty"`
}

type SearchService struct {
	reg     Registrar
	pricing Pricing
	fx      *fx.Client
}

func NewSearchService(reg Registrar, pricing Pricing, rates *fx.Client) *SearchService {
	if rates == nil {
		rates = fx.NewClient(pricing.FallbackUSD)
	}
	if pricing.DisplayDiscountPercent <= 0 {
		pricing.DisplayDiscountPercent = 15
	}
	return &SearchService{reg: reg, pricing: pricing, fx: rates}
}

func (s *SearchService) Search(ctx context.Context, query, currency string) []SearchResult {
	currency = s.normalizeCurrency(currency)
	candidates := opensrs.ExpandCandidates(query)
	if len(candidates) == 0 {
		return nil
	}
	if s.reg == nil || !s.reg.Configured() {
		out := make([]SearchResult, 0, len(candidates))
		for _, d := range candidates {
			out = append(out, SearchResult{
				Domain:         d,
				Currency:       currency,
				ProviderStatus: "not_configured",
				Message:        "OpenSRS credentials not configured",
			})
		}
		return out
	}

	out := make([]SearchResult, len(candidates))
	var wg sync.WaitGroup
	for i, d := range candidates {
		wg.Add(1)
		go func(i int, d string) {
			defer wg.Done()
			out[i] = s.lookupOne(ctx, d, 1, currency)
		}(i, d)
	}
	wg.Wait()
	return out
}

func (s *SearchService) Quote(ctx context.Context, domain string, years int, currency string) SearchResult {
	domain = opensrs.NormalizeDomain(domain)
	currency = s.normalizeCurrency(currency)
	if years < 1 {
		years = 1
	}
	if years > 10 {
		years = 10
	}
	if s.reg == nil || !s.reg.Configured() {
		return SearchResult{
			Domain:         domain,
			Currency:       currency,
			ProviderStatus: "not_configured",
			Message:        "OpenSRS credentials not configured",
		}
	}
	return s.lookupOne(ctx, domain, years, currency)
}

func (s *SearchService) normalizeCurrency(currency string) string {
	if c := fx.NormalizeCurrency(currency); c != "" {
		return c
	}
	return "USD"
}

func (s *SearchService) lookupOne(ctx context.Context, d string, years int, currency string) SearchResult {
	if years < 1 {
		years = 1
	}
	res := SearchResult{Domain: d, Currency: currency, ProviderStatus: "ok"}
	look, err := s.reg.Lookup(ctx, d)
	if err != nil {
		log.Printf("opensrs lookup %s: %v", d, err)
		return softLookupFailure(res, err)
	}
	avail := look.Available
	res.Available = &avail
	if !look.Available {
		res.Message = "Taken"
		return res
	}

	price, pricedYears, err := s.priceWithFallback(ctx, d, years)
	if err != nil {
		log.Printf("opensrs price %s: %v", d, err)
		res.Message = "Available"
		res.ProviderStatus = "price_unavailable"
		return res
	}
	_ = pricedYears
	w := price.PriceUSD
	res.WholesaleUSD = &w
	rate, src, err := s.fx.Rate(ctx, currency)
	if err != nil {
		log.Printf("fx %s: %v", currency, err)
		res.Message = "Available"
		res.ProviderStatus = "price_unavailable"
		return res
	}
	res.FXSource = src
	// Normalize displayed retail to requested term when we had to price a longer min term.
	unitUSD := price.PriceUSD
	if pricedYears > 0 && years > 0 && pricedYears != years {
		unitUSD = price.PriceUSD * float64(years) / float64(pricedYears)
	}
	amount, compare := s.pricing.Retail(unitUSD, rate, currency)
	res.AmountMinor = &amount
	res.CompareAtMinor = &compare
	res.PricePaise = &amount
	disc := s.pricing.displayDiscount()
	res.DiscountPercent = &disc
	res.Premium = price.Premium
	res.Message = "Available"
	return res
}

func (s *SearchService) priceWithFallback(ctx context.Context, domain string, years int) (opensrs.PriceResult, int, error) {
	var last error
	for _, y := range opensrs.PriceYearsToTry(domain, years) {
		price, err := s.reg.GetPrice(ctx, domain, y)
		if err == nil {
			return price, y, nil
		}
		last = err
	}
	if last == nil {
		last = context.Canceled
	}
	return opensrs.PriceResult{}, 0, last
}

func softLookupFailure(res SearchResult, err error) SearchResult {
	res.Available = nil
	switch {
	case opensrs.IsSupplierUnavailable(err):
		res.ProviderStatus = "unavailable"
		res.Message = "Unavailable"
	case opensrs.IsConnLimitErr(err):
		res.ProviderStatus = "busy"
		res.Message = "Temporarily busy"
	default:
		res.ProviderStatus = "unavailable"
		res.Message = "Unavailable"
	}
	return res
}

func CurrencyFromRequest(queryCurrency, headerCurrency, acceptLang, cfCountry string) string {
	for _, c := range []string{queryCurrency, headerCurrency} {
		if fx.IsSupported(c) {
			return strings.ToUpper(strings.TrimSpace(c))
		}
	}
	return fx.LocaleCurrency(acceptLang, cfCountry)
}
