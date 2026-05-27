package alpaca

import (
	"testing"

	"github.com/shopspring/decimal"
	sdkalpaca "github.com/alpacahq/alpaca-trade-api-go/v3/alpaca"
)

func TestPlaceOrderInputToSDK_marketBuyQty(t *testing.T) {
	q := decimal.RequireFromString("1.5")
	req, err := placeOrderInputToSDK(PlaceOrderInput{
		Symbol:        "AAPL",
		Side:          "BUY",
		Qty:           &q,
		ClientOrderID: "rtp-testclient",
	})
	if err != nil {
		t.Fatal(err)
	}
	if req.Symbol != "AAPL" || req.Side != sdkalpaca.Buy || req.Type != sdkalpaca.Market || req.TimeInForce != sdkalpaca.Day {
		t.Fatalf("unexpected req: %+v", req)
	}
	if req.Qty == nil || !req.Qty.Equal(q) {
		t.Fatalf("qty %+v", req.Qty)
	}
}

func TestPlaceOrderInputToSDK_limitRequiresPrice(t *testing.T) {
	q := decimal.RequireFromString("1")
	_, err := placeOrderInputToSDK(PlaceOrderInput{
		Symbol:        "AAPL",
		Side:          "SELL",
		Qty:           &q,
		OrderType:     "limit",
		ClientOrderID: "cid",
	})
	if err == nil {
		t.Fatal("expected error for limit without limit_price")
	}
}

func TestPlaceOrderInputToSDK_bracketRequiresLegs(t *testing.T) {
	q := decimal.RequireFromString("1")
	_, err := placeOrderInputToSDK(PlaceOrderInput{
		Symbol:        "AAPL",
		Side:          "BUY",
		Qty:           &q,
		OrderClass:    "bracket",
		ClientOrderID: "cid",
	})
	if err == nil {
		t.Fatal("expected error for bracket without stop/take-profit")
	}
}

func TestPlaceOrderInputToSDK_bracketSuccess(t *testing.T) {
	q := decimal.RequireFromString("1")
	stop := decimal.RequireFromString("120.5")
	take := decimal.RequireFromString("165.25")
	req, err := placeOrderInputToSDK(PlaceOrderInput{
		Symbol:                "AAPL",
		Side:                  "BUY",
		Qty:                   &q,
		OrderClass:            "bracket",
		StopLossStopPrice:     &stop,
		TakeProfitLimitPrice:  &take,
		ClientOrderID:         "cid",
	})
	if err != nil {
		t.Fatal(err)
	}
	if req.OrderClass != sdkalpaca.Bracket {
		t.Fatalf("order class=%q", req.OrderClass)
	}
	if req.StopLoss == nil || req.TakeProfit == nil {
		t.Fatalf("expected stop_loss and take_profit populated")
	}
}
