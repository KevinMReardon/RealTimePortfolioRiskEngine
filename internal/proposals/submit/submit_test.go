package submit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/connectors/alpaca"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/policy"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/portfolio"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/proposals"
)

func testPID() uuid.UUID {
	return uuid.MustParse("00000000-0000-0000-0000-0000000000b1")
}

func testPropID() uuid.UUID {
	return uuid.MustParse("00000000-0000-0000-0000-0000000000c1")
}

func assemblerPositiveEquity(sym string) portfolio.PortfolioAssemblerInput {
	price := decimal.RequireFromString("150")
	qty := decimal.RequireFromString("10")
	return portfolio.PortfolioAssemblerInput{
		PortfolioID: uuid.Nil,
		Positions: []portfolio.ProjectionRow{
			{Symbol: sym, Quantity: qty},
		},
		PriceBySymbol: map[string]portfolio.PriceMarkInput{
			sym: {Price: price},
		},
	}
}

func approvedMarketBuyProposal(pid, propID uuid.UUID) proposals.Proposal {
	qty := decimal.NewFromInt(1)
	side := "BUY"
	ot := "market"
	tif := "day"
	return proposals.Proposal{
		ProposalID:  propID,
		PortfolioID: pid,
		Symbol:      "AAPL",
		Side:        side,
		Quantity:    &qty,
		OrderType:   &ot,
		TimeInForce: &tif,
		PayloadHash: "deadbeef",
		RowVersion:  3,
		Status:      "approved",
	}
}

type fakeREST struct {
	acct     alpaca.AccountSummary
	acctErr  error
	order    alpaca.OrderSnapshot
	placeErr error

	lastPlace alpaca.PlaceOrderInput
}

func (f *fakeREST) GetAccount(ctx context.Context) (alpaca.AccountSummary, error) {
	if f.acctErr != nil {
		return alpaca.AccountSummary{}, f.acctErr
	}
	return f.acct, nil
}

func (f *fakeREST) ListPositions(ctx context.Context) ([]alpaca.PositionRow, error) {
	return nil, nil
}

func (f *fakeREST) ListActivities(ctx context.Context, req alpaca.ListActivitiesRequest) (alpaca.ActivitiesPage, error) {
	return alpaca.ActivitiesPage{}, nil
}

func (f *fakeREST) ListOrders(ctx context.Context, req alpaca.ListOrdersRequest) ([]alpaca.OrderSnapshot, error) {
	return nil, nil
}

func (f *fakeREST) PlaceOrder(ctx context.Context, in alpaca.PlaceOrderInput) (alpaca.OrderSnapshot, error) {
	f.lastPlace = in
	if f.placeErr != nil {
		return alpaca.OrderSnapshot{}, f.placeErr
	}
	if f.order.ID != "" || f.order.ClientOrderID != "" {
		return f.order, nil
	}
	return alpaca.OrderSnapshot{ID: "ord-test-1", ClientOrderID: in.ClientOrderID}, nil
}

type fakeStore struct {
	prop           proposals.Proposal
	getErr         error
	submitErr      error
	brokerErrCalls int
	submitCalls    int

	killActive    bool
	killPresent   bool
	killErr       error
	loadAnchorEq  decimal.Decimal
	loadAnchorOK  bool
	loadAnchorErr error
}

func (f *fakeStore) GetByIDForPortfolio(ctx context.Context, portfolioID, proposalID uuid.UUID) (proposals.Proposal, error) {
	if f.getErr != nil {
		return proposals.Proposal{}, f.getErr
	}
	return f.prop, nil
}

func (f *fakeStore) MarkProposalSubmitted(ctx context.Context, p proposals.SubmitSuccessParams) error {
	f.submitCalls++
	if f.submitErr != nil {
		return f.submitErr
	}
	return nil
}

func (f *fakeStore) MarkProposalBrokerError(ctx context.Context, portfolioID, proposalID uuid.UUID, msg string) error {
	f.brokerErrCalls++
	return nil
}

func (f *fakeStore) LoadEquityAnchorForPortfolioDate(ctx context.Context, portfolioID uuid.UUID, anchorDate time.Time) (decimal.Decimal, bool, error) {
	if f.loadAnchorErr != nil {
		return decimal.Zero, false, f.loadAnchorErr
	}
	return f.loadAnchorEq, f.loadAnchorOK, nil
}

func (f *fakeStore) KillSwitchLatestActive(ctx context.Context) (active bool, ok bool, err error) {
	if f.killErr != nil {
		return false, false, f.killErr
	}
	return f.killActive, f.killPresent, nil
}

type fakeRead struct {
	in  portfolio.PortfolioAssemblerInput
	ok  bool
	err error
}

func (f *fakeRead) LoadPortfolioAssemblerInput(ctx context.Context, portfolioID uuid.UUID) (portfolio.PortfolioAssemblerInput, bool, error) {
	if f.err != nil {
		return portfolio.PortfolioAssemblerInput{}, false, f.err
	}
	return f.in, f.ok, nil
}

