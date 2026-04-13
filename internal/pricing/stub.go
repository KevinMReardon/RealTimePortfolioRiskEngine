package pricing

import "github.com/shopspring/decimal"

// PriceRepository defines minimal read/write access to cached prices.
type PriceRepository interface {
	Set(symbol string, price decimal.Decimal) error
	Get(symbol string) (decimal.Decimal, bool, error)
}
