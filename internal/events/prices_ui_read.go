package events

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/domain"
)

// ListPriceMarksParams controls server-side listing of prices_projection rows.
type ListPriceMarksParams struct {
	Query  string
	Sort   string
	Order  string
	Limit  int
	Offset int
}

// PriceMarkListRow is one row for price directory UIs.
type PriceMarkListRow struct {
	Symbol    string
	Price     string
	AsOf      time.Time
	UpdatedAt time.Time
	Source    string
	// ChangePct is the latest stored daily_return (decimal string); nil when unknown.
	ChangePct *string
}

type ListPriceMarksResult struct {
	Items []PriceMarkListRow
	Total int64
}

// PriceHistoryPoint is one daily return bucket for mini-history panels.
type PriceHistoryPoint struct {
	ReturnDate    string
	ClosePrice    string
	DailyReturn   *string
	AsOfEventTime time.Time
}

// PriceSymbolDetail is latest mark plus recent return history for a symbol.
type PriceSymbolDetail struct {
	Symbol    string
	Price     string
	AsOf      time.Time
	UpdatedAt time.Time
	Source    string
	History   []PriceHistoryPoint
}

func normalizeListSort(sort, order string) (orderExpr string) {
	s := strings.ToLower(strings.TrimSpace(sort))
	o := strings.ToLower(strings.TrimSpace(order))
	if o != "desc" {
		o = "asc"
	}
	col := "pr.symbol"
	switch s {
	case "price":
		col = "pr.price"
	case "as_of":
		col = "pr.as_of"
	case "updated_at":
		col = "pr.updated_at"
	case "change_pct":
		col = "sr.dr"
	}
	nulls := "NULLS LAST"
	if s == "symbol" && o == "desc" {
		nulls = "NULLS LAST"
	}
	return fmt.Sprintf("%s %s %s", col, strings.ToUpper(o), nulls)
}

// ListPriceMarks returns paginated price marks with optional symbol filter (substring, case-insensitive).
func (s *PostgresStore) ListPriceMarks(ctx context.Context, p ListPriceMarksParams) (ListPriceMarksResult, error) {
	if p.Limit <= 0 {
		p.Limit = 50
	}
	if p.Limit > 500 {
		p.Limit = 500
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
	q := strings.TrimSpace(p.Query)
	if q != "" {
		if err := domain.ValidateSymbol(strings.ToUpper(q)); err != nil {
			// Allow partial: only safe characters.
			for _, ch := range q {
				if (ch < 'A' || ch > 'Z') && (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '.' && ch != '_' && ch != '-' {
					return ListPriceMarksResult{}, fmt.Errorf("invalid query characters: %w", domain.ErrValidation)
				}
			}
		}
	}

	orderSQL := normalizeListSort(p.Sort, p.Order)

	countSQL := `SELECT COUNT(*)::bigint FROM prices_projection pr WHERE ($1::text = '' OR pr.symbol ILIKE '%' || $1 || '%')`
	var total int64
	if err := s.pool.QueryRow(ctx, countSQL, q).Scan(&total); err != nil {
		return ListPriceMarksResult{}, fmt.Errorf("count prices_projection: %w", err)
	}

	listSQL := `
SELECT pr.symbol, pr.price::text, pr.as_of, pr.updated_at, COALESCE(e.source, ''),
       sr.dr
FROM prices_projection pr
LEFT JOIN events e ON e.event_id = pr.as_of_event_id
LEFT JOIN LATERAL (
	SELECT x.daily_return::text AS dr
	FROM symbol_returns x
	WHERE x.symbol = pr.symbol
	ORDER BY x.return_date DESC
	LIMIT 1
) sr ON true
WHERE ($1::text = '' OR pr.symbol ILIKE '%' || $1 || '%')
ORDER BY ` + orderSQL + `
LIMIT $2 OFFSET $3`

	rows, err := s.pool.Query(ctx, listSQL, q, p.Limit, p.Offset)
	if err != nil {
		return ListPriceMarksResult{}, fmt.Errorf("list prices_projection: %w", err)
	}
	defer rows.Close()

	out := make([]PriceMarkListRow, 0, p.Limit)
	for rows.Next() {
		var row PriceMarkListRow
		var change *string
		if err := rows.Scan(&row.Symbol, &row.Price, &row.AsOf, &row.UpdatedAt, &row.Source, &change); err != nil {
			return ListPriceMarksResult{}, fmt.Errorf("scan price list row: %w", err)
		}
		row.AsOf = row.AsOf.UTC()
		row.UpdatedAt = row.UpdatedAt.UTC()
		if change != nil && *change != "" {
			v := *change
			row.ChangePct = &v
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return ListPriceMarksResult{}, fmt.Errorf("iterate price list: %w", err)
	}
	return ListPriceMarksResult{Items: out, Total: total}, nil
}

// GetPriceSymbolDetail loads the latest mark plus up to historyLimit return rows (most recent first).
func (s *PostgresStore) GetPriceSymbolDetail(ctx context.Context, symbol string, historyLimit int) (*PriceSymbolDetail, bool, error) {
	symbol = strings.TrimSpace(symbol)
	if err := domain.ValidateSymbol(symbol); err != nil {
		return nil, false, err
	}
	if historyLimit <= 0 {
		historyLimit = 10
	}
	if historyLimit > 60 {
		historyLimit = 60
	}

	const head = `
SELECT pr.symbol, pr.price::text, pr.as_of, pr.updated_at, COALESCE(e.source, '')
FROM prices_projection pr
LEFT JOIN events e ON e.event_id = pr.as_of_event_id
WHERE pr.symbol = $1`

	var d PriceSymbolDetail
	err := s.pool.QueryRow(ctx, head, symbol).Scan(&d.Symbol, &d.Price, &d.AsOf, &d.UpdatedAt, &d.Source)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("load price head: %w", err)
	}
	d.AsOf = d.AsOf.UTC()
	d.UpdatedAt = d.UpdatedAt.UTC()

	histSQL := `
SELECT return_date::text, close_price::text, daily_return::text, as_of_event_time
FROM symbol_returns
WHERE symbol = $1
ORDER BY return_date DESC
LIMIT $2`
	rows, err := s.pool.Query(ctx, histSQL, symbol, historyLimit)
	if err != nil {
		return nil, false, fmt.Errorf("load symbol_returns: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var p PriceHistoryPoint
		var dr *string
		if err := rows.Scan(&p.ReturnDate, &p.ClosePrice, &dr, &p.AsOfEventTime); err != nil {
			return nil, false, fmt.Errorf("scan history row: %w", err)
		}
		p.AsOfEventTime = p.AsOfEventTime.UTC()
		if dr != nil && *dr != "" {
			v := *dr
			p.DailyReturn = &v
		}
		d.History = append(d.History, p)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate history: %w", err)
	}
	return &d, true, nil
}