type fakeKeys struct {
	mat    events.PortfolioAlpacaKeyMaterial
	linked bool
	err    error
}

func (f *fakeKeys) LoadPortfolioAlpacaKeyMaterial(ctx context.Context, portfolioID uuid.UUID) (events.PortfolioAlpacaKeyMaterial, bool, error) {
	if f.err != nil {
		return events.PortfolioAlpacaKeyMaterial{}, false, f.err
	}
	return f.mat, f.linked, nil
}

func goodAccount() alpaca.AccountSummary {
	return alpaca.AccountSummary{
		Equity:           decimal.RequireFromString("50000"),
		PatternDayTrader: false,
		TradingBlocked:   false,
		AccountBlocked:   false,
	}
}

func baseDeps(rest alpaca.REST, fs *fakeStore, fr *fakeRead) Deps {
	return Deps{
		Store:          fs,
		Read:           fr,
		Keys:           &fakeKeys{mat: events.PortfolioAlpacaKeyMaterial{KeyID: "k", SecretKey: "s", BaseURL: "https://paper-api.alpaca.markets"}, linked: true},
		Policy:         policy.Config{},
		TradingHaltEnv: false,
		Log:            zap.NewNop(),
		NewREST: func(cfg alpaca.RESTConfig) (alpaca.REST, error) {
			return rest, nil
		},
	}
}

