package opensrs

import (
	"errors"
	"testing"
)

func TestMinRegistrationYearsAI(t *testing.T) {
	if MinRegistrationYears("foo.ai") != 2 {
		t.Fatal("ai min")
	}
	if MinRegistrationYears("foo.com") != 1 {
		t.Fatal("com min")
	}
}

func TestPriceYearsToTry(t *testing.T) {
	ys := PriceYearsToTry("x.ai", 1)
	if ys[0] != 2 {
		t.Fatalf("want 2 first, got %v", ys)
	}
}

func TestErrorClassifiers(t *testing.T) {
	if !IsPeriodNotSupported(errors.New("opensrs get_price 400: 1 year period is not supported for 'new' orders")) {
		t.Fatal("period")
	}
	if !IsSupplierUnavailable(errors.New("opensrs lookup 720: No Registry Agents (Supplier Unavailable)")) {
		t.Fatal("supplier")
	}
}
