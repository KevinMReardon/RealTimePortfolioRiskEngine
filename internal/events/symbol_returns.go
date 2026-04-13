package events

import (
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

const symbolReturnsWindowN = 60

func eventUTCDate(t time.Time) time.Time {
	ut := t.UTC()
	return time.Date(ut.Year(), ut.Month(), ut.Day(), 0, 0, 0, 0, time.UTC)
}

func computeDailyReturn(prevClose, close decimal.Decimal) (decimal.Decimal, error) {
	if !prevClose.GreaterThan(decimal.Zero) {
		return decimal.Zero, fmt.Errorf("prev close must be > 0")
	}
	if !close.GreaterThan(decimal.Zero) {
		return decimal.Zero, fmt.Errorf("close must be > 0")
	}
	return close.Div(prevClose).Sub(decimal.NewFromInt(1)), nil
}
