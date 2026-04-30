package proposals

import "testing"

func TestKillSwitchInputs(t *testing.T) {
	t.Parallel()
	e, d := KillSwitchInputs(true, false, false)
	if !e || d {
		t.Fatalf("want env only: got env=%v db=%v", e, d)
	}
	e, d = KillSwitchInputs(false, true, true)
	if e || !d {
		t.Fatalf("want db only: got env=%v db=%v", e, d)
	}
	e, d = KillSwitchInputs(false, true, false) // row absent => db flag false
	if e || d {
		t.Fatalf("want no db when absent: got env=%v db=%v", e, d)
	}
}
