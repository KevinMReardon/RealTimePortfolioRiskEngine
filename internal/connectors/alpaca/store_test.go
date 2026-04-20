package alpaca

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestSyncStateStore_Upsert_requiresPortfolioID(t *testing.T) {
	s := &SyncStateStore{}
	err := s.Upsert(context.Background(), SyncState{})
	if err == nil {
		t.Fatal("expected error")
	}
	err = s.Upsert(context.Background(), SyncState{PortfolioID: uuid.New()})
	if err == nil {
		t.Fatal("expected error for nil pool")
	}
}
