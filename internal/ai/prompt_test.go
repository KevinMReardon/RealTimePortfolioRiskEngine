package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildUserContent_prettyShapeAndScenarioStub(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
  "portfolio_id": "p1",
  "portfolio": {"totals": {"market_value": "100"}},
  "risk": {"var_95_1d": "5"},
  "recent_events": [{"event_type": "TradeExecuted"}],
  "client_payload": {"x": 1}
}`)
	out, err := BuildUserContent(raw)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if _, ok := m["portfolio_id"]; ok {
		t.Fatal("portfolio_id should be stripped from CONTEXT")
	}
	if _, ok := m["client_payload"]; ok {
		t.Fatal("client_payload should be stripped from CONTEXT")
	}
	if m["scenario"] != nil {
		t.Fatalf("scenario = %v want null", m["scenario"])
	}
	if !strings.Contains(out, "\n") {
		t.Fatal("expected pretty-printed (indented) JSON")
	}
}

func TestBuildUserContent_emptyInput(t *testing.T) {
	t.Parallel()
	_, err := BuildUserContent(nil)
	if err == nil {
		t.Fatal("expected error")
	}
}
