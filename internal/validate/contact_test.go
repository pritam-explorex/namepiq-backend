package validate

import "testing"

func TestNormalizePhoneToE164(t *testing.T) {
	cases := []struct {
		phone, country, want string
	}{
		{"9876543210", "IN", "+919876543210"},
		{"+91.9876543210", "IN", "+919876543210"},
		{"+919876543210", "IN", "+919876543210"},
		{"(416) 555-0123", "CA", "+14165550123"},
		{"+1.4165550123", "US", "+14165550123"},
	}
	for _, tc := range cases {
		got, err := NormalizePhoneToE164(tc.phone, tc.country)
		if err != nil {
			t.Fatalf("%s/%s: %v", tc.phone, tc.country, err)
		}
		if got != tc.want {
			t.Fatalf("%s/%s: got %s want %s", tc.phone, tc.country, got, tc.want)
		}
	}
}

func TestNormalizePhoneRejects(t *testing.T) {
	if _, err := NormalizePhoneToE164("123", "IN"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := NormalizePhoneToE164("", "IN"); err == nil {
		t.Fatal("expected error")
	}
}

func TestNormalizeCountry(t *testing.T) {
	got, err := NormalizeCountry("in")
	if err != nil || got != "IN" {
		t.Fatalf("%s %v", got, err)
	}
	got, err = NormalizeCountry("UK")
	if err != nil || got != "GB" {
		t.Fatalf("%s %v", got, err)
	}
	if _, err := NormalizeCountry("XX"); err == nil {
		t.Fatal("expected unsupported")
	}
}

func TestValidatePostal(t *testing.T) {
	if err := ValidatePostal("560001", "IN"); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePostal("94107", "US"); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePostal("SW1A 1AA", "GB"); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePostal("12", "IN"); err == nil {
		t.Fatal("expected invalid")
	}
	if err := ValidatePostal("", "AE"); err != nil {
		t.Fatal(err)
	}
}
