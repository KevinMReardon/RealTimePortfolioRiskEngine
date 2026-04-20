"use client";

import Link from "next/link";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { zodResolver } from "@hookform/resolvers/zod";

import { useCreatePortfolioMutation, usePortfoliosQuery } from "@/hooks/use-portfolio-api";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Badge } from "@/components/ui/badge";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { ErrorAlert } from "@/components/feedback/query-states";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

const schema = z.object({
  name: z.string().min(1, "Name is required"),
  base_currency: z.string().min(3, "Use a 3-letter code").max(3, "Use a 3-letter code"),
  alpaca_account_mode: z.enum(["paper", "live"]),
  alpaca_key_id: z.string().optional(),
  alpaca_secret_key: z.string().optional(),
  alpaca_base_url: z.string().optional(),
});

type FormValues = z.infer<typeof schema>;

export function PortfoliosManager() {
  const portfoliosQ = usePortfoliosQuery();
  const createM = useCreatePortfolioMutation();
  const items = portfoliosQ.data ?? [];

  const form = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      name: "Primary book",
      base_currency: "USD",
      alpaca_account_mode: "paper",
      alpaca_key_id: "",
      alpaca_secret_key: "",
      alpaca_base_url: "",
    },
  });

  const hasBook = items.length >= 1;

  return (
    <div className="mx-auto max-w-6xl space-y-6 animate-fade-in">
      <div className="space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight">Portfolio</h1>
        <p className="text-sm text-muted-foreground">
          Single-book workspace: one catalog row ties your risk engine, manual ingest, and Alpaca fill sync.
        </p>
      </div>

      <div className="grid gap-4 lg:grid-cols-3">
        {!hasBook ? (
          <Card className="lg:col-span-1">
            <CardHeader>
              <CardTitle>Create your book</CardTitle>
              <CardDescription>A UUID is assigned server-side on create.</CardDescription>
            </CardHeader>
            <CardContent>
              <form
              className="space-y-3"
              onSubmit={form.handleSubmit(async (v) => {
                createM.reset();
                const keyID = (v.alpaca_key_id ?? "").trim();
                const secret = (v.alpaca_secret_key ?? "").trim();
                const created = await createM.mutateAsync({
                  name: v.name.trim(),
                  base_currency: v.base_currency.toUpperCase(),
                  alpaca:
                    keyID && secret
                      ? {
                          account_mode: v.alpaca_account_mode,
                          sync_enabled: true,
                          key_id: keyID,
                          secret_key: secret,
                          base_url: (v.alpaca_base_url ?? "").trim() || undefined,
                        }
                      : undefined,
                });
                form.reset({
                  name: created.name,
                  base_currency: created.base_currency,
                  alpaca_account_mode: "paper",
                  alpaca_key_id: "",
                  alpaca_secret_key: "",
                  alpaca_base_url: "",
                });
              })}
            >
              <div className="space-y-2">
                <Label htmlFor="name">Name</Label>
                <Input id="name" {...form.register("name")} />
                {form.formState.errors.name?.message ? (
                  <p className="text-xs text-destructive">
                    {form.formState.errors.name.message}
                  </p>
                ) : null}
              </div>
              <div className="space-y-2">
                <Label htmlFor="base_currency">Base currency</Label>
                <Select
                  value={form.watch("base_currency")}
                  onValueChange={(v) => form.setValue("base_currency", v)}
                >
                  <SelectTrigger id="base_currency">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="USD">USD</SelectItem>
                    <SelectItem value="EUR">EUR</SelectItem>
                    <SelectItem value="GBP">GBP</SelectItem>
                    <SelectItem value="JPY">JPY</SelectItem>
                  </SelectContent>
                </Select>
                {form.formState.errors.base_currency?.message ? (
                  <p className="text-xs text-destructive">
                    {form.formState.errors.base_currency.message}
                  </p>
                ) : null}
              </div>
              <div className="rounded-md border bg-muted/20 p-3 space-y-3">
                <div className="text-sm font-medium">Alpaca link (optional)</div>
                <p className="text-xs text-muted-foreground">
                  Provide portfolio-specific Alpaca credentials to tie this portfolio directly to a broker account.
                </p>
                <div className="space-y-2">
                  <Label htmlFor="alpaca_account_mode">Account mode</Label>
                  <Select
                    value={form.watch("alpaca_account_mode")}
                    onValueChange={(v) => form.setValue("alpaca_account_mode", v as "paper" | "live")}
                  >
                    <SelectTrigger id="alpaca_account_mode">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="paper">Paper</SelectItem>
                      <SelectItem value="live">Live</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-2">
                  <Label htmlFor="alpaca_key_id">API key id</Label>
                  <Input id="alpaca_key_id" {...form.register("alpaca_key_id")} />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="alpaca_secret_key">API secret key</Label>
                  <Input id="alpaca_secret_key" type="password" {...form.register("alpaca_secret_key")} />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="alpaca_base_url">Base URL (optional)</Label>
                  <Input id="alpaca_base_url" placeholder="https://paper-api.alpaca.markets" {...form.register("alpaca_base_url")} />
                </div>
              </div>
              {createM.error ? <ErrorAlert error={createM.error} /> : null}
              <Button className="w-full" type="submit" disabled={createM.isPending}>
                {createM.isPending ? "Creating…" : "Create portfolio"}
              </Button>
            </form>
          </CardContent>
        </Card>
        ) : (
          <Card className="lg:col-span-1 border-muted">
            <CardHeader>
              <CardTitle>Book created</CardTitle>
              <CardDescription>
                This deployment allows one portfolio per account. Alpaca sync linkage is DB-backed
                (`alpaca_account_mode` + `alpaca_sync_enabled`) and applies to the UUID below.
              </CardDescription>
            </CardHeader>
          </Card>
        )}

        <Card className="lg:col-span-2">
          <CardHeader className="flex flex-row items-start justify-between gap-4 space-y-0">
            <div className="space-y-1">
              <CardTitle>Catalog</CardTitle>
              <CardDescription>Portfolio row backing events and projections.</CardDescription>
            </div>
            <Badge variant="outline">{items.length} total</Badge>
          </CardHeader>
          <CardContent>
            {portfoliosQ.error ? <ErrorAlert error={portfoliosQ.error} /> : null}
            {items.length ? (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Name</TableHead>
                    <TableHead>Currency</TableHead>
                      <TableHead>Linked</TableHead>
                      <TableHead>Mode</TableHead>
                    <TableHead>Alpaca account</TableHead>
                    <TableHead>ID</TableHead>
                    <TableHead className="text-right">Actions</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {items.map((p) => (
                    <TableRow key={p.portfolio_id}>
                      <TableCell className="font-medium">{p.name}</TableCell>
                      <TableCell>{p.base_currency}</TableCell>
                      <TableCell>{p.alpaca_linked ? "Yes" : "No"}</TableCell>
                      <TableCell className="uppercase">{p.alpaca_account_mode}</TableCell>
                      <TableCell className="font-mono text-xs text-muted-foreground">
                        {p.alpaca_account_id ?? "—"}
                      </TableCell>
                      <TableCell className="font-mono text-xs">{p.portfolio_id}</TableCell>
                      <TableCell className="text-right">
                        <div className="flex justify-end gap-2">
                          <Button asChild size="sm" variant="secondary">
                            <Link href={`/portfolios/${p.portfolio_id}`}>Open</Link>
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            ) : (
              <Alert>
                <AlertTitle>Nothing saved yet</AlertTitle>
                <AlertDescription>
                  Create your first portfolio to start ingesting trades and viewing risk metrics.
                </AlertDescription>
              </Alert>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
