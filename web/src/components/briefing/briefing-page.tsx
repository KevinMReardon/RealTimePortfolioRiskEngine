"use client";

import { useEffect, useMemo, useState, type ReactNode } from "react";
import { useRouter, useSearchParams } from "next/navigation";

import { errorHttpStatus } from "@/lib/api/errors";
import type { BriefingOutput } from "@/lib/api/types";
import {
  useBriefingsListQuery,
  useCreateBriefingMutation,
  useLatestBriefingQuery,
} from "@/hooks/use-briefing";
import { usePortfoliosQuery } from "@/hooks/use-portfolio-api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { ErrorAlert } from "@/components/feedback/query-states";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";

const IN_FLIGHT = new Set(["queued", "running"]);
const SETTINGS_STORAGE_KEY = "briefing-ui-settings-v1";

type BriefingSettingsState = {
  riskBudgetPct: string;
  maxSingleNameWeightPct: string;
  timeHorizon: string;
  stopStyle: string;
  targetStyle: string;
  allowNewSymbols: boolean;
  notes: string;
  maxTokens: string;
  temperature: string;
};

const DEFAULT_SETTINGS: BriefingSettingsState = {
  riskBudgetPct: "",
  maxSingleNameWeightPct: "",
  timeHorizon: "",
  stopStyle: "",
  targetStyle: "",
  allowNewSymbols: false,
  notes: "",
  maxTokens: "",
  temperature: "",
};

