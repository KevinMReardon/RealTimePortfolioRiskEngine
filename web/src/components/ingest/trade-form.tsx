"use client";

import * as React from "react";
import { useSearchParams } from "next/navigation";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { zodResolver } from "@hookform/resolvers/zod";

import { usePostTradeMutation } from "@/hooks/use-portfolio-api";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { ErrorAlert } from "@/components/feedback/query-states";

const schema = z.object({
  portfolio_id: z.string().uuid(),
  source: z.string().min(1),
  trade_id: z.string().min(1),
  symbol: z.string().min(1),
  side: z.enum(["BUY", "SELL"]),
  quantity: z.string().min(1),
  price: z.string().min(1),
  currency: z.string().min(1),
});

type FormValues = z.infer<typeof schema>;

export function TradeIngestForm() {
  const params = useSearchParams();
  const defaultPortfolio = params.get("portfolio") ?? "";

  const mutation = usePostTradeMutation();
  const [success, setSuccess] = React.useState<string | null>(null);

  const form = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      portfolio_id: defaultPortfolio,
      source: "console",
      trade_id: crypto.randomUUID(),
      symbol: "AAPL",
      side: "BUY",
      quantity: "10",
      price: "190.25",
      currency: "USD",
    },
  });

  React.useEffect(() => {
    if (defaultPortfolio) {
      form.setValue("portfolio_id", defaultPortfolio);
    }
  }, [defaultPortfolio, form]);

  return (
    <Card className="mx-auto max-w-2xl animate-fade-in">
      <CardHeader>
        <CardTitle>Record trade</CardTitle>
        <CardDescription>
          Posts to <span className="font-mono">POST /v1/trades</span> through the same-origin proxy.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form
          className="space-y-4"
          onSubmit={form.handleSubmit(async (v) => {
            setSuccess(null);
            mutation.reset();
            const res = await mutation.mutateAsync({
              portfolio_id: v.portfolio_id,
              idempotency_key: crypto.randomUUID(),
              source: v.source,
              trade: {
                trade_id: v.trade_id,
                symbol: v.symbol,
                side: v.side,
                quantity: v.quantity,
                price: v.price,
                currency: v.currency,
              },
            });
            setSuccess(`Event ${res.event_id} (${res.status})`);
            form.setValue("trade_id", crypto.randomUUID());
          })}
        >
          {success ? (
            <Alert>
              <AlertTitle>Ingested</AlertTitle>
              <AlertDescription>{success}</AlertDescription>
            </Alert>
          ) : null}
          {mutation.error ? <ErrorAlert error={mutation.error} /> : null}

          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-2 sm:col-span-2">
              <Label htmlFor="portfolio_id">Portfolio UUID</Label>
              <Input id="portfolio_id" {...form.register("portfolio_id")} />
              {form.formState.errors.portfolio_id?.message ? (
                <p className="text-xs text-destructive">
                  {form.formState.errors.portfolio_id.message}
                </p>
              ) : null}
            </div>
            <div className="space-y-2">
              <Label htmlFor="source">Source</Label>
              <Input id="source" {...form.register("source")} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="trade_id">Trade id</Label>
              <Input id="trade_id" {...form.register("trade_id")} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="symbol">Symbol</Label>
              <Input id="symbol" {...form.register("symbol")} />
            </div>
            <div className="space-y-2">
              <Label>Side</Label>
              <Select
                value={form.watch("side")}
                onValueChange={(v) => form.setValue("side", v as FormValues["side"])}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="BUY">BUY</SelectItem>
                  <SelectItem value="SELL">SELL</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label htmlFor="quantity">Quantity</Label>
              <Input id="quantity" {...form.register("quantity")} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="price">Price</Label>
              <Input id="price" {...form.register("price")} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="currency">Currency</Label>
              <Input id="currency" {...form.register("currency")} />
            </div>
          </div>

          <Button type="submit" disabled={mutation.isPending}>
            {mutation.isPending ? "Submitting…" : "Submit trade"}
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}
