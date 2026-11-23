package domain

import (
	"math"

	"github.com/namepiq/api/internal/fx"
)

type Pricing struct {
	MarkupPercent          float64 // charge markup over wholesale, e.g. 30
	DisplayDiscountPercent float64 // UI "off" percent, e.g. 15
	FallbackUSD            map[string]float64
}

func (p Pricing) markupFactor() float64 {
	m := p.MarkupPercent
	if m < 0 {
		m = 0
	}
	return 1 + m/100
}

func (p Pricing) displayDiscount() float64 {
	d := p.DisplayDiscountPercent
	if d <= 0 || d >= 100 {
		return 15
	}
	return d
}

// Retail converts wholesale USD into shopper currency minor units.
// charge = wholesale × (1+markup) × usdRate
// compare_at = charge / (1 - displayDiscount) so UI can show "15% off"
func (p Pricing) Retail(wholesaleUSD, usdToCurrency float64, currency string) (amountMinor, compareAtMinor int64) {
	if usdToCurrency <= 0 {
		usdToCurrency = 1
	}
	chargeMajor := wholesaleUSD * p.markupFactor() * usdToCurrency
	disc := p.displayDiscount() / 100
	compareMajor := chargeMajor
	if disc > 0 && disc < 1 {
		compareMajor = chargeMajor / (1 - disc)
	}
	exp := fx.Exponent(currency)
	scale := math.Pow10(exp)
	amountMinor = int64(math.Round(chargeMajor * scale))
	compareAtMinor = int64(math.Round(compareMajor * scale))
	if compareAtMinor < amountMinor {
		compareAtMinor = amountMinor
	}
	return amountMinor, compareAtMinor
}
