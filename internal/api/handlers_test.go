package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/config"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/domain"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/ingestion"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/portfolio"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/scenario"
)

var testPricePartitions = config.DerivePriceStreamPartitions(uuid.MustParse("00000000-0000-4000-8000-000000000001"), 4)

type fakeIngestStore struct {
	appended []domain.EventEnvelope
	err      error
}

func (f *fakeIngestStore) Append(ctx context.Context, e domain.EventEnvelope) (events.AppendResult, error) {
	if f.err != nil {
		return events.AppendResult{}, f.err
	}
	f.appended = append(f.appended, e)
	return events.AppendResult{EventID: e.EventID, Inserted: true}, nil
}

type idempotentFakeStore struct {
	byKey map[string]uuid.UUID
}

func (f *idempotentFakeStore) Append(ctx context.Context, e domain.EventEnvelope) (events.AppendResult, error) {
	key := e.PortfolioID + "\x00" + e.IdempotencyKey
	if id, ok := f.byKey[key]; ok {
		return events.AppendResult{EventID: id, Inserted: false}, nil
	}
	f.byKey[key] = e.EventID
	return events.AppendResult{EventID: e.EventID, Inserted: true}, nil
}

func testRouter(logger *zap.Logger, svc ingestion.Service) *gin.Engine {
	return NewRouter(RouterConfig{Logger: logger, Ingest: svc, PriceStreamPartitions: testPricePartitions})
}

