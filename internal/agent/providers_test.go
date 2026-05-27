package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/connectors/alpaca"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
)

type stubKeyLoader struct {
	mat    events.PortfolioAlpacaKeyMaterial
	linked bool
	err    error
}

func (s *stubKeyLoader) LoadPortfolioAlpacaKeyMaterial(_ context.Context, _ uuid.UUID) (events.PortfolioAlpacaKeyMaterial, bool, error) {
	return s.mat, s.linked, s.err
}

type stubREST struct{ acct alpaca.AccountSummary }

func (s *stubREST) GetAccount(_ context.Context) (alpaca.AccountSummary, error) { return s.acct, nil }
func (s *stubREST) ListPositions(_ context.Context) ([]alpaca.PositionRow, error) {
	return nil, nil
}
func (s *stubREST) ListActivities(_ context.Context, _ alpaca.ListActivitiesRequest) (alpaca.ActivitiesPage, error) {
	return alpaca.ActivitiesPage{}, nil
}
func (s *stubREST) ListOrders(_ context.Context, _ alpaca.ListOrdersRequest) ([]alpaca.OrderSnapshot, error) {
	return nil, nil
}
func (s *stubREST) PlaceOrder(_ context.Context, _ alpaca.PlaceOrderInput) (alpaca.OrderSnapshot, error) {
	return alpaca.OrderSnapshot{}, nil
}

func TestAlpacaBuyingPowerProvider_HappyPath(t *testing.T) {
	p := &AlpacaBuyingPowerProvider{
		Keys: &stubKeyLoader{linked: true, mat: events.PortfolioAlpacaKeyMaterial{KeyID: "k", SecretKey: "s", BaseURL: "https://paper-api.alpaca.markets"}},
		NewREST: func(_ alpaca.RESTConfig) (alpaca.REST, error) {
			return &stubREST{acct: alpaca.AccountSummary{BuyingPower: decimal.RequireFromString("12345.67")}}, nil
		},
	}
	bp, configured, err := p.GetBuyingPower(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !configured {
		t.Fatal("expected configured=true")
	}
	if bp != "12345.67" {
		t.Fatalf("buying power = %q, want 12345.67", bp)
	}
}

func TestAlpacaBuyingPowerProvider_NotLinked(t *testing.T) {
	p := &AlpacaBuyingPowerProvider{
		Keys:    &stubKeyLoader{linked: false},
		NewREST: func(_ alpaca.RESTConfig) (alpaca.REST, error) { return &stubREST{}, nil },
	}
	bp, configured, err := p.GetBuyingPower(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if configured {
		t.Fatal("expected configured=false when not linked")
	}
	if bp != "" {
		t.Fatalf("buying power should be empty when not linked; got %q", bp)
	}
}

func TestAlpacaBuyingPowerProvider_KeysError(t *testing.T) {
	p := &AlpacaBuyingPowerProvider{
		Keys:    &stubKeyLoader{err: errors.New("db down")},
		NewREST: func(_ alpaca.RESTConfig) (alpaca.REST, error) { return &stubREST{}, nil },
	}
	if _, _, err := p.GetBuyingPower(context.Background(), uuid.New()); err == nil {
		t.Fatal("expected key load error to propagate")
	}
}

func TestAlpacaBuyingPowerProvider_NilSafe(t *testing.T) {
	var p *AlpacaBuyingPowerProvider
	bp, configured, err := p.GetBuyingPower(context.Background(), uuid.New())
	if err != nil || configured || bp != "" {
		t.Fatalf("nil-safe path violated: err=%v configured=%v bp=%q", err, configured, bp)
	}
}
