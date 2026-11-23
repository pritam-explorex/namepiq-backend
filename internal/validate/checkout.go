package validate

import "fmt"

// Cart / checkout abuse caps.
const MaxCheckoutItems = 10

// MaxCheckoutMinor is the max cart total in currency minor units (paise/cents/yen).
var MaxCheckoutMinor = map[string]int64{
	"INR": 500_000_00, // ₹500,000
	"USD": 5_000_00,   // $5,000
	"EUR": 5_000_00,
	"GBP": 4_000_00,
	"AUD": 8_000_00,
	"CAD": 7_000_00,
	"SGD": 7_000_00,
	"AED": 20_000_00,
	"JPY": 750_000, // ¥750,000 (no minor digits)
}

func CheckCartLimits(itemCount int, totalMinor int64, currency string) error {
	if itemCount < 1 {
		return fmt.Errorf("cart is empty")
	}
	if itemCount > MaxCheckoutItems {
		return fmt.Errorf("cart limited to %d domains", MaxCheckoutItems)
	}
	capAmt, ok := MaxCheckoutMinor[currency]
	if !ok {
		capAmt = MaxCheckoutMinor["USD"]
	}
	if totalMinor > capAmt {
		return fmt.Errorf("cart total exceeds maximum for %s", currency)
	}
	return nil
}
