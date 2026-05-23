package evaluation

import (
	"context"
	"sync"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/connectors/alpaca"
	"github.com/shopspring/decimal"
)

// MockREST is a deterministic in-memory Alpaca REST stub for replay tests (no network).
type MockREST struct {
	mu sync.Mutex

	Orders []alpaca.OrderSnapshot
	Fills  []decimal.Decimal // per PlaceOrder notional PnL delta (scripted)
}

func NewMockREST() *MockREST {
	return &MockREST{}
}

func (m *MockREST) GetAccount(ctx context.Context) (alpaca.AccountSummary, error) {
	return alpaca.AccountSummary{Equity: decimal.NewFromInt(100000)}, nil
}

func (m *MockREST) ListPositions(ctx context.Context) ([]alpaca.PositionRow, error) {
	return nil, nil
}

func (m *MockREST) ListActivities(ctx context.Context, req alpaca.ListActivitiesRequest) (alpaca.ActivitiesPage, error) {
	return alpaca.ActivitiesPage{}, nil
}

func (m *MockREST) ListOrders(ctx context.Context, req alpaca.ListOrdersRequest) ([]alpaca.OrderSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]alpaca.OrderSnapshot, len(m.Orders))
	copy(out, m.Orders)
	return out, nil
}

func (m *MockREST) PlaceOrder(ctx context.Context, in alpaca.PlaceOrderInput) (alpaca.OrderSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := "mock-" + decimal.NewFromInt(int64(len(m.Orders) + 1)).String()
	snap := alpaca.OrderSnapshot{ID: id, ClientOrderID: in.ClientOrderID, Symbol: in.Symbol}
	m.Orders = append(m.Orders, snap)
	return snap, nil
}