func TestFromProposal_table(t *testing.T) {
	pid := testPID()
	propID := testPropID()
	baseProp := approvedMarketBuyProposal(pid, propID)

	ctx := context.Background()

	tests := []struct {
		name  string
		prop  proposals.Proposal
		opts  Options
		deps  func(*fakeStore, *fakeRead, *fakeREST) Deps
		want  Outcome
		check func(t *testing.T, fs *fakeStore, fr *fakeREST)
	}{
		{
			name: "version_mismatch",
			prop: baseProp,
			opts: Options{PayloadHash: strPtr("other")},
			deps: func(fs *fakeStore, fr *fakeRead, rest *fakeREST) Deps {
				return baseDeps(rest, fs, fr)
			},
			want: OutcomeVersionMismatch,
		},
		{
			name: "no_keys",
			prop: baseProp,
			deps: func(fs *fakeStore, fr *fakeRead, rest *fakeREST) Deps {
				d := baseDeps(rest, fs, fr)
				d.Keys = &fakeKeys{linked: false}
				return d
			},
			want: OutcomeNoKeys,
		},
		{
			name: "keys_error",
			prop: baseProp,
			deps: func(fs *fakeStore, fr *fakeRead, rest *fakeREST) Deps {
				d := baseDeps(rest, fs, fr)
				d.Keys = &fakeKeys{err: errors.New("db down")}
				return d
			},
			want: OutcomeError,
		},
		{
			name: "rest_factory_error",
			prop: baseProp,
			deps: func(fs *fakeStore, fr *fakeRead, rest *fakeREST) Deps {
				d := baseDeps(rest, fs, fr)
				d.NewREST = func(cfg alpaca.RESTConfig) (alpaca.REST, error) {
					return nil, errors.New("bad cfg")
				}
				return d
			},
			want: OutcomeError,
		},
		{
			name: "get_account_error",
			prop: baseProp,
			deps: func(fs *fakeStore, fr *fakeRead, rest *fakeREST) Deps {
				rest.acctErr = errors.New("timeout")
				return baseDeps(rest, fs, fr)
			},
			want: OutcomeAlpacaAccountError,
			check: func(t *testing.T, fs *fakeStore, _ *fakeREST) {
				if fs.brokerErrCalls != 1 {
					t.Fatalf("brokerErrCalls=%d want 1", fs.brokerErrCalls)
				}
			},
		},
		{
			name: "account_blocked",
			prop: baseProp,
			deps: func(fs *fakeStore, fr *fakeRead, rest *fakeREST) Deps {
				rest.acct = goodAccount()
				rest.acct.TradingBlocked = true
				return baseDeps(rest, fs, fr)
			},
			want: OutcomeAccountBlocked,
		},
		{
			name: "assembler_missing",
			prop: baseProp,
			deps: func(fs *fakeStore, fr *fakeRead, rest *fakeREST) Deps {
				fr.ok = false
				return baseDeps(rest, fs, fr)
			},
			want: OutcomeError,
		},
		{
			name: "assembler_error",
			prop: baseProp,
			deps: func(fs *fakeStore, fr *fakeRead, rest *fakeREST) Deps {
				fr.err = errors.New("read fail")
				return baseDeps(rest, fs, fr)
			},
			want: OutcomeError,
		},
		{
			name: "kill_switch_db_error",
			prop: baseProp,
			deps: func(fs *fakeStore, fr *fakeRead, rest *fakeREST) Deps {
				fs.killErr = errors.New("ks fail")
				return baseDeps(rest, fs, fr)
			},
			want: OutcomeError,
		},
		{
			name: "bad_intent",
			prop: func() proposals.Proposal {
				p := approvedMarketBuyProposal(pid, propID)
				p.Side = "INVALID"
				return p
			}(),
			deps: func(fs *fakeStore, fr *fakeRead, rest *fakeREST) Deps {
				return baseDeps(rest, fs, fr)
			},
			want: OutcomeBadIntent,
		},
		{
			name: "policy_denied_kill_env",
			prop: baseProp,
			deps: func(fs *fakeStore, fr *fakeRead, rest *fakeREST) Deps {
				d := baseDeps(rest, fs, fr)
				d.TradingHaltEnv = true
				return d
			},
			want: OutcomePolicyDenied,
		},
		{
			name: "notional_below_minimum",
			prop: func() proposals.Proposal {
				p := approvedMarketBuyProposal(pid, propID)
				n := decimal.RequireFromString("0.50")
				p.Quantity = nil
				p.NotionalUSD = &n
				return p
			}(),
			deps: func(fs *fakeStore, fr *fakeRead, rest *fakeREST) Deps {
				return baseDeps(rest, fs, fr)
			},
			want: OutcomeNotionalBelowMinimum,
		},
		{
			name: "place_order_error",
			prop: baseProp,
			deps: func(fs *fakeStore, fr *fakeRead, rest *fakeREST) Deps {
				rest.placeErr = errors.New("reject")
				return baseDeps(rest, fs, fr)
			},
			want: OutcomeAlpacaPlaceError,
			check: func(t *testing.T, fs *fakeStore, _ *fakeREST) {
				if fs.brokerErrCalls != 1 {
					t.Fatalf("brokerErrCalls=%d want 1", fs.brokerErrCalls)
				}
			},
		},
		{
			name: "success",
			prop: baseProp,
			deps: func(fs *fakeStore, fr *fakeRead, rest *fakeREST) Deps {
				rest.order = alpaca.OrderSnapshot{ID: "ord-xyz"}
				return baseDeps(rest, fs, fr)
			},
			want: OutcomeSuccess,
			check: func(t *testing.T, fs *fakeStore, rest *fakeREST) {
				if fs.submitCalls != 1 {
					t.Fatalf("submitCalls=%d want 1", fs.submitCalls)
				}
				if rest.lastPlace.Symbol != "AAPL" {
					t.Fatalf("symbol=%q", rest.lastPlace.Symbol)
				}
			},
		},
		{
			name: "conflict_after_broker",
			prop: baseProp,
			deps: func(fs *fakeStore, fr *fakeRead, rest *fakeREST) Deps {
				fs.submitErr = proposals.ErrSubmitConflict
				return baseDeps(rest, fs, fr)
			},
			want: OutcomeConflictAfterBroker,
			check: func(t *testing.T, fs *fakeStore, _ *fakeREST) {
				if fs.submitCalls != 1 {
					t.Fatalf("submitCalls=%d want 1", fs.submitCalls)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fs := &fakeStore{prop: tc.prop}
			fr := &fakeRead{in: assemblerPositiveEquity("AAPL"), ok: true}
			rest := &fakeREST{acct: goodAccount()}
			deps := tc.deps(fs, fr, rest)
			got := FromProposal(ctx, deps, tc.prop, tc.opts)
			if got.Outcome != tc.want {
				t.Fatalf("outcome=%q want %q full=%+v", got.Outcome, tc.want, got)
			}
			if tc.check != nil {
				tc.check(t, fs, rest)
			}
		})
	}
}

func TestFromStore_not_found(t *testing.T) {
	fs := &fakeStore{getErr: proposals.ErrProposalNotFound}
	deps := baseDeps(&fakeREST{}, fs, &fakeRead{})
	res := FromStore(context.Background(), deps, testPID(), testPropID(), Options{})
	if res.Outcome != OutcomeNotFound {
		t.Fatalf("got %q", res.Outcome)
	}
}

func TestFromStore_wrong_status(t *testing.T) {
	p := approvedMarketBuyProposal(testPID(), testPropID())
	p.Status = "proposed"
	fs := &fakeStore{prop: p}
	deps := baseDeps(&fakeREST{}, fs, &fakeRead{})
	res := FromStore(context.Background(), deps, testPID(), testPropID(), Options{})
	if res.Outcome != OutcomeBadStatus {
		t.Fatalf("got %q", res.Outcome)
	}
}

func TestFromStore_success(t *testing.T) {
	pid := testPID()
	propID := testPropID()
	p := approvedMarketBuyProposal(pid, propID)
	fs := &fakeStore{prop: p}
	fr := &fakeRead{in: assemblerPositiveEquity("AAPL"), ok: true}
	rest := &fakeREST{acct: goodAccount(), order: alpaca.OrderSnapshot{ID: "ok"}}
	deps := baseDeps(rest, fs, fr)
	res := FromStore(context.Background(), deps, pid, propID, Options{})
	if res.Outcome != OutcomeSuccess || res.BrokerOrderID != "ok" {
		t.Fatalf("got %+v", res)
	}
}

func strPtr(s string) *string { return &s }
