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
import {
  useApproveProposalMutation,
  useDenyProposalMutation,
  useProposalsQuery,
  useSubmitProposalMutation,
} from "@/hooks/use-proposals";
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
  const proposalsQ = useProposalsQuery(selectedPortfolio);
  const approveProposalM = useApproveProposalMutation(selectedPortfolio ?? "");
  const denyProposalM = useDenyProposalMutation(selectedPortfolio ?? "");
  const submitProposalM = useSubmitProposalMutation(selectedPortfolio ?? "");
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
  const proposals = useMemo(() => proposalsQ.data?.proposals ?? [], [proposalsQ.data?.proposals]);
  const proposalByTradeIdeaIndex = useMemo(() => {
    const sessionId = latestQ.data?.SessionID ?? "";
    const out = new Map<number, (typeof proposals)[number]>();
    for (const p of proposals) {
      if (p.agent_session_id && p.agent_session_id !== sessionId) continue;
      if (typeof p.trade_idea_index === "number" && !out.has(p.trade_idea_index)) {
        out.set(p.trade_idea_index, p);
      }
    }
    return out;
  }, [latestQ.data?.SessionID, proposals]);

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
      {proposalsQ.error ? <ErrorAlert error={proposalsQ.error} title="Failed to load proposals" /> : null}
      {approveProposalM.error ? (
        <ErrorAlert error={approveProposalM.error} title="Failed to approve proposal" />
      ) : null}
      {denyProposalM.error ? (
        <ErrorAlert error={denyProposalM.error} title="Failed to reject proposal" />
      ) : null}
      {submitProposalM.error ? (
        <ErrorAlert error={submitProposalM.error} title="Failed to execute proposal" />
      ) : null}
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
              <Meta label="Run date" value={formatRunDate(latestQ.data?.RunDate)} />
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
                  rankIdeas(latestOutput).map(({ idea, originalIndex }, rankIdx) => (
                    <div key={`${idea.symbol ?? "idea"}-${originalIndex}`} className="rounded-md border p-3">
                      {(() => {
                        const proposal = proposalByTradeIdeaIndex.get(originalIndex);
                        const status = proposal ? proposalStatusLabel(proposal.status) : null;
                        const policyReason = proposal ? extractPolicyDenyReason(proposal.policy_result) : null;
                        const criticReason = proposal ? extractCriticBlockReason(proposal.critic_verdict) : null;
                        const proposalBusy =
                          (approveProposalM.isPending &&
                            approveProposalM.variables?.proposalId === proposal?.proposal_id) ||
                          (denyProposalM.isPending &&
                            denyProposalM.variables?.proposalId === proposal?.proposal_id) ||
                          (submitProposalM.isPending &&
                            submitProposalM.variables?.proposalId === proposal?.proposal_id);
                        return (
                          <>
                      <div className="flex flex-wrap items-center justify-between gap-2">
                        <div className="font-semibold">
                          #{rankIdx + 1} {idea.symbol?.trim() || "Idea"}
                        </div>
                        <div className="flex items-center gap-2">
                          <Badge variant="outline">
                            {Math.round((idea.confidence ?? 0) * 100)}% confidence
                          </Badge>
                          {status ? <Badge variant="outline">{status}</Badge> : null}
                        </div>
                      </div>
                      <div className="mt-2 grid gap-2 text-sm md:grid-cols-3">
                        <Meta label="Size" value={idea.size || "—"} />
                        <Meta label="Stop" value={idea.stop || "—"} />
                        <Meta label="Target" value={idea.target || "—"} />
                      </div>
                      <p className="mt-2 text-sm text-muted-foreground">
                        {idea.rationale || "No rationale provided."}
                      </p>
                      <div className="mt-3 flex flex-wrap gap-2">
                        {proposal?.status === "proposed" ? (
                          <>
                            <Button
                              size="sm"
                              disabled={proposalBusy}
                              onClick={() =>
                                approveProposalM.mutate({
                                  proposalId: proposal.proposal_id,
                                  payloadHash: proposal.payload_hash,
                                  rowVersion: proposal.row_version,
                                })
                              }
                            >
                              {proposalBusy ? "Working..." : "Approve"}
                            </Button>
                            <Button
                              size="sm"
                              variant="outline"
                              disabled={proposalBusy}
                              onClick={() =>
                                denyProposalM.mutate({
                                  proposalId: proposal.proposal_id,
                                  payloadHash: proposal.payload_hash,
                                  rowVersion: proposal.row_version,
                                  denyReason: "Rejected from briefing UI",
                                })
                              }
                            >
                              {proposalBusy ? "Working..." : "Reject"}
                            </Button>
                          </>
                        ) : null}
                        {proposal?.status === "approved" ? (
                          <Button
                            size="sm"
                            disabled={proposalBusy}
                            onClick={() =>
                              submitProposalM.mutate({
                                proposalId: proposal.proposal_id,
                                payloadHash: proposal.payload_hash,
                                rowVersion: proposal.row_version,
                              })
                            }
                          >
                            {proposalBusy ? "Executing..." : "Execute trade"}
                          </Button>
                        ) : null}
                        {!proposal ? (
                          <span className="text-xs text-muted-foreground">
                            Awaiting materialized proposal for this idea.
                          </span>
                        ) : null}
                        {proposal?.status === "proposed" && policyReason ? (
                          <span className="text-xs text-amber-600">
                            Auto-submit blocked by policy: {policyReason}
                          </span>
                        ) : null}
                        {proposal?.status === "proposed" && !policyReason && criticReason ? (
                          <span className="text-xs text-amber-600">
                            Auto-submit blocked by critic: {criticReason}
                          </span>
                        ) : null}
                      </div>
                          </>
                        );
                      })()}
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

function formatRunDate(raw: string | undefined | null): string {
  if (!raw) return "—";
  // run_date is a logical trading date; keep it date-only to avoid timezone day-shift confusion.
  const m = raw.match(/^(\d{4})-(\d{2})-(\d{2})/);
  if (m) {
    return `${m[1]}-${m[2]}-${m[3]}`;
  }
  return formatMaybeDate(raw);
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
  return [...(output.trade_ideas ?? [])]
    .map((idea, originalIndex) => ({ idea, originalIndex }))
    .sort((a, b) => (b.idea.confidence ?? 0) - (a.idea.confidence ?? 0));
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

function proposalStatusLabel(status: string): string {
  const s = status.trim().toLowerCase();
  if (s === "proposed") return "Pending approval";
  if (s === "approved") return "Approved";
  if (s === "rejected") return "Rejected";
  if (s === "submitted") return "Submitted";
  return status;
}

function extractPolicyDenyReason(policyResult: unknown): string | null {
  if (!policyResult || typeof policyResult !== "object") return null;
  const rec = policyResult as {
    effective_outcome?: unknown;
    EffectiveOutcome?: unknown;
    violations?: unknown;
    Violations?: unknown;
  };
  const effectiveOutcome = String(rec.effective_outcome ?? rec.EffectiveOutcome ?? "").toUpperCase();
  if (effectiveOutcome !== "DENY") return null;
  const violations = Array.isArray(rec.violations)
    ? rec.violations
    : Array.isArray(rec.Violations)
      ? rec.Violations
      : [];
  if (violations.length === 0) return "denied";
  const first = violations[0] as {
    code?: unknown;
    Code?: unknown;
    message?: unknown;
    detail?: unknown;
    Detail?: unknown;
  };
  const code = String(first.code ?? first.Code ?? "").trim();
  const msg = String(first.message ?? first.detail ?? first.Detail ?? "").trim();
  if (code && msg) return `${code} (${msg})`;
  if (code) return code;
  if (msg) return msg;
  return "denied";
}

function extractCriticBlockReason(criticVerdict: unknown): string | null {
  if (!criticVerdict || typeof criticVerdict !== "object") return null;
  const rec = criticVerdict as { allow?: unknown; reason_code?: unknown; notes?: unknown };
  const allow = Boolean(rec.allow);
  if (allow) return null;
  const reasonCode = String(rec.reason_code ?? "").trim();
  const notes = String(rec.notes ?? "").trim();
  if (reasonCode && notes) return `${reasonCode} (${notes})`;
  if (reasonCode) return reasonCode;
  if (notes) return notes;
  return "veto";
}
