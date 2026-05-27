"use client";

import * as React from "react";
import { Activity, AlertCircle, CheckCircle2, Clock, Radio } from "lucide-react";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { ErrorAlert } from "@/components/feedback/query-states";
import { usePriceFeedStatusQuery } from "@/hooks/use-price-data";

function fmtTime(iso: string | undefined) {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(d);
}

function fmtRelative(iso: string | undefined) {
  if (!iso) return null;
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return null;
  const diff = Date.now() - d.getTime();
  if (diff < 0) return null;
  const sec = Math.floor(diff / 1000);
  if (sec < 60) return `${sec}s ago`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min}m ago`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr}h ago`;
  const day = Math.floor(hr / 24);
  return `${day}d ago`;
}

function StatCell({
  label,
  value,
  hint,
}: {
  label: string;
  value: React.ReactNode;
  hint?: string | null;
}) {
  return (
    <div className="space-y-0.5">
      <div className="text-[11px] uppercase tracking-wide text-muted-foreground">
        {label}
      </div>
      <div className="text-sm font-medium">{value}</div>
      {hint ? <div className="text-[11px] text-muted-foreground">{hint}</div> : null}
    </div>
  );
}

export function FeedStatusCard() {
  const q = usePriceFeedStatusQuery();

  if (q.isPending) {
    return (
      <Card>
        <CardHeader className="pb-2">
          <Skeleton className="h-5 w-40" />
          <Skeleton className="h-4 w-full max-w-md" />
        </CardHeader>
        <CardContent className="space-y-2">
          <Skeleton className="h-20 w-full" />
        </CardContent>
      </Card>
    );
  }

  if (q.isError) {
    return <ErrorAlert error={q.error} title="Could not load feed status" />;
  }

  const s = q.data;
  const active = s.active_provider ?? s.configured_provider;
  const lastSuccessRel = fmtRelative(s.last_successful_fetch_at);

  return (
    <Card>
      <CardHeader className="pb-2">
        <div className="flex items-start justify-between gap-2">
          <div>
            <CardTitle className="text-base flex items-center gap-2">
              <Radio className="h-4 w-4 text-muted-foreground" aria-hidden />
              Feed status
            </CardTitle>
            <CardDescription>Live provider loop health.</CardDescription>
          </div>
          <div className="flex flex-wrap items-center justify-end gap-1.5">
            <Badge variant={s.feed_enabled ? "success" : "outline"}>
              {s.feed_enabled ? (
                <span className="flex items-center gap-1">
                  <CheckCircle2 className="h-3 w-3" aria-hidden />
                  Enabled
                </span>
              ) : (
                "Disabled"
              )}
            </Badge>
            {s.last_tick_used_failover ? (
              <Badge variant="warning">
                <span className="flex items-center gap-1">
                  <AlertCircle className="h-3 w-3" aria-hidden />
                  Failover
                </span>
              </Badge>
            ) : null}
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="grid grid-cols-2 gap-x-4 gap-y-3 sm:grid-cols-3">
          <StatCell
            label="Provider"
            value={active || "—"}
            hint={
              s.active_provider && s.active_provider !== s.configured_provider
                ? `configured: ${s.configured_provider}`
                : null
            }
          />
          <StatCell
            label="Poll interval"
            value={
              s.poll_interval_ms > 0
                ? `${Math.round(s.poll_interval_ms / 1000)}s`
                : "—"
            }
          />
          <StatCell
            label="Watchlist"
            value={`${s.watchlist_count} symbol${s.watchlist_count === 1 ? "" : "s"}`}
          />
          <StatCell
            label="Last success"
            value={
              <span className="inline-flex items-center gap-1">
                <Clock className="h-3 w-3 text-muted-foreground" aria-hidden />
                {fmtTime(s.last_successful_fetch_at)}
              </span>
            }
            hint={lastSuccessRel}
          />
          <StatCell label="Last tick start" value={fmtTime(s.last_tick_started_at)} />
          <StatCell
            label="Last tick finished"
            value={fmtTime(s.last_tick_finished_at)}
            hint={
              typeof s.last_tick_ingested_count === "number"
                ? `ingested ${s.last_tick_ingested_count}`
                : null
            }
          />
        </div>

        {s.last_error ? (
          <Alert variant="destructive">
            <Activity className="h-4 w-4" />
            <AlertTitle>Last feed error</AlertTitle>
            <AlertDescription className="font-mono text-xs whitespace-pre-wrap">
              {s.last_error}
            </AlertDescription>
          </Alert>
        ) : null}
      </CardContent>
    </Card>
  );
}
