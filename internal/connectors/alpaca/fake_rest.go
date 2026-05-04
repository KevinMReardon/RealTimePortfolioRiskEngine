package alpaca

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// FakeREST is a test double implementing REST with static responses.
type FakeREST struct {
	Account            AccountSummary
	Positions          []PositionRow
	ActivitiesPages    []ActivitiesPage
	Orders             []OrderSnapshot
	ListActivitiesErr  error
	ListPositionsErr   error
	GetAccountErr      error
	ListOrdersErr      error
	ActivitiesCallIdx  int
	PlaceOrderFunc       func(ctx context.Context, in PlaceOrderInput) (OrderSnapshot, error)
	PlaceOrderErr        error
	lastPlaceOrderInput PlaceOrderInput
}

// GetAccount implements REST.
func (f *FakeREST) GetAccount(ctx context.Context) (AccountSummary, error) {
	if err := ctx.Err(); err != nil {
		return AccountSummary{}, err
	}
	if f.GetAccountErr != nil {
		return AccountSummary{}, f.GetAccountErr
	}
	return f.Account, nil
}

// ListPositions implements REST.
func (f *FakeREST) ListPositions(ctx context.Context) ([]PositionRow, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.ListPositionsErr != nil {
		return nil, f.ListPositionsErr
	}
	return f.Positions, nil
}

// ListActivities implements REST; when ActivitiesPages is set, returns successive pages by call index.
func (f *FakeREST) ListActivities(ctx context.Context, req ListActivitiesRequest) (ActivitiesPage, error) {
	if err := ctx.Err(); err != nil {
		return ActivitiesPage{}, err
	}
	if f.ListActivitiesErr != nil {
		return ActivitiesPage{}, f.ListActivitiesErr
	}
	if len(f.ActivitiesPages) == 0 {
		return ActivitiesPage{}, nil
	}
	idx := f.ActivitiesCallIdx
	if idx >= len(f.ActivitiesPages) {
		return ActivitiesPage{}, errors.New("fake: no more activity pages")
	}
	f.ActivitiesCallIdx++
	return f.ActivitiesPages[idx], nil
}

// ListOrders implements REST.
func (f *FakeREST) ListOrders(ctx context.Context, req ListOrdersRequest) ([]OrderSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.ListOrdersErr != nil {
		return nil, f.ListOrdersErr
	}
	return f.Orders, nil
}

// LastPlaceOrderInput returns the last PlaceOrder input (for tests); zero value if never called.
func (f *FakeREST) LastPlaceOrderInput() PlaceOrderInput {
	return f.lastPlaceOrderInput
}

// PlaceOrder implements REST.
func (f *FakeREST) PlaceOrder(ctx context.Context, in PlaceOrderInput) (OrderSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return OrderSnapshot{}, err
	}
	if f.PlaceOrderFunc != nil {
		return f.PlaceOrderFunc(ctx, in)
	}
	if f.PlaceOrderErr != nil {
		return OrderSnapshot{}, f.PlaceOrderErr
	}
	f.lastPlaceOrderInput = in
	return OrderSnapshot{
		ID:             "fake-order-" + uuid.New().String(),
		ClientOrderID:  in.ClientOrderID,
		Symbol:         in.Symbol,
		Side:           in.Side,
		Type:           in.OrderType,
		Status:         "accepted",
		TimeInForce:    in.TimeInForce,
		SubmittedAt:    time.Now().UTC(),
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}, nil
}
