package alpaca

import (
	"time"

	"github.com/shopspring/decimal"
)

// MinNotionalStockOrderUSD is Alpaca's minimum USD notional for notional-sized US equity orders
// (HTTP 422, code 42210000: "notional amount must be >= 1.00").
var MinNotionalStockOrderUSD = decimal.RequireFromString("1")

// AccountSummary is a read-only snapshot of account fields commonly used for sync and risk.
type AccountSummary struct {
	ID               string
	Status           string
	Currency         string
	BuyingPower      decimal.Decimal
	Cash             decimal.Decimal
	Equity           decimal.Decimal
	PortfolioValue   decimal.Decimal
	LongMarketValue  decimal.Decimal
	ShortMarketValue decimal.Decimal
	PatternDayTrader bool
	TradingBlocked   bool
	AccountBlocked   bool
	CreatedAt        time.Time
}

// PositionRow is an open position from the broker.
type PositionRow struct {
	Symbol          string
	Qty             decimal.Decimal
	QtyAvailable    decimal.Decimal
	AvgEntryPrice   decimal.Decimal
	MarketValue     decimal.Decimal
	CostBasis       decimal.Decimal
	Side            string
	AssetClass      string
	CurrentPrice    decimal.Decimal
	UnrealizedPL    decimal.Decimal
	UnrealizedPLPct decimal.Decimal
}

// ActivityRow is one account activity (fills, dividends, etc.).
type ActivityRow struct {
	ID               string
	ActivityType     string
	TransactionTime  time.Time
	Symbol           string
	Qty              decimal.Decimal
	Price            decimal.Decimal
	Side             string
	NetAmount        decimal.Decimal
	OrderID          string
	Description      string
	CumQty           decimal.Decimal
	LeavesQty        decimal.Decimal
	PerShareAmount   decimal.Decimal
}

// ListActivitiesRequest mirrors Alpaca GET /v2/account/activities query parameters.
type ListActivitiesRequest struct {
	ActivityTypes []string
	After         time.Time
	Until         time.Time
	Date          time.Time
	Direction     string
	PageSize      int
	PageToken     string
	Category      string
}

// ActivitiesPage is one page of activities plus the token for the next page (if any).
// Alpaca pagination: pass PageToken from the previous response’s NextPageToken to get the following page.
// NextPageToken is derived from the last activity ID when the page is full (see Alpaca docs).
type ActivitiesPage struct {
	Activities    []ActivityRow
	NextPageToken string
}

// ListOrdersRequest mirrors Alpaca GET /v2/orders query parameters (read-only use).
type ListOrdersRequest struct {
	Status    string
	Limit     int
	After     time.Time
	Until     time.Time
	Direction string
	Nested    bool
	Side      string
	Symbols   []string
}

// PlaceOrderInput is a minimal Alpaca POST /v2/orders request (equities; market or limit).
type PlaceOrderInput struct {
	Symbol        string
	Side          string // BUY | SELL
	Qty           *decimal.Decimal
	NotionalUSD   *decimal.Decimal
	OrderType     string // market | limit (empty defaults to market)
	TimeInForce   string // day | gtc | ... (empty defaults to day)
	LimitPrice    *decimal.Decimal
	ClientOrderID string
	OrderClass    string // simple | bracket (empty defaults to simple)
	// Bracket legs (used when OrderClass=bracket).
	TakeProfitLimitPrice *decimal.Decimal
	StopLossStopPrice    *decimal.Decimal
}

// OrderSnapshot is a read-only view of an order.
type OrderSnapshot struct {
	ID             string
	ClientOrderID  string
	Symbol         string
	Side           string
	Type           string
	Status         string
	TimeInForce    string
	AssetClass     string
	Qty            decimal.Decimal
	FilledQty      decimal.Decimal
	FilledAvgPrice decimal.Decimal
	LimitPrice     decimal.Decimal
	SubmittedAt    time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// StreamBar is a normalized minute (or aggregate) bar from the market data stream.
type StreamBar struct {
	Symbol    string
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    uint64
	VWAP      float64
	Timestamp time.Time
}
