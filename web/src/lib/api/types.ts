export type UUID = string;

export type PortfolioPosition = {
  symbol: string;
  quantity: string;
  average_cost: string;
  realized_pnl: string;
  last_price?: string;
  market_value?: string;
  unrealized_pnl?: string;
};

export type PortfolioTotalsView = {
  market_value: string;
  realized_pnl: string;
  unrealized_pnl: string;
};

export type ReadAsOfRef = {
  event_id: UUID;
  event_time: string;
  processing_time?: string;
};

export type PriceMarkAsOf = {
  symbol: string;
  event_id: UUID;
  event_time: string;
  processing_time?: string;
};

export type PortfolioView = {
  portfolio_id: string;
  positions: PortfolioPosition[];
  unpriced_symbols: string[];
  totals: PortfolioTotalsView;
  driving_event_ids?: UUID[];
  as_of_positions?: ReadAsOfRef;
  as_of_prices?: PriceMarkAsOf[];
  as_of_event_id?: UUID;
  as_of_event_time?: string;
  as_of_processing_time?: string;
};

export type PortfolioCatalogEntry = {
  portfolio_id: string;
  name: string;
  base_currency: string;
  created_at: string;
  updated_at: string;
};

export type ListPortfoliosResponse = {
  portfolios: PortfolioCatalogEntry[];
};

export type CreatePortfolioRequest = {
  name: string;
  base_currency?: string;
};

export type RiskAssumptions = Record<string, unknown>;

export type RiskExposure = {
  symbol: string;
  exposure: string;
  weight: string;
};

export type RiskConcentration = {
  top_n?: RiskExposure[];
  hhi: string;
};

export type RiskVolSym = {
  symbol: string;
  sigma_1d: string;
};

export type RiskVolatility = {
  sigma_1d_portfolio: string;
  by_symbol?: RiskVolSym[];
};

export type RiskMetadata = {
  sigma_window_n: number;
  min_daily_returns_required: number;
};

export type RiskHTTPResponse = {
  portfolio_id: string;
  exposure: RiskExposure[];
  concentration: RiskConcentration;
  volatility: RiskVolatility;
  var_95_1d: string;
  assumptions: RiskAssumptions;
  metadata: RiskMetadata;
  driving_event_ids?: UUID[];
  as_of_positions?: ReadAsOfRef;
  as_of_prices?: PriceMarkAsOf[];
  as_of_event_id?: UUID;
  as_of_event_time?: string;
  as_of_processing_time?: string;
};

export type ScenarioShockRequest = {
  symbol: string;
  type?: string;
  kind?: string;
  value: string;
};

export type ScenarioRunRequest = {
  shocks: ScenarioShockRequest[];
};

export type ScenarioDelta = {
  market_value: string;
  unrealized_pnl: string;
  realized_pnl: string;
};

export type ScenarioShockEcho = {
  symbol: string;
  type: string;
  value: string;
};

export type ScenarioBaseMetadata = {
  driving_event_ids?: UUID[];
  as_of_positions?: ReadAsOfRef;
  as_of_prices?: PriceMarkAsOf[];
  as_of_event_id?: UUID;
  as_of_event_time?: string;
  as_of_processing_time?: string;
};

export type ScenarioHTTPResponse = {
  portfolio_id: string;
  base_metadata: ScenarioBaseMetadata;
  base: PortfolioView;
  shocked: PortfolioView;
  delta: ScenarioDelta;
  shocks: ScenarioShockEcho[];
  base_as_of_event_id?: UUID;
  base_as_of_event_time?: string;
  base_as_of_processing_time?: string;
};

export type TradePayload = {
  trade_id: string;
  symbol: string;
  side: "BUY" | "SELL" | string;
  quantity: string;
  price: string;
  currency: string;
};

export type PostTradeRequest = {
  portfolio_id: string;
  idempotency_key: string;
  source: string;
  event_time?: string;
  trade: TradePayload;
};

export type PostPriceRequest = {
  idempotency_key: string;
  source: string;
  event_time?: string;
  price: {
    symbol: string;
    price: string;
    currency: string;
    source_sequence: number;
  };
};

export type IngestResponse = {
  event_id: string;
  status: "created" | "duplicate" | string;
};

export type APIErrorBody = {
  error_code: string;
  message: string;
  details: Record<string, unknown>;
  request_id: string;
};

export type InsightsExplainResponse = {
  context?: unknown;
  explanation: string;
  used_metrics: string[];
  model: string;
};

export type PriceListItem = {
  symbol: string;
  price: string;
  change_pct?: string;
  as_of: string;
  updated_at: string;
  source: string;
  staleness_seconds: number;
  provider_data_status: string;
};

export type ListPricesResponse = {
  items: PriceListItem[];
  total: number;
  limit: number;
  offset: number;
};

export type PriceHistoryPoint = {
  return_date: string;
  close_price: string;
  daily_return?: string;
  as_of_event_time: string;
};

export type PriceDetailResponse = {
  symbol: string;
  price: string;
  as_of: string;
  updated_at: string;
  source: string;
  history: PriceHistoryPoint[];
  history_summary: string;
  staleness_seconds: number;
  provider_data_status: string;
};

export type PriceFeedStatusResponse = {
  feed_enabled: boolean;
  configured_provider: string;
  poll_interval_ms: number;
  watchlist_count: number;
  watchlist_preview?: string[];
  staleness_threshold_seconds: number;
  last_tick_started_at?: string;
  last_tick_finished_at?: string;
  last_successful_fetch_at?: string;
  active_provider?: string;
  last_tick_used_failover: boolean;
  last_tick_ingested_count?: number;
  last_error?: string;
};

export type PriceFeedWatchlistResponse = {
  watchlist: string[];
};

export type UpdatePriceFeedWatchlistRequest = {
  watchlist: string[];
};
