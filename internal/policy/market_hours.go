package policy

import (
	"time"
)

var eastern *time.Location

func init() {
	var err error
	eastern, err = time.LoadLocation("America/New_York")
	if err != nil {
		// Fallback without DST (tests still run; production should have zoneinfo).
		eastern = time.FixedZone("America/New_York", -5*3600)
	}
}

// IsUSRegularSessionEquities is true during NYSE regular hours Mon–Fri 09:30–16:00 America/New_York,
// half-open [open, close). Weekends are closed. Federal holidays are not modeled (session may be wrong on those weekdays—fail-closed only for weekends in v1).
func IsUSRegularSessionEquities(now time.Time) bool {
	local := now.In(eastern)
	wd := local.Weekday()
	if wd == time.Saturday || wd == time.Sunday {
		return false
	}
	openT := time.Date(local.Year(), local.Month(), local.Day(), 9, 30, 0, 0, eastern)
	closeT := time.Date(local.Year(), local.Month(), local.Day(), 16, 0, 0, 0, eastern)
	return !local.Before(openT) && local.Before(closeT)
}
