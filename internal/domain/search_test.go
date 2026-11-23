package domain

import (
	"context"
	"errors"
	"testing"

	"github.com/namepiq/api/internal/fx"
	"github.com/namepiq/api/internal/opensrs"
)

type fakeReg struct {
	configured bool
	avail      map[string]bool
	prices     map[string]float64
	failLookup string
}

func (f *fakeReg) Configured() bool { return f.configured }
func (f *fakeReg) Lookup(ctx context.Context, domain string) (opensrs.LookupResult, error) {
	if domain == f.failLookup {
		return opensrs.LookupResult{}, errors.New("boom")
	}
	a, ok := f.avail[domain]
	if !ok {
		a = false
	}
	code := 211
	if a {
		code = 210
	}
	return opensrs.LookupResult{Domain: domain, Available: a, Code: code}, nil
}
func (f *fakeReg) GetPrice(ctx context.Context, domain string, years int) (opensrs.PriceResult, error) {
	p := f.prices[domain]
	return opensrs.PriceResult{Domain: domain, PriceUSD: p}, nil
}

func TestRetailMarkupAndDiscount(t *testing.T) {
	p := Pricing{MarkupPercent: 30, DisplayDiscountPercent: 15}
	// 10 USD * 1.3 * 100 INR = 1300 INR = 130000 paise
	amount, compare := p.Retail(10, 100, "INR")
	if amount != 130000 {
		t.Fatalf("amount=%d", amount)
	}
	// compare = 1300 / 0.85 ≈ 1529.41 → 152941 paise
	if compare < amount {
		t.Fatalf("compare=%d amount=%d", compare, amount)
	}
}

func TestSearchNotConfigured(t *testing.T) {
	svc := NewSearchService(&fakeReg{configured: false}, Pricing{}, fx.NewClient(nil))
	res := svc.Search(context.Background(), "acme.com", "INR")
	if len(res) < 2 || res[0].Domain != "acme.com" || res[0].ProviderStatus != "not_configured" {
		t.Fatalf("%+v", res)
	}
}

func TestSearchAvailableAndTaken(t *testing.T) {
	reg := &fakeReg{
		configured: true,
		avail: map[string]bool{
			"free.com":  true,
			"taken.com": false,
		},
		prices: map[string]float64{"free.com": 10},
	}
	// markup 0, rate 80 → 800 INR = 80000 paise
	svc := NewSearchService(reg, Pricing{
		MarkupPercent:          0,
		DisplayDiscountPercent: 15,
		FallbackUSD:            map[string]float64{"INR": 80},
	}, fx.NewStaticClient(map[string]float64{"INR": 80}))
	res := svc.Search(context.Background(), "free.com", "INR")
	if len(res) < 2 || res[0].Domain != "free.com" || res[0].Available == nil || !*res[0].Available {
		t.Fatalf("%+v", res)
	}
	if res[0].AmountMinor == nil || *res[0].AmountMinor != 80000 {
		t.Fatalf("price %+v", res[0])
	}

	res = svc.Search(context.Background(), "taken.com", "INR")
	if res[0].Domain != "taken.com" || res[0].Available == nil || *res[0].Available {
		t.Fatalf("expected taken %+v", res[0])
	}
}

func TestSearchExpandsBareLabel(t *testing.T) {
	reg := &fakeReg{configured: true, avail: map[string]bool{}, prices: map[string]float64{}}
	svc := NewSearchService(reg, Pricing{MarkupPercent: 30}, fx.NewClient(nil))
	res := svc.Search(context.Background(), "acme", "USD")
	if len(res) < 3 {
		t.Fatalf("expected tld expansion, got %d", len(res))
	}
}

func TestQuoteMultiYear(t *testing.T) {
	reg := &fakeReg{
		configured: true,
		avail:      map[string]bool{"multi.com": true},
		prices:     map[string]float64{"multi.com": 30},
	}
	svc := NewSearchService(reg, Pricing{MarkupPercent: 0, FallbackUSD: map[string]float64{"USD": 1}}, fx.NewStaticClient(map[string]float64{"USD": 1}))
	q := svc.Quote(context.Background(), "multi.com", 3, "USD")
	if q.Available == nil || !*q.Available || q.AmountMinor == nil || *q.AmountMinor != 3000 {
		// 30 USD with exp 2 = 3000 cents when markup 0
		t.Fatalf("%+v", q)
	}
}
