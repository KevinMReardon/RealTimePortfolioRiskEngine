package alpaca

import (
	"context"
	"fmt"

	sdkalpaca "github.com/alpacahq/alpaca-trade-api-go/v3/alpaca"
	"github.com/shopspring/decimal"
)

// REST is a narrow read-only trading API surface (Phase 0). Implementations wrap the official SDK.
type REST interface {
	GetAccount(ctx context.Context) (AccountSummary, error)
	ListPositions(ctx context.Context) ([]PositionRow, error)
	ListActivities(ctx context.Context, req ListActivitiesRequest) (ActivitiesPage, error)
	ListOrders(ctx context.Context, req ListOrdersRequest) ([]OrderSnapshot, error)
}

// RESTClient wraps the Alpaca REST client behind the REST interface.
type RESTClient struct {
	sdk *sdkalpaca.Client
}

// NewREST constructs a REST client from config. Credentials and BaseURL map directly to sdkalpaca.ClientOpts.
func NewREST(cfg RESTConfig) (*RESTClient, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cli := sdkalpaca.NewClient(sdkalpaca.ClientOpts{
		APIKey:    cfg.KeyID,
		APISecret: cfg.SecretKey,
		BaseURL:   cfg.BaseURL,
	})
	return &RESTClient{sdk: cli}, nil
}

// GetAccount implements REST.
func (c *RESTClient) GetAccount(ctx context.Context) (AccountSummary, error) {
	if err := ctx.Err(); err != nil {
		return AccountSummary{}, err
	}
	acct, err := c.sdk.GetAccount()
	if err != nil {
		return AccountSummary{}, fmt.Errorf("alpaca GetAccount: %w", err)
	}
	return mapAccount(acct), nil
}

// ListPositions implements REST.
func (c *RESTClient) ListPositions(ctx context.Context) ([]PositionRow, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	pos, err := c.sdk.GetPositions()
	if err != nil {
		return nil, fmt.Errorf("alpaca GetPositions: %w", err)
	}
	out := make([]PositionRow, 0, len(pos))
	for i := range pos {
		out = append(out, mapPosition(&pos[i]))
	}
	return out, nil
}

// ListActivities implements REST.
func (c *RESTClient) ListActivities(ctx context.Context, req ListActivitiesRequest) (ActivitiesPage, error) {
	if err := ctx.Err(); err != nil {
		return ActivitiesPage{}, err
	}
	pageSize := req.PageSize
	if pageSize == 0 {
		pageSize = 100
	}
	sdkReq := sdkalpaca.GetAccountActivitiesRequest{
		ActivityTypes: req.ActivityTypes,
		Date:          req.Date,
		Until:         req.Until,
		After:         req.After,
		Direction:     req.Direction,
		PageSize:      pageSize,
		PageToken:     req.PageToken,
		Category:      req.Category,
	}
	acts, err := c.sdk.GetAccountActivities(sdkReq)
	if err != nil {
		return ActivitiesPage{}, fmt.Errorf("alpaca GetAccountActivities: %w", err)
	}
	out := make([]ActivityRow, 0, len(acts))
	for i := range acts {
		out = append(out, mapActivity(&acts[i]))
	}
	return ActivitiesPage{
		Activities:    out,
		NextPageToken: nextActivitiesPageToken(out, pageSize),
	}, nil
}

// ListOrders implements REST.
func (c *RESTClient) ListOrders(ctx context.Context, req ListOrdersRequest) ([]OrderSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sdkReq := sdkalpaca.GetOrdersRequest{
		Status:    req.Status,
		Limit:     req.Limit,
		After:     req.After,
		Until:     req.Until,
		Direction: req.Direction,
		Nested:    req.Nested,
		Side:      req.Side,
		Symbols:   req.Symbols,
	}
	orders, err := c.sdk.GetOrders(sdkReq)
	if err != nil {
		return nil, fmt.Errorf("alpaca GetOrders: %w", err)
	}
	out := make([]OrderSnapshot, 0, len(orders))
	for i := range orders {
		out = append(out, mapOrder(&orders[i]))
	}
	return out, nil
}

func mapAccount(a *sdkalpaca.Account) AccountSummary {
	return AccountSummary{
		ID:               a.ID,
		Status:           a.Status,
		Currency:         a.Currency,
		BuyingPower:      a.BuyingPower,
		Cash:             a.Cash,
		Equity:           a.Equity,
		PortfolioValue:   a.PortfolioValue,
		LongMarketValue:  a.LongMarketValue,
		ShortMarketValue: a.ShortMarketValue,
		PatternDayTrader: a.PatternDayTrader,
		TradingBlocked:   a.TradingBlocked,
		AccountBlocked:   a.AccountBlocked,
		CreatedAt:        a.CreatedAt,
	}
}

func mapPosition(p *sdkalpaca.Position) PositionRow {
	return PositionRow{
		Symbol:          p.Symbol,
		Qty:             p.Qty,
		QtyAvailable:    p.QtyAvailable,
		AvgEntryPrice:   p.AvgEntryPrice,
		MarketValue:     decimalPtrOrZero(p.MarketValue),
		CostBasis:       p.CostBasis,
		Side:            p.Side,
		AssetClass:      string(p.AssetClass),
		CurrentPrice:    decimalPtrOrZero(p.CurrentPrice),
		UnrealizedPL:    decimalPtrOrZero(p.UnrealizedPL),
		UnrealizedPLPct: decimalPtrOrZero(p.UnrealizedPLPC),
	}
}

func mapActivity(a *sdkalpaca.AccountActivity) ActivityRow {
	return ActivityRow{
		ID:              a.ID,
		ActivityType:    a.ActivityType,
		TransactionTime: a.TransactionTime,
		Symbol:          a.Symbol,
		Qty:             a.Qty,
		Price:           a.Price,
		Side:            a.Side,
		NetAmount:       a.NetAmount,
		OrderID:         a.OrderID,
		Description:     a.Description,
		CumQty:          a.CumQty,
		LeavesQty:       a.LeavesQty,
		PerShareAmount:  a.PerShareAmount,
	}
}

func mapOrder(o *sdkalpaca.Order) OrderSnapshot {
	snap := OrderSnapshot{
		ID:            o.ID,
		ClientOrderID: o.ClientOrderID,
		Symbol:        o.Symbol,
		Side:          string(o.Side),
		Type:          string(o.Type),
		Status:        o.Status,
		TimeInForce:   string(o.TimeInForce),
		AssetClass:    string(o.AssetClass),
		Qty:           decimalPtrOrZero(o.Qty),
		FilledQty:     o.FilledQty,
		SubmittedAt:   o.SubmittedAt,
		CreatedAt:     o.CreatedAt,
		UpdatedAt:     o.UpdatedAt,
	}
	if o.FilledAvgPrice != nil {
		snap.FilledAvgPrice = *o.FilledAvgPrice
	}
	if o.LimitPrice != nil {
		snap.LimitPrice = *o.LimitPrice
	}
	return snap
}

func decimalPtrOrZero(d *decimal.Decimal) decimal.Decimal {
	if d == nil {
		return decimal.Zero
	}
	return *d
}

// nextActivitiesPageToken follows Alpaca’s pagination hint: when a page is full,
// request the next page using the last activity’s ID as page_token.
func nextActivitiesPageToken(activities []ActivityRow, pageSize int) string {
	if pageSize <= 0 || len(activities) == 0 || len(activities) < pageSize {
		return ""
	}
	return activities[len(activities)-1].ID
}
