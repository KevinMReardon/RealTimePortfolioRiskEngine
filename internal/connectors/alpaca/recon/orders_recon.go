package recon

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/connectors/alpaca"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/proposals"
)

type Target struct {
	PortfolioID uuid.UUID
	KeyID       string
	SecretKey   string
	BaseURL     string
}

type TargetLister interface {
	ListTargets(ctx context.Context) ([]Target, error)
}

type ProposalStore interface {
	ListByPortfolio(ctx context.Context, portfolioID uuid.UUID, filter proposals.ListFilter) ([]proposals.Proposal, error)
	MarkProposalFilled(ctx context.Context, portfolioID, proposalID uuid.UUID, brokerOrderID string) error
	MarkProposalCancelled(ctx context.Context, portfolioID, proposalID uuid.UUID, brokerOrderID, reason string) error
}

type RESTFactory func(cfg alpaca.RESTConfig) (alpaca.REST, error)

type Worker struct {
	Store      ProposalStore
	Targets    TargetLister
	NewREST    RESTFactory
	Interval   time.Duration
	OrdersLookback time.Duration
	Log        *zap.Logger
}

func (w *Worker) Run(ctx context.Context) {
	if w == nil || w.Store == nil || w.Targets == nil || w.NewREST == nil {
		return
	}
	interval := w.Interval
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	w.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

func (w *Worker) tick(ctx context.Context) {
	targets, err := w.Targets.ListTargets(ctx)
	if err != nil {
		w.logger().Warn("orders_recon_targets_failed", zap.Error(err))
		return
	}
	for _, target := range targets {
		if ctx.Err() != nil {
			return
		}
		w.reconcilePortfolio(ctx, target)
	}
}

func (w *Worker) reconcilePortfolio(ctx context.Context, t Target) {
	rest, err := w.NewREST(alpaca.RESTConfig{
		KeyID:     strings.TrimSpace(t.KeyID),
		SecretKey: strings.TrimSpace(t.SecretKey),
		BaseURL:   strings.TrimSpace(t.BaseURL),
	})
	if err != nil {
		w.logger().Warn("orders_recon_rest_failed", zap.String("portfolio_id", t.PortfolioID.String()), zap.Error(err))
		return
	}
	status := "submitted"
	props, err := w.Store.ListByPortfolio(ctx, t.PortfolioID, proposals.ListFilter{Status: &status})
	if err != nil {
		w.logger().Warn("orders_recon_list_proposals_failed", zap.String("portfolio_id", t.PortfolioID.String()), zap.Error(err))
		return
	}
	if len(props) == 0 {
		return
	}
	lookback := w.OrdersLookback
	if lookback <= 0 {
		lookback = 48 * time.Hour
	}
	orders, err := rest.ListOrders(ctx, alpaca.ListOrdersRequest{
		Status: "all",
		Limit:  500,
		After:  time.Now().UTC().Add(-lookback),
	})
	if err != nil {
		w.logger().Warn("orders_recon_list_orders_failed", zap.String("portfolio_id", t.PortfolioID.String()), zap.Error(err))
		return
	}
	orderByID := make(map[string]alpaca.OrderSnapshot, len(orders))
	for _, o := range orders {
		id := strings.TrimSpace(o.ID)
		if id != "" {
			orderByID[id] = o
		}
		coid := strings.TrimSpace(o.ClientOrderID)
		if coid != "" {
			orderByID[coid] = o
		}
	}
	for _, p := range props {
		if p.BrokerOrderID == nil {
			continue
		}
		bid := strings.TrimSpace(*p.BrokerOrderID)
		if bid == "" {
			continue
		}
		o, ok := orderByID[bid]
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(o.Status)) {
		case "filled":
			if err := w.Store.MarkProposalFilled(ctx, p.PortfolioID, p.ProposalID, bid); err != nil {
				w.logger().Warn("orders_recon_mark_filled_failed",
					zap.String("portfolio_id", p.PortfolioID.String()),
					zap.String("proposal_id", p.ProposalID.String()),
					zap.String("broker_order_id", bid),
					zap.Error(err),
				)
			}
		case "canceled", "cancelled", "expired", "rejected":
			if err := w.Store.MarkProposalCancelled(ctx, p.PortfolioID, p.ProposalID, bid, "broker_"+strings.ToLower(strings.TrimSpace(o.Status))); err != nil {
				w.logger().Warn("orders_recon_mark_cancelled_failed",
					zap.String("portfolio_id", p.PortfolioID.String()),
					zap.String("proposal_id", p.ProposalID.String()),
					zap.String("broker_order_id", bid),
					zap.Error(err),
				)
			}
		}
	}
}

func (w *Worker) logger() *zap.Logger {
	if w.Log == nil {
		return zap.NewNop()
	}
	return w.Log
}
