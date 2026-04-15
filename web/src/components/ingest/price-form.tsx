"use client";

import * as React from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { zodResolver } from "@hookform/resolvers/zod";

import { usePostPriceMutation } from "@/hooks/use-portfolio-api";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { ErrorAlert } from "@/components/feedback/query-states";

const schema = z.object({
  source: z.string().min(1),
  symbol: z.string().min(1),
  price: z.string().min(1),
  currency: z.string().min(1),
  source_sequence: z
    .string()
    .trim()
    .transform((s) => Number(s))
    .refine((n) => Number.isFinite(n) && Number.isInteger(n), {
      message: "Must be an integer",
    }),
});

type FormInput = z.input<typeof schema>;
type FormValues = z.output<typeof schema>;

export function PriceIngestForm() {
  const mutation = usePostPriceMutation();
  const [success, setSuccess] = React.useState<string | null>(null);

  const form = useForm<FormInput, unknown, FormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      source: "console",
      symbol: "AAPL",
      price: "190.25",
      currency: "USD",
      source_sequence: String(Date.now()),
    },
  });

  return (
    <Card className="mx-auto max-w-2xl animate-fade-in">
      <CardHeader>
        <CardTitle>Record price</CardTitle>
        <CardDescription>
          Posts to <span className="font-mono">POST /v1/prices</span>. The server routes the event to a partition UUID derived from the symbol.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form
          className="space-y-4"
          onSubmit={form.handleSubmit(async (v) => {
            setSuccess(null);
            mutation.reset();
            const res = await mutation.mutateAsync({
              idempotency_key: crypto.randomUUID(),
              source: v.source,
              price: {
                symbol: v.symbol,
                price: v.price,
                currency: v.currency,
                source_sequence: v.source_sequence,
              },
            });
            setSuccess(`Event ${res.event_id} (${res.status})`);
            form.setValue("source_sequence", String(Date.now()));
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
            <div className="space-y-2">
              <Label htmlFor="source">Source</Label>
              <Input id="source" {...form.register("source")} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="symbol">Symbol</Label>
              <Input id="symbol" {...form.register("symbol")} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="price">Price</Label>
              <Input id="price" {...form.register("price")} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="currency">Currency</Label>
              <Input id="currency" {...form.register("currency")} />
            </div>
            <div className="space-y-2 sm:col-span-2">
              <Label htmlFor="source_sequence">Source sequence</Label>
              <Input id="source_sequence" type="number" {...form.register("source_sequence")} />
            </div>
          </div>

          <Button type="submit" disabled={mutation.isPending}>
            {mutation.isPending ? "Submitting…" : "Submit price"}
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}