func TestPostTrade_HTTPValidationMapping(t *testing.T) {
	t.Parallel()
	logger := zap.NewNop()
	store := &fakeIngestStore{}
	svc := ingestion.NewService(store)

	router := testRouter(logger, svc)

	t.Run("invalid_json_body", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodPost, "/v1/trades", bytes.NewReader([]byte(`not-json`)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d want %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("zero_quantity_ErrValidation_400", func(t *testing.T) {
		t.Parallel()
		body := map[string]any{
			"portfolio_id":    uuid.MustParse("550e8400-e29b-41d4-a716-446655440000").String(),
			"idempotency_key": "k1",
			"source":          "trade_api",
			"trade": map[string]any{
				"trade_id": "t1",
				"symbol":   "AAPL",
				"side":     "BUY",
				"quantity": "0",
				"price":    "100",
				"currency": "USD",
			},
		}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/v1/trades", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d want %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("valid_trade_201", func(t *testing.T) {
		t.Parallel()
		singleStore := &fakeIngestStore{}
		singleSvc := ingestion.NewService(singleStore)
		r := testRouter(logger, singleSvc)

		body := map[string]any{
			"portfolio_id":    uuid.MustParse("550e8400-e29b-41d4-a716-446655440000").String(),
			"idempotency_key": "k2",
			"source":          "trade_api",
			"trade": map[string]any{
				"trade_id": "t1",
				"symbol":   "AAPL",
				"side":     "BUY",
				"quantity": "10",
				"price":    "100.5",
				"currency": "USD",
			},
		}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/v1/trades", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d want %d body=%s", rec.Code, http.StatusCreated, rec.Body.String())
		}
		if len(singleStore.appended) != 1 {
			t.Fatalf("appended = %d want 1", len(singleStore.appended))
		}
		if singleStore.appended[0].EventType != domain.EventTypeTradeExecuted {
			t.Fatalf("event type = %s", singleStore.appended[0].EventType)
		}
	})

	t.Run("trade_reserved_price_partition_400", func(t *testing.T) {
		t.Parallel()
		r := testRouter(logger, svc)
		body := map[string]any{
			"portfolio_id":    testPricePartitions[0].String(),
			"idempotency_key": "k-res",
			"source":          "trade_api",
			"trade": map[string]any{
				"trade_id": "t1",
				"symbol":   "AAPL",
				"side":     "BUY",
				"quantity": "1",
				"price":    "1",
				"currency": "USD",
			},
		}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/v1/trades", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
		}
	})

	t.Run("idempotent_duplicate_200_same_event_id", func(t *testing.T) {
		t.Parallel()
		dupStore := &idempotentFakeStore{byKey: make(map[string]uuid.UUID)}
		dupSvc := ingestion.NewService(dupStore)
		r := testRouter(logger, dupSvc)

		body := map[string]any{
			"portfolio_id":    uuid.MustParse("550e8400-e29b-41d4-a716-446655440020").String(),
			"idempotency_key": "idem-dup",
			"source":          "trade_api",
			"trade": map[string]any{
				"trade_id": "t1",
				"symbol":   "AAPL",
				"side":     "BUY",
				"quantity": "1",
				"price":    "10",
				"currency": "USD",
			},
		}
		b, _ := json.Marshal(body)
		req1 := httptest.NewRequest(http.MethodPost, "/v1/trades", bytes.NewReader(b))
		req1.Header.Set("Content-Type", "application/json")
		rec1 := httptest.NewRecorder()
		r.ServeHTTP(rec1, req1)
		if rec1.Code != http.StatusCreated {
			t.Fatalf("first status = %d want %d", rec1.Code, http.StatusCreated)
		}
		var firstResp map[string]any
		if err := json.Unmarshal(rec1.Body.Bytes(), &firstResp); err != nil {
			t.Fatal(err)
		}
		firstID, _ := firstResp["event_id"].(string)

		req2 := httptest.NewRequest(http.MethodPost, "/v1/trades", bytes.NewReader(b))
		req2.Header.Set("Content-Type", "application/json")
		rec2 := httptest.NewRecorder()
		r.ServeHTTP(rec2, req2)
		if rec2.Code != http.StatusOK {
			t.Fatalf("second status = %d want %d body=%s", rec2.Code, http.StatusOK, rec2.Body.String())
		}
		var secondResp map[string]any
		if err := json.Unmarshal(rec2.Body.Bytes(), &secondResp); err != nil {
			t.Fatal(err)
		}
		secondID, _ := secondResp["event_id"].(string)
		if secondID != firstID {
			t.Fatalf("second event_id %q != first canonical %q", secondID, firstID)
		}
	})

	t.Run("store_failure_500", func(t *testing.T) {
		t.Parallel()
		broken := &fakeIngestStore{err: errors.New("db down")}
		r := testRouter(logger, ingestion.NewService(broken))
		body := map[string]any{
			"portfolio_id":    uuid.MustParse("550e8400-e29b-41d4-a716-446655440001").String(),
			"idempotency_key": "k3",
			"source":          "trade_api",
			"trade": map[string]any{
				"trade_id": "t1",
				"symbol":   "AAPL",
				"side":     "BUY",
				"quantity": "1",
				"price":    "1",
				"currency": "USD",
			},
		}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/v1/trades", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(RequestIDHeader, "bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d want %d", rec.Code, http.StatusInternalServerError)
		}
		assertAPIErrorEnvelope(t, rec.Body.Bytes(), ErrCodeInternal, "internal error", "bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	})
}

func TestPostPrice_ingest_response(t *testing.T) {
	t.Parallel()
	logger := zap.NewNop()
	store := &fakeIngestStore{}
	svc := ingestion.NewService(store)
	r := testRouter(logger, svc)

	body := map[string]any{
		"idempotency_key": "px1",
		"source":          "price_feed",
		"price": map[string]any{
			"symbol":          "AAPL",
			"price":           "150.25",
			"currency":        "USD",
			"source_sequence": 1,
		},
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/prices", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d want %d body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	eid, _ := resp["event_id"].(string)
	if _, err := uuid.Parse(eid); err != nil {
		t.Fatalf("event_id: %v", err)
	}
	if len(store.appended) != 1 {
		t.Fatalf("appended = %d", len(store.appended))
	}
	if store.appended[0].EventType != domain.EventTypePriceUpdated {
		t.Fatalf("event type = %s", store.appended[0].EventType)
	}
	pid, _ := uuid.Parse(store.appended[0].PortfolioID)
	found := false
	for _, p := range testPricePartitions {
		if p == pid {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("portfolio_id %v not in test price partitions", store.appended[0].PortfolioID)
	}
}

func TestPostPrice_idempotent_200(t *testing.T) {
	t.Parallel()
	logger := zap.NewNop()
	dupStore := &idempotentFakeStore{byKey: make(map[string]uuid.UUID)}
	svc := ingestion.NewService(dupStore)
	r := testRouter(logger, svc)

	body := map[string]any{
		"idempotency_key": "px-dup",
		"source":          "price_feed",
		"price": map[string]any{
			"symbol":          "MSFT",
			"price":           "300",
			"currency":        "USD",
			"source_sequence": 2,
		},
	}
	b, _ := json.Marshal(body)
	req1 := httptest.NewRequest(http.MethodPost, "/v1/prices", bytes.NewReader(b))
	req1.Header.Set("Content-Type", "application/json")
	rec1 := httptest.NewRecorder()
	r.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first = %d", rec1.Code)
	}
	var first map[string]any
	_ = json.Unmarshal(rec1.Body.Bytes(), &first)
	firstID := first["event_id"].(string)

	req2 := httptest.NewRequest(http.MethodPost, "/v1/prices", bytes.NewReader(b))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second = %d body=%s", rec2.Code, rec2.Body.String())
	}
	var second map[string]any
	_ = json.Unmarshal(rec2.Body.Bytes(), &second)
	if second["event_id"].(string) != firstID {
		t.Fatalf("event_id mismatch on duplicate")
	}
}

func TestMapScenarioRunError(t *testing.T) {
	t.Parallel()
	if st, code, msg := MapScenarioRunError(domain.ErrValidation); st != http.StatusBadRequest || code != ErrCodeValidation {
		t.Fatalf("domain via scenario mapper: %d %s %s", st, code, msg)
	}
	if st, code, _ := MapScenarioRunError(scenario.ErrEmptyShocks); st != http.StatusBadRequest || code != ErrCodeValidation {
		t.Fatalf("empty shocks: %d %s", st, code)
	}
	if st, code, _ := MapScenarioRunError(portfolio.ErrAssemblerPortfolioIDRequired); st != http.StatusInternalServerError || code != ErrCodeInternal {
		t.Fatalf("assembler: %d %s", st, code)
	}
}

func TestMapDomainIngestError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		err            error
		wantStatus     int
		wantErrCode    string
		wantMsgSubstr  string // empty = skip
	}{
		{domain.ErrValidation, http.StatusBadRequest, ErrCodeValidation, ""},
		{domain.ErrInvalidPayload, http.StatusBadRequest, ErrCodeValidation, ""},
		{domain.ErrInvalidEvent, http.StatusBadRequest, ErrCodeValidation, ""},
		{domain.ErrUnknownType, http.StatusBadRequest, ErrCodeValidation, ""},
		{domain.ErrPositionUnderflow, http.StatusConflict, ErrCodePositionUnderflow, "trade would exceed position"},
		{domain.ErrDuplicateEvent, http.StatusConflict, ErrCodeIdempotencyConflict, ""},
		{domain.ErrOutOfOrderEvent, http.StatusConflict, ErrCodeLateEventBeyondWatermark, ""},
		{errors.New("other"), http.StatusInternalServerError, ErrCodeInternal, "internal error"},
	}
	for _, tt := range tests {
		status, code, msg := MapDomainIngestError(tt.err)
		if status != tt.wantStatus || code != tt.wantErrCode {
			t.Errorf("MapDomainIngestError(%v) = (%d, %q, %q) want (%d, %q, …)", tt.err, status, code, msg, tt.wantStatus, tt.wantErrCode)
		}
		if tt.wantMsgSubstr != "" && msg != tt.wantMsgSubstr {
			t.Errorf("MapDomainIngestError(%v) message = %q want %q", tt.err, msg, tt.wantMsgSubstr)
		}
	}
}
