package agent

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/connectors/alpaca"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
)

// AlpacaBuyingPowerProvider implements BuyingPowerProvider by loading per-portfolio Alpaca
// credentials and calling GetAccount. Returns ("", false, nil) when the portfolio is not linked.
type AlpacaBuyingPowerProvider struct {
	Keys    AlpacaKeyLoader
	NewREST AlpacaRESTFactory
}

// AlpacaKeyLoader mirrors submit.AlpacaKeyLoader without importing the submit package here.
type AlpacaKeyLoader interface {
	LoadPortfolioAlpacaKeyMaterial(ctx context.Context, portfolioID uuid.UUID) (events.PortfolioAlpacaKeyMaterial, bool, error)
}

// AlpacaRESTFactory builds an alpaca.REST client from key material.
type AlpacaRESTFactory func(cfg alpaca.RESTConfig) (alpaca.REST, error)

// GetBuyingPower implements BuyingPowerProvider.
func (p *AlpacaBuyingPowerProvider) GetBuyingPower(ctx context.Context, portfolioID uuid.UUID) (string, bool, error) {
	if p == nil || p.Keys == nil || p.NewREST == nil {
		return "", false, nil
	}
	keys, linked, err := p.Keys.LoadPortfolioAlpacaKeyMaterial(ctx, portfolioID)
	if err != nil {
		return "", false, err
	}
	if !linked {
		return "", false, nil
	}
	baseURL := strings.TrimSpace(keys.BaseURL)
	if baseURL == "" {
		if strings.EqualFold(keys.AccountMode, "live") {
			baseURL = alpaca.DefaultRESTBaseURLLive
		} else {
			baseURL = alpaca.DefaultRESTBaseURLPaper
		}
	}
	rest, err := p.NewREST(alpaca.RESTConfig{KeyID: keys.KeyID, SecretKey: keys.SecretKey, BaseURL: baseURL})
	if err != nil {
		return "", false, err
	}
	acct, err := rest.GetAccount(ctx)
	if err != nil {
		return "", false, err
	}
	return acct.BuyingPower.String(), true, nil
}
