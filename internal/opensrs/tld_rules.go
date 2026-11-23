package opensrs

import "strings"

// MinRegistrationYears is the shortest term OpenSRS accepts for a new registration.
func MinRegistrationYears(domain string) int {
	switch tldOf(domain) {
	case "ai":
		return 2
	default:
		return 1
	}
}

// PriceYearsToTry returns years to attempt for pricing (shortest first).
func PriceYearsToTry(domain string, requested int) []int {
	if requested < 1 {
		requested = 1
	}
	min := MinRegistrationYears(domain)
	if requested < min {
		requested = min
	}
	seen := map[int]bool{}
	out := make([]int, 0, 4)
	add := func(y int) {
		if y < 1 || y > 10 || seen[y] {
			return
		}
		seen[y] = true
		out = append(out, y)
	}
	add(requested)
	for _, y := range []int{2, 3, 5, 1, 10} {
		add(y)
	}
	return out
}

func tldOf(domain string) string {
	domain = NormalizeDomain(domain)
	i := strings.LastIndex(domain, ".")
	if i < 0 || i == len(domain)-1 {
		return ""
	}
	return domain[i+1:]
}

func IsPeriodNotSupported(err error) bool {
	if err == nil {
		return false
	}
	t := strings.ToLower(err.Error())
	return strings.Contains(t, "period is not supported") ||
		strings.Contains(t, "year period is not supported")
}

func IsSupplierUnavailable(err error) bool {
	if err == nil {
		return false
	}
	t := strings.ToLower(err.Error())
	return strings.Contains(t, "supplier unavailable") ||
		strings.Contains(t, "no registry agents") ||
		strings.Contains(t, "720:")
}

func IsConnLimitErr(err error) bool {
	if err == nil {
		return false
	}
	t := strings.ToLower(err.Error())
	return strings.Contains(t, "310:") ||
		strings.Contains(t, "connection limit") ||
		strings.Contains(t, "maximum client")
}
