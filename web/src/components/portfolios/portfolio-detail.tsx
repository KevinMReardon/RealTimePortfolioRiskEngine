"use client";

import Link from "next/link";
import * as React from "react";
import { useFieldArray, useForm } from "react-hook-form";
import { z } from "zod";
import { zodResolver } from "@hookform/resolvers/zod";

import {
  useExplainInsightsMutation,
  usePortfolioQuery,
  useRiskQuery,
  useRunScenarioMutation,
} from "@/hooks/use-portfolio-api";
import { useSavedPortfolios } from "@/hooks/use-saved-portfolios";
import { compactNumber, compactUsdLike } from "@/lib/format";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Separator } from "@/components/ui/separator";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { ErrorAlert, LoadingCardGrid } from "@/components/feedback/query-states";
import { ExposureBars } from "@/components/charts/exposure-bars";
import { PositionsTable } from "@/components/tables/positions-table";

const scenarioSchema = z.object({
  shocks: z
    .array(
      z.object({
        symbol: z.string().min(1),
        value: z.string().min(1),
      }),
    )
    .min(1),
});

type ScenarioForm = z.infer<typeof scenarioSchema>;

const insightsSchema = z.object({
  prompt: z.string().optional(),
  audience: z.enum(["risk", "pm", "exec"]).default("risk"),
});

type InsightsInput = z.input<typeof insightsSchema>;
type InsightsValues = z.output<typeof insightsSchema>;

