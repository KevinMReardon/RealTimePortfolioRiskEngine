package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
)

type fakePortfolioCatalogStore struct {
	rows         []events.PortfolioCatalogEntry
	err          error
	ownershipOK  bool
	ownershipSet bool
}

func (f *fakePortfolioCatalogStore) ListPortfolios(ctx context.Context) ([]events.PortfolioCatalogEntry, error) {
	if f.err != nil {
		return nil, f.err
	}
	return append([]events.PortfolioCatalogEntry(nil), f.rows...), nil
}

func (f *fakePortfolioCatalogStore) ListPortfoliosByOwner(ctx context.Context, ownerUserID uuid.UUID) ([]events.PortfolioCatalogEntry, error) {
	_ = ownerUserID
	return f.ListPortfolios(ctx)
}

func (f *fakePortfolioCatalogStore) CreatePortfolio(ctx context.Context, portfolioID uuid.UUID, name, baseCurrency string) (events.PortfolioCatalogEntry, error) {
	if f.err != nil {
		return events.PortfolioCatalogEntry{}, f.err
	}
	now := time.Now().UTC().Truncate(time.Second)
	row := events.PortfolioCatalogEntry{
		PortfolioID:  portfolioID,
		Name:         name,
		BaseCurrency: baseCurrency,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	f.rows = append([]events.PortfolioCatalogEntry{row}, f.rows...)
	return row, nil
}

func (f *fakePortfolioCatalogStore) CreatePortfolioForOwner(ctx context.Context, ownerUserID, portfolioID uuid.UUID, name, baseCurrency string) (events.PortfolioCatalogEntry, error) {
	_ = ownerUserID
	return f.CreatePortfolio(ctx, portfolioID, name, baseCurrency)
}

func (f *fakePortfolioCatalogStore) PortfolioOwnedByUser(ctx context.Context, portfolioID, ownerUserID uuid.UUID) (bool, error) {
	_ = ctx
	_ = portfolioID
	_ = ownerUserID
	if f.ownershipSet {
		return f.ownershipOK, nil
	}
	return true, nil
}

func TestPortfolioCatalog_ListAndCreate(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	store := &fakePortfolioCatalogStore{
		ownershipOK:  true,
		ownershipSet: true,
		rows: []events.PortfolioCatalogEntry{
			{
				PortfolioID:  uuid.MustParse("550e8400-e29b-41d4-a716-446655440111"),
				Name:         "Seed Portfolio",
				BaseCurrency: "USD",
				CreatedAt:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				UpdatedAt:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			},
		},
	}
	r := NewRouter(RouterConfig{
		Logger:                zap.NewNop(),
		ReadPortfolio:         &fakePortfolioReadStore{found: false},
		PortfolioCatalog:      store,
		PriceStreamPartitions: testPricePartitions,
	})

	t.Run("list_200", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/portfolios", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
		}
		var out struct {
			Portfolios []portfolioCatalogResponse `json:"portfolios"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(out.Portfolios) != 1 {
			t.Fatalf("portfolios len=%d", len(out.Portfolios))
		}
		if out.Portfolios[0].PortfolioID != "550e8400-e29b-41d4-a716-446655440111" {
			t.Fatalf("portfolio_id=%q", out.Portfolios[0].PortfolioID)
		}
	})

	t.Run("create_201", func(t *testing.T) {
		body := []byte(`{"name":"Growth Book","base_currency":"usd"}`)
		req := httptest.NewRequest(http.MethodPost, "/v1/portfolios", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
		}
		var out portfolioCatalogResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if _, err := uuid.Parse(out.PortfolioID); err != nil {
			t.Fatalf("portfolio_id parse: %v", err)
		}
		if out.Name != "Growth Book" || out.BaseCurrency != "USD" {
			t.Fatalf("row = %+v", out)
		}
	})

	t.Run("create_validation_400", func(t *testing.T) {
		body := []byte(`{"name":"   "}`)
		req := httptest.NewRequest(http.MethodPost, "/v1/portfolios", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(RequestIDHeader, fixedRequestID)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
		}
		assertAPIErrorEnvelope(t, rec.Body.Bytes(), ErrCodeValidation, "name is required", fixedRequestID)
	})
}

func TestPortfolioCatalog_StoreFailure(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	r := NewRouter(RouterConfig{
		Logger:                zap.NewNop(),
		ReadPortfolio:         &fakePortfolioReadStore{found: false},
		PortfolioCatalog:      &fakePortfolioCatalogStore{err: errors.New("db down")},
		PriceStreamPartitions: testPricePartitions,
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/portfolios", nil)
	req.Header.Set(RequestIDHeader, fixedRequestID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertAPIErrorEnvelope(t, rec.Body.Bytes(), ErrCodeInternal, "internal error", fixedRequestID)
}
