package events

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCompareCursors(t *testing.T) {
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	id1 := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	id2 := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	a := Cursor{Time: t1, ID: id2}
	b := Cursor{Time: t1, ID: id1}
	if CompareCursors(a, b) != 1 {
		t.Fatalf("same time id order")
	}
	if CompareCursors(b, a) != -1 {
		t.Fatalf("reverse")
	}
	if CompareCursors(a, a) != 0 {
		t.Fatalf("equal")
	}
	if CompareCursors(Cursor{Time: t1, ID: id1}, Cursor{Time: t2, ID: id1}) != -1 {
		t.Fatalf("time order")
	}
}
