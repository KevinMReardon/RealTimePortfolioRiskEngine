package pricesource

import "testing"

func TestNormalizeToInternalSymbol(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "equity upper", in: "AAPL", want: "AAPL"},
		{name: "fx slash", in: "EUR/USD", want: "EUR-USD"},
		{name: "crypto colon", in: "BINANCE:BTC/USDT", want: "BINANCE-BTC-USDT"},
		{name: "trim and upper", in: " msft ", want: "MSFT"},
		{name: "invalid symbol", in: "$$$", wantErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeToInternalSymbol(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got symbol=%q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