export function PortfolioDetail({ portfolioId }: { portfolioId: string }) {
  const portfolioQ = usePortfolioQuery(portfolioId);
  const riskQ = useRiskQuery(portfolioId);
  const scenarioM = useRunScenarioMutation(portfolioId);
  const insightsM = useExplainInsightsMutation(portfolioId);
  const { upsert } = useSavedPortfolios();

  React.useEffect(() => {
    if (portfolioQ.data?.portfolio_id) {
      upsert({ id: portfolioQ.data.portfolio_id, label: "Portfolio" });
    }
  }, [portfolioQ.data?.portfolio_id, upsert]);

  const scenarioForm = useForm<ScenarioForm>({
    resolver: zodResolver(scenarioSchema),
    defaultValues: { shocks: [{ symbol: "AAPL", value: "0.05" }] },
  });
  const shocksFieldArray = useFieldArray({
    control: scenarioForm.control,
    name: "shocks",
  });

  const insightsForm = useForm<InsightsInput, unknown, InsightsValues>({
    resolver: zodResolver(insightsSchema),
    defaultValues: { prompt: "", audience: "risk" },
  });

  return (
    <div className="mx-auto max-w-6xl space-y-6 animate-fade-in">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="space-y-2">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="text-2xl font-semibold tracking-tight">Portfolio</h1>
            <Badge variant="outline" className="font-mono text-xs">
              {portfolioId}
            </Badge>
          </div>
          <p className="text-sm text-muted-foreground">
            Progressive disclosure: start with positions, then risk, then shocks and narrative insights.
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button asChild variant="outline" size="sm">
            <Link href="/dashboard">Back to overview</Link>
          </Button>
          <Button asChild variant="secondary" size="sm">
            <Link href={`/ingest/trade?portfolio=${encodeURIComponent(portfolioId)}`}>
              Record trade
            </Link>
          </Button>
        </div>
      </div>

      {portfolioQ.isLoading ? <LoadingCardGrid /> : null}
      {portfolioQ.error ? <ErrorAlert error={portfolioQ.error} /> : null}

      {portfolioQ.data ? (
        <div className="grid gap-4 md:grid-cols-3">
          <Card>
            <CardHeader className="pb-2">
              <CardDescription>Market value</CardDescription>
              <CardTitle className="text-2xl font-semibold tracking-tight">
                {compactUsdLike(portfolioQ.data.totals.market_value)}
              </CardTitle>
            </CardHeader>
            <CardContent className="text-xs text-muted-foreground">
              uPnL {compactUsdLike(portfolioQ.data.totals.unrealized_pnl)} · rPnL{" "}
              {compactUsdLike(portfolioQ.data.totals.realized_pnl)}
            </CardContent>
          </Card>
          <Card>
            <CardHeader className="pb-2">
              <CardDescription>Unpriced</CardDescription>
              <CardTitle className="text-2xl font-semibold tracking-tight">
                {portfolioQ.data.unpriced_symbols.length}
              </CardTitle>
            </CardHeader>
            <CardContent className="text-xs text-muted-foreground">
              {portfolioQ.data.unpriced_symbols.length
                ? portfolioQ.data.unpriced_symbols.join(", ")
                : "All marks present"}
            </CardContent>
          </Card>
          <Card>
            <CardHeader className="pb-2">
              <CardDescription>Lineage</CardDescription>
              <CardTitle className="text-sm font-medium">
                {portfolioQ.data.as_of_event_time
                  ? new Date(portfolioQ.data.as_of_event_time).toLocaleString()
                  : "—"}
              </CardTitle>
            </CardHeader>
            <CardContent className="text-xs text-muted-foreground">
              As-of fields are omitted unless the server can provide a single coherent tuple.
            </CardContent>
          </Card>
        </div>
      ) : null}

      <Tabs defaultValue="positions">
        <TabsList className="w-full justify-start overflow-x-auto">
          <TabsTrigger value="positions">Positions</TabsTrigger>
          <TabsTrigger value="risk">Risk</TabsTrigger>
          <TabsTrigger value="scenarios">Scenarios</TabsTrigger>
          <TabsTrigger value="insights">Insights</TabsTrigger>
        </TabsList>

        <TabsContent value="positions">
          <Card>
            <CardHeader>
              <CardTitle>Holdings</CardTitle>
              <CardDescription>Projection-backed table with client-side controls.</CardDescription>
            </CardHeader>
            <CardContent>
              {portfolioQ.data?.positions?.length ? (
                <PositionsTable rows={portfolioQ.data.positions} />
              ) : (
                <div className="text-sm text-muted-foreground">No positions.</div>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="risk">
          <Card>
            <CardHeader>
              <CardTitle>Risk</CardTitle>
              <CardDescription>
                When the API can’t price or lacks return history, you’ll see a structured error with request IDs.
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              {riskQ.isLoading ? <LoadingCardGrid /> : null}
              {riskQ.error ? <ErrorAlert error={riskQ.error} /> : null}
              {riskQ.data ? (
                <div className="grid gap-4 lg:grid-cols-2">
                  <div className="space-y-3">
                    <div>
                      <div className="text-xs text-muted-foreground">VaR (95%, 1d)</div>
                      <div className="text-3xl font-semibold tracking-tight">
                        {compactUsdLike(riskQ.data.var_95_1d)}
                      </div>
                    </div>
                    <div className="grid grid-cols-2 gap-3">
                      <div>
                        <div className="text-xs text-muted-foreground">σ portfolio (1d)</div>
                        <div className="text-lg font-semibold">
                          {compactNumber(riskQ.data.volatility.sigma_1d_portfolio)}
                        </div>
                      </div>
                      <div>
                        <div className="text-xs text-muted-foreground">HHI</div>
                        <div className="text-lg font-semibold">
                          {compactNumber(riskQ.data.concentration.hhi)}
                        </div>
                      </div>
                    </div>
                  </div>
                  <div>
                    <div className="mb-2 text-sm font-medium">Exposure</div>
                    <ExposureBars rows={riskQ.data.exposure} />
                  </div>
                </div>
              ) : null}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="scenarios">
          <Card>
            <CardHeader>
              <CardTitle>Scenario lab</CardTitle>
              <CardDescription>
                Shocks are percentage changes to marks (`type: PCT`). Example:{" "}
                <span className="font-mono">0.10</span> means +10%.
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <form
                className="space-y-4"
                onSubmit={scenarioForm.handleSubmit(async (v) => {
                  scenarioM.reset();
                  await scenarioM.mutateAsync({
                    shocks: v.shocks.map((s) => ({
                      symbol: s.symbol,
                      type: "PCT",
                      value: s.value,
                    })),
                  });
                })}
              >
                <div className="space-y-3">
                  {shocksFieldArray.fields.map((field, idx) => (
                    <div
                      key={field.id}
                      className="grid gap-3 sm:grid-cols-[1fr_120px_auto] sm:items-end"
                    >
                      <div className="space-y-2">
                        <Label>Symbol</Label>
                        <Input {...scenarioForm.register(`shocks.${idx}.symbol`)} />
                      </div>
                      <div className="space-y-2">
                        <Label>PCT shock</Label>
                        <Input {...scenarioForm.register(`shocks.${idx}.value`)} />
                      </div>
                      <Button
                        type="button"
                        variant="outline"
                        onClick={() => shocksFieldArray.remove(idx)}
                        disabled={shocksFieldArray.fields.length <= 1}
                      >
                        Remove
                      </Button>
                    </div>
                  ))}
                </div>
                <div className="flex flex-wrap gap-2">
                  <Button
                    type="button"
                    variant="secondary"
                    onClick={() => shocksFieldArray.append({ symbol: "", value: "0.05" })}
                  >
                    Add shock
                  </Button>
                  <Button type="submit" disabled={scenarioM.isPending}>
                    {scenarioM.isPending ? "Running…" : "Run scenario"}
                  </Button>
                </div>
              </form>

              <Separator />

              {scenarioM.error ? <ErrorAlert error={scenarioM.error} /> : null}

              {scenarioM.data ? (
                <div className="grid gap-4 lg:grid-cols-2">
                  <Card className="border-dashed">
                    <CardHeader>
                      <CardTitle className="text-base">Base vs shocked</CardTitle>
                      <CardDescription>Market value delta highlights the scenario impact.</CardDescription>
                    </CardHeader>
                    <CardContent className="space-y-2 text-sm">
                      <div className="flex items-center justify-between gap-3">
                        <span className="text-muted-foreground">Δ Market value</span>
                        <span className="font-medium">
                          {compactUsdLike(scenarioM.data.delta.market_value)}
                        </span>
                      </div>
                      <div className="flex items-center justify-between gap-3">
                        <span className="text-muted-foreground">Δ Unrealized</span>
                        <span className="font-medium">
                          {compactUsdLike(scenarioM.data.delta.unrealized_pnl)}
                        </span>
                      </div>
                      <div className="flex items-center justify-between gap-3">
                        <span className="text-muted-foreground">Δ Realized</span>
                        <span className="font-medium">
                          {compactUsdLike(scenarioM.data.delta.realized_pnl)}
                        </span>
                      </div>
                    </CardContent>
                  </Card>
                  <Card className="border-dashed">
                    <CardHeader>
                      <CardTitle className="text-base">Shocked marks (totals)</CardTitle>
                      <CardDescription>Full position tables remain available in the response model.</CardDescription>
                    </CardHeader>
                    <CardContent className="text-sm">
                      <div className="flex items-center justify-between gap-3">
                        <span className="text-muted-foreground">Shocked MV</span>
                        <span className="font-medium">
                          {compactUsdLike(scenarioM.data.shocked.totals.market_value)}
                        </span>
                      </div>
                    </CardContent>
                  </Card>
                </div>
              ) : (
                <div className="text-sm text-muted-foreground">
                  Run a scenario to see shocked valuations side-by-side with the base snapshot.
                </div>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="insights">
          <Card>
            <CardHeader>
              <CardTitle>Explain</CardTitle>
              <CardDescription>
                Calls `POST /v1/portfolios/:id/insights/explain`. If OpenAI isn’t configured, the API returns a structured 503-style envelope — the UI surfaces it verbatim.
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <form
                className="space-y-3"
                onSubmit={insightsForm.handleSubmit(async (v) => {
                  insightsM.reset();
                  const payload = {
                    audience: v.audience,
                    prompt: v.prompt?.trim() ? v.prompt.trim() : undefined,
                  };
                  await insightsM.mutateAsync(payload);
                })}
              >
                <div className="grid gap-3 sm:grid-cols-2">
                  <div className="space-y-2">
                    <Label>Audience</Label>
                                       <Select
                      value={insightsForm.watch("audience")}
                      onValueChange={(v) =>
                        insightsForm.setValue(
                          "audience",
                          v as InsightsValues["audience"],
                        )
                      }
                    >
                      <SelectTrigger>
                        <SelectValue placeholder="Audience" />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="risk">Risk</SelectItem>
                        <SelectItem value="pm">PM</SelectItem>
                        <SelectItem value="exec">Exec</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                  <div className="space-y-2">
                    <Label>Focus (optional)</Label>
                    <Input placeholder="e.g. concentration vs vol" {...insightsForm.register("prompt")} />
                  </div>
                </div>
                <Button type="submit" disabled={insightsM.isPending}>
                  {insightsM.isPending ? "Generating…" : "Generate explanation"}
                </Button>
              </form>

              {insightsM.error ? <ErrorAlert error={insightsM.error} /> : null}

              {insightsM.data ? (
                <div className="space-y-3">
                  <div className="flex flex-wrap gap-2">
                    <Badge variant="outline">model: {insightsM.data.model}</Badge>
                    <Badge variant="outline">
                      metrics: {insightsM.data.used_metrics.length}
                    </Badge>
                  </div>
                  <Textarea readOnly value={insightsM.data.explanation} className="min-h-[220px]" />
                </div>
              ) : (
                <Alert>
                  <AlertTitle>Guidance</AlertTitle>
                  <AlertDescription>
                    Keep prompts specific. The server already bundles portfolio, risk, and recent events into model context.
                  </AlertDescription>
                </Alert>
              )}
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  );
}