export function BriefingPage() {
  const router = useRouter();
  const params = useSearchParams();
  const portfoliosQ = usePortfoliosQuery();
  const portfolios = portfoliosQ.data ?? [];
  const selectedPortfolio = params.get("portfolio") ?? portfolios[0]?.portfolio_id ?? null;

  const latestQ = useLatestBriefingQuery(selectedPortfolio);
  const listQ = useBriefingsListQuery(selectedPortfolio, 10, 0);
  const createM = useCreateBriefingMutation(selectedPortfolio ?? "");
  const [settings, setSettings] = useState<BriefingSettingsState>(DEFAULT_SETTINGS);

  useEffect(() => {
    try {
      const raw = window.localStorage.getItem(SETTINGS_STORAGE_KEY);
      if (!raw) return;
      const parsed = JSON.parse(raw) as Partial<BriefingSettingsState>;
      setSettings((prev) => ({ ...prev, ...parsed }));
    } catch {
      // Ignore malformed local storage payloads.
    }
  }, []);

  useEffect(() => {
    window.localStorage.setItem(SETTINGS_STORAGE_KEY, JSON.stringify(settings));
  }, [settings]);

  const latestStatus = latestQ.data?.Status?.toLowerCase() ?? "";
  const isRunning = IN_FLIGHT.has(latestStatus);

  const latestOutput = useMemo(
    () => readOutputFromSession(latestQ.data?.ResponseValidated),
    [latestQ.data?.ResponseValidated],
  );

  const state = resolvePageState({
    hasPortfolio: Boolean(selectedPortfolio),
    portfoliosLoading: portfoliosQ.isLoading,
    latestLoading: latestQ.isLoading,
    latestError: latestQ.error,
    latestStatus: latestQ.data?.Status,
    hasOutput: Boolean(latestOutput),
  });

  return (
    <div className="mx-auto max-w-6xl space-y-6 animate-fade-in">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
        <div className="space-y-1">
          <h1 className="text-2xl font-semibold tracking-tight">Briefing</h1>
          <p className="text-sm text-muted-foreground">
            Generate an on-demand AI market briefing for your portfolio and review ranked trade ideas.
          </p>
        </div>
        <div className="flex flex-col gap-2 sm:items-end">
          {portfolios.length > 1 ? (
            <Select
              value={selectedPortfolio ?? undefined}
              onValueChange={(v) => {
                const url = new URL(window.location.href);
                url.searchParams.set("portfolio", v);
                router.push(`${url.pathname}?${url.searchParams.toString()}`);
              }}
              disabled={portfoliosQ.isLoading}
            >
              <SelectTrigger className="w-[min(92vw,380px)]">
                <SelectValue placeholder="Select portfolio…" />
              </SelectTrigger>
              <SelectContent>
                {portfolios.map((p) => (
                  <SelectItem key={p.portfolio_id} value={p.portfolio_id}>
                    <span className="font-medium">{p.name}</span>
                    <span className="ml-2 font-mono text-xs text-muted-foreground">
                      {p.portfolio_id.slice(0, 8)}…
                    </span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          ) : portfolios.length === 1 ? (
            <div className="w-[min(92vw,380px)] rounded-md border bg-muted/30 px-3 py-2 text-left text-sm">
              <div className="font-medium">{portfolios[0].name}</div>
              <div className="break-all font-mono text-[11px] text-muted-foreground">
                {portfolios[0].portfolio_id}
              </div>
            </div>
          ) : null}
          <Button
            onClick={async () => {
              if (!selectedPortfolio) return;
              await createM.mutateAsync(buildCreateBriefingOptions(settings));
              await latestQ.refetch();
            }}
            disabled={!selectedPortfolio || createM.isPending || isRunning}
          >
            {createM.isPending
              ? "Starting briefing…"
              : isRunning
                ? "Briefing in progress…"
                : "Run on-demand briefing"}
          </Button>
        </div>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Briefing settings</CardTitle>
          <CardDescription>
            Optional risk and execution preferences passed to the briefing request.
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-3 md:grid-cols-2">
          <Field label="Risk budget per idea (%)">
            <Input
              placeholder="e.g. 0.5"
              value={settings.riskBudgetPct}
              onChange={(e) => setSettings((s) => ({ ...s, riskBudgetPct: e.target.value }))}
            />
          </Field>
          <Field label="Max single-name weight (%)">
            <Input
              placeholder="e.g. 40"
              value={settings.maxSingleNameWeightPct}
              onChange={(e) =>
                setSettings((s) => ({ ...s, maxSingleNameWeightPct: e.target.value }))
              }
            />
          </Field>
          <Field label="Time horizon">
            <Input
              placeholder="e.g. swing / position / long-term"
              value={settings.timeHorizon}
              onChange={(e) => setSettings((s) => ({ ...s, timeHorizon: e.target.value }))}
            />
          </Field>
          <Field label="Stop style">
            <Input
              placeholder="e.g. hard price / thesis / trailing"
              value={settings.stopStyle}
              onChange={(e) => setSettings((s) => ({ ...s, stopStyle: e.target.value }))}
            />
          </Field>
          <Field label="Target style">
            <Input
              placeholder="e.g. % move / R-multiple / weight target"
              value={settings.targetStyle}
              onChange={(e) => setSettings((s) => ({ ...s, targetStyle: e.target.value }))}
            />
          </Field>
          <Field label="Allow new symbols">
            <label className="inline-flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={settings.allowNewSymbols}
                onChange={(e) =>
                  setSettings((s) => ({ ...s, allowNewSymbols: e.target.checked }))
                }
              />
              Allow diversification ideas outside current holdings
            </label>
          </Field>
          <Field label="Max tokens (optional)">
            <Input
              placeholder="e.g. 6144"
              value={settings.maxTokens}
              onChange={(e) => setSettings((s) => ({ ...s, maxTokens: e.target.value }))}
            />
          </Field>
          <Field label="Temperature (optional)">
            <Input
              placeholder="0.0 - 1.0"
              value={settings.temperature}
              onChange={(e) => setSettings((s) => ({ ...s, temperature: e.target.value }))}
            />
          </Field>
          <div className="md:col-span-2">
            <Field label="Additional notes for the model">
              <Textarea
                className="min-h-[88px]"
                placeholder="Any constraints or preferences to include..."
                value={settings.notes}
                onChange={(e) => setSettings((s) => ({ ...s, notes: e.target.value }))}
              />
            </Field>
          </div>
        </CardContent>
      </Card>

      {createM.error ? <ErrorAlert error={createM.error} title="Failed to start briefing" /> : null}
      {state === "error" && latestQ.error ? <ErrorAlert error={latestQ.error} /> : null}

      {state === "idle" ? (
        <Card>
          <CardHeader>
            <CardTitle>No portfolio selected</CardTitle>
            <CardDescription>
              Pick or create a portfolio before requesting a briefing.
            </CardDescription>
          </CardHeader>
        </Card>
      ) : null}

      {state === "loading" ? (
        <Card>
          <CardHeader>
            <CardTitle>Loading briefing</CardTitle>
            <CardDescription>Fetching the latest run and recent history.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            <Skeleton className="h-20 w-full" />
            <Skeleton className="h-24 w-full" />
            <Skeleton className="h-24 w-full" />
          </CardContent>
        </Card>
      ) : null}

      {state === "empty" ? (
        <Card>
          <CardHeader>
            <CardTitle>No briefings yet</CardTitle>
            <CardDescription>
              Trigger your first run to generate a market summary and trade ideas.
            </CardDescription>
          </CardHeader>
        </Card>
      ) : null}

      {state === "success" ? (
        <>
          <Card>
            <CardHeader className="flex flex-row items-start justify-between gap-4 space-y-0">
              <div className="space-y-1">
                <CardTitle>Latest briefing</CardTitle>
                <CardDescription>
                  Status, timing, and model details for the most recent briefing run.
                </CardDescription>
              </div>
              <Badge variant={isRunning ? "outline" : "default"}>
                {latestQ.data?.Status ?? "unknown"}
              </Badge>
            </CardHeader>
            <CardContent className="grid gap-3 text-sm md:grid-cols-2">
              <Meta label="Session" value={latestQ.data?.SessionID ?? "—"} mono />
              <Meta label="Trigger" value={latestQ.data?.TriggerSource ?? "—"} />
              <Meta label="Run date" value={formatMaybeDate(latestQ.data?.RunDate)} />
              <Meta label="Completed" value={formatMaybeDate(latestQ.data?.CompletedAt)} />
            </CardContent>
          </Card>

          {latestOutput ? (
            <Card>
              <CardHeader>
                <CardTitle>Trade ideas</CardTitle>
                <CardDescription>
                  Ranked by confidence with size, stop, target, and rationale.
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-3">
                {rankIdeas(latestOutput).length ? (
                  rankIdeas(latestOutput).map((idea, idx) => (
                    <div key={`${idea.symbol ?? "idea"}-${idx}`} className="rounded-md border p-3">
                      <div className="flex flex-wrap items-center justify-between gap-2">
                        <div className="font-semibold">
                          #{idx + 1} {idea.symbol?.trim() || "Idea"}
                        </div>
                        <Badge variant="outline">
                          {Math.round((idea.confidence ?? 0) * 100)}% confidence
                        </Badge>
                      </div>
                      <div className="mt-2 grid gap-2 text-sm md:grid-cols-3">
                        <Meta label="Size" value={idea.size || "—"} />
                        <Meta label="Stop" value={idea.stop || "—"} />
                        <Meta label="Target" value={idea.target || "—"} />
                      </div>
                      <p className="mt-2 text-sm text-muted-foreground">
                        {idea.rationale || "No rationale provided."}
                      </p>
                    </div>
                  ))
                ) : (
                  <div className="text-sm text-muted-foreground">
                    No trade ideas in the latest briefing output.
                  </div>
                )}
              </CardContent>
            </Card>
          ) : (
            <Card>
              <CardHeader>
                <CardTitle>Latest output unavailable</CardTitle>
                <CardDescription>
                  The latest session has not produced a validated output yet.
                </CardDescription>
              </CardHeader>
            </Card>
          )}

          <Card>
            <CardHeader>
              <CardTitle>Recent briefing runs</CardTitle>
              <CardDescription>Most recent sessions for this portfolio.</CardDescription>
            </CardHeader>
            <CardContent className="space-y-2">
              {listQ.error ? (
                <ErrorAlert error={listQ.error} title="Could not load briefing history" />
              ) : (
                (listQ.data?.items ?? []).map((row) => (
                  <div
                    key={row.SessionID}
                    className="flex flex-wrap items-center justify-between gap-2 rounded-md border p-3 text-sm"
                  >
                    <div className="space-y-1">
                      <div className="font-mono text-xs">{row.SessionID}</div>
                      <div className="text-muted-foreground">
                        {formatMaybeDate(row.CreatedAt)} • {row.TriggerSource}
                      </div>
                    </div>
                    <Badge variant="outline">{row.Status}</Badge>
                  </div>
                ))
              )}
              {!listQ.isLoading && (listQ.data?.items?.length ?? 0) === 0 ? (
                <div className="text-sm text-muted-foreground">No history yet.</div>
              ) : null}
            </CardContent>
          </Card>
        </>
      ) : null}
    </div>
  );
}

function Meta({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="space-y-1">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className={mono ? "font-mono text-xs break-all" : ""}>{value}</div>
    </div>
  );
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="space-y-1">
      <div className="text-xs text-muted-foreground">{label}</div>
      {children}
    </div>
  );
}

function formatMaybeDate(raw: string | undefined | null): string {
  if (!raw) return "—";
  const t = Date.parse(raw);
  if (Number.isNaN(t)) return raw;
  return new Date(t).toLocaleString();
}

function readOutputFromSession(raw: unknown): BriefingOutput | null {
  if (!raw || typeof raw !== "object") return null;
  const candidate = raw as Record<string, unknown>;
  if (
    typeof candidate.market_summary === "string" &&
    typeof candidate.portfolio_context === "string" &&
    Array.isArray(candidate.trade_ideas)
  ) {
    return candidate as unknown as BriefingOutput;
  }
  return null;
}

function rankIdeas(output: BriefingOutput) {
  return [...(output.trade_ideas ?? [])].sort(
    (a, b) => (b.confidence ?? 0) - (a.confidence ?? 0),
  );
}

function buildCreateBriefingOptions(settings: BriefingSettingsState) {
  const userInput: Record<string, unknown> = {};
  const riskBudgetPct = parseOptionalFloat(settings.riskBudgetPct);
  const maxSingleNameWeightPct = parseOptionalFloat(settings.maxSingleNameWeightPct);
  const maxTokens = parseOptionalInt(settings.maxTokens);
  const temperature = parseOptionalFloat(settings.temperature);

  if (riskBudgetPct !== undefined) userInput.risk_budget_per_trade_pct = riskBudgetPct;
  if (maxSingleNameWeightPct !== undefined) {
    userInput.max_single_name_weight_pct = maxSingleNameWeightPct;
  }
  if (settings.timeHorizon.trim()) userInput.time_horizon = settings.timeHorizon.trim();
  if (settings.stopStyle.trim()) userInput.stop_style = settings.stopStyle.trim();
  if (settings.targetStyle.trim()) userInput.target_style = settings.targetStyle.trim();
  if (settings.notes.trim()) userInput.notes = settings.notes.trim();
  userInput.allow_new_symbols = settings.allowNewSymbols;

  return {
    user_input: userInput,
    ...(maxTokens !== undefined ? { max_tokens: maxTokens } : {}),
    ...(temperature !== undefined ? { temperature } : {}),
  };
}

function parseOptionalFloat(raw: string): number | undefined {
  const value = raw.trim();
  if (!value) return undefined;
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : undefined;
}

function parseOptionalInt(raw: string): number | undefined {
  const value = raw.trim();
  if (!value) return undefined;
  const parsed = Number.parseInt(value, 10);
  return Number.isFinite(parsed) ? parsed : undefined;
}

function resolvePageState(input: {
  hasPortfolio: boolean;
  portfoliosLoading: boolean;
  latestLoading: boolean;
  latestError: unknown;
  latestStatus?: string;
  hasOutput: boolean;
}): "idle" | "loading" | "error" | "empty" | "success" {
  if (!input.hasPortfolio) return input.portfoliosLoading ? "loading" : "idle";
  if (input.latestLoading) return "loading";
  if (input.latestError) {
    return errorHttpStatus(input.latestError) === 404 ? "empty" : "error";
  }
  if (!input.latestStatus) return "empty";
  if (input.latestStatus.toLowerCase() === "succeeded" && input.hasOutput) return "success";
  return "success";
}
