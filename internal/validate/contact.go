package validate

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// Supported ISO-3166-1 alpha-2 countries (aligned with shopper UI).
var SupportedCountries = map[string]bool{
	"IN": true, "US": true, "GB": true, "AU": true, "CA": true,
	"SG": true, "AE": true, "JP": true, "DE": true, "FR": true, "ES": true,
}

var dialCode = map[string]string{
	"IN": "91", "US": "1", "CA": "1", "GB": "44", "AU": "61",
	"SG": "65", "AE": "971", "JP": "81", "DE": "49", "FR": "33", "ES": "34",
}

var (
	reE164        = regexp.MustCompile(`^\+[1-9]\d{7,14}$`)
	reOpenSRSPhone = regexp.MustCompile(`^\+[1-9]\d{0,3}\.\d{6,14}$`)
)

var postalRules = map[string]*regexp.Regexp{
	"IN": regexp.MustCompile(`^\d{6}$`),
	"US": regexp.MustCompile(`^\d{5}(-\d{4})?$`),
	"CA": regexp.MustCompile(`(?i)^[A-Z]\d[A-Z]\s?\d[A-Z]\d$`),
	"GB": regexp.MustCompile(`(?i)^[A-Z]{1,2}\d[A-Z\d]?\s?\d[A-Z]{2}$`),
	"AU": regexp.MustCompile(`^\d{4}$`),
	"SG": regexp.MustCompile(`^\d{6}$`),
	"AE": regexp.MustCompile(`^(\d{5}|\d{6})?$`), // often optional
	"JP": regexp.MustCompile(`^\d{3}-?\d{4}$`),
	"DE": regexp.MustCompile(`^\d{5}$`),
	"FR": regexp.MustCompile(`^\d{5}$`),
	"ES": regexp.MustCompile(`^\d{5}$`),
}

// NormalizeCountry returns uppercased ISO alpha-2 or error.
func NormalizeCountry(country string) (string, error) {
	cc := strings.ToUpper(strings.TrimSpace(country))
	if cc == "UK" {
		cc = "GB"
	}
	if len(cc) != 2 {
		return "", fmt.Errorf("country must be a 2-letter ISO code (e.g. IN, US)")
	}
	for _, r := range cc {
		if r < 'A' || r > 'Z' {
			return "", fmt.Errorf("country must be a 2-letter ISO code (e.g. IN, US)")
		}
	}
	if !SupportedCountries[cc] {
		return "", fmt.Errorf("unsupported country %s", cc)
	}
	return cc, nil
}

// NormalizePhoneToE164 accepts E.164, OpenSRS (+CC.national), or national digits + country.
func NormalizePhoneToE164(phone, country string) (string, error) {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return "", fmt.Errorf("phone is required")
	}
	cc, err := NormalizeCountry(country)
	if err != nil {
		// allow phone normalize if country already known from prefix; still require country for national
		cc = strings.ToUpper(strings.TrimSpace(country))
	}

	// OpenSRS: +91.9876543210 → +919876543210
	if reOpenSRSPhone.MatchString(phone) {
		parts := strings.SplitN(phone, ".", 2)
		e164 := parts[0] + parts[1]
		if !reE164.MatchString(e164) {
			return "", fmt.Errorf("phone must be a valid international number")
		}
		return e164, nil
	}

	if strings.HasPrefix(phone, "+") {
		digits := digitsOnly(phone)
		e164 := "+" + digits
		if !reE164.MatchString(e164) {
			return "", fmt.Errorf("phone must be E.164 (e.g. +919876543210)")
		}
		return e164, nil
	}

	national := digitsOnly(phone)
	if national == "" {
		return "", fmt.Errorf("phone must contain digits")
	}
	prefix, ok := dialCode[cc]
	if !ok {
		return "", fmt.Errorf("cannot infer dial code; use E.164 (e.g. +919876543210)")
	}
	// strip leading dial code if user typed it without +
	if strings.HasPrefix(national, prefix) && len(national) > len(prefix)+5 {
		national = national[len(prefix):]
	}
	e164 := "+" + prefix + national
	if !reE164.MatchString(e164) {
		return "", fmt.Errorf("phone must be a valid national number for %s (or E.164)", cc)
	}
	return e164, nil
}

// ValidatePostal checks basic postal format for the country. Empty is allowed
// only for AE (often unused); other countries require a match.
func ValidatePostal(postal, country string) error {
	cc, err := NormalizeCountry(country)
	if err != nil {
		return err
	}
	postal = strings.TrimSpace(postal)
	rule, ok := postalRules[cc]
	if !ok {
		if postal == "" {
			return fmt.Errorf("postal_code is required")
		}
		return nil
	}
	if cc == "AE" && postal == "" {
		return nil
	}
	if postal == "" {
		return fmt.Errorf("postal_code is required")
	}
	if !rule.MatchString(postal) {
		return fmt.Errorf("invalid postal_code for %s", cc)
	}
	return nil
}

// NormalizePostal trims and uppercases letters where useful (CA/GB).
func NormalizePostal(postal, country string) string {
	postal = strings.TrimSpace(postal)
	cc := strings.ToUpper(strings.TrimSpace(country))
	switch cc {
	case "CA", "GB":
		var b strings.Builder
		for _, r := range postal {
			if unicode.IsLetter(r) {
				b.WriteRune(unicode.ToUpper(r))
			} else {
				b.WriteRune(r)
			}
		}
		return b.String()
	default:
		return postal
	}
}

func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
