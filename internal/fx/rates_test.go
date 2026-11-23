package fx

import (
	"context"
	"testing"
)

func TestExponent(t *testing.T) {
	if Exponent("JPY") != 0 || Exponent("USD") != 2 || Exponent("KWD") != 3 {
		t.Fatal("exponents")
	}
}

func TestRateUSDAndFallback(t *testing.T) {
	c := NewClient(map[string]float64{"INR": 84, "EUR": 0.9})
	r, src, err := c.Rate(context.Background(), "USD")
	if err != nil || r != 1 || src != "identity" {
		t.Fatalf("%v %v %v", r, src, err)
	}
}

func TestLocaleCurrency(t *testing.T) {
	if LocaleCurrency("", "IN") != "INR" {
		t.Fatal("IN")
	}
	if LocaleCurrency("en-GB,en;q=0.9", "") != "GBP" {
		t.Fatal("en-GB")
	}
}

func TestFrankfurterOrFallback(t *testing.T) {
	c := NewClient(map[string]float64{"INR": 84})
	r, src, err := c.Rate(context.Background(), "INR")
	if err != nil {
		t.Fatal(err)
	}
	if r <= 0 {
		t.Fatalf("rate=%v src=%s", r, src)
	}
	t.Logf("INR rate=%v source=%s", r, src)
}
