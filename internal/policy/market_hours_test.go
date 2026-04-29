package policy

import (
	"testing"
	"time"
)

func TestIsUSRegularSessionEquities(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	// Wednesday 2020-01-08
	if !IsUSRegularSessionEquities(time.Date(2020, 1, 8, 10, 0, 0, 0, ny)) {
		t.Fatal("10:00 Wed should be in session")
	}
	if IsUSRegularSessionEquities(time.Date(2020, 1, 8, 9, 29, 0, 0, ny)) {
		t.Fatal("pre-open should be closed")
	}
	if IsUSRegularSessionEquities(time.Date(2020, 1, 8, 16, 0, 0, 0, ny)) {
		t.Fatal("at 16:00 should be closed (half-open interval)")
	}
	if IsUSRegularSessionEquities(time.Date(2020, 1, 11, 12, 0, 0, 0, ny)) {
		t.Fatal("Saturday should be closed")
	}
}
