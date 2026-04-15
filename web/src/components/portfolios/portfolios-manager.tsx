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
});

type FormValues = z.infer<typeof schema>;

export function PortfoliosManager() {
  const portfoliosQ = usePortfoliosQuery();
  const createM = useCreatePortfolioMutation();
  const items = portfoliosQ.data ?? [];

  const form = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: { name: "Primary book", base_currency: "USD" },
  });

  return (
    <div className="mx-auto max-w-6xl space-y-6 animate-fade-in">
      <div className="space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight">Portfolios</h1>
        <p className="text-sm text-muted-foreground">
          Create and browse portfolio records from the backend catalog (`GET/POST /v1/portfolios`).
        </p>
      </div>

      <div className="grid gap-4 lg:grid-cols-3">
        <Card className="lg:col-span-1">
          <CardHeader>
            <CardTitle>Add portfolio</CardTitle>
            <CardDescription>A UUID is generated server-side on create.</CardDescription>
          </CardHeader>
          <CardContent>
            <form
              className="space-y-3"
              onSubmit={form.handleSubmit(async (v) => {
                createM.reset();
                const created = await createM.mutateAsync({
                  name: v.name.trim(),
                  base_currency: v.base_currency.toUpperCase(),
                });
                form.reset({ name: created.name, base_currency: created.base_currency });
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
              {createM.error ? <ErrorAlert error={createM.error} /> : null}
              <Button className="w-full" type="submit" disabled={createM.isPending}>
                {createM.isPending ? "Creating…" : "Create portfolio"}
              </Button>
            </form>
          </CardContent>
        </Card>

        <Card className="lg:col-span-2">
          <CardHeader className="flex flex-row items-start justify-between gap-4 space-y-0">
            <div className="space-y-1">
              <CardTitle>Catalog</CardTitle>
              <CardDescription>Backend-managed portfolio directory.</CardDescription>
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
                    <TableHead>ID</TableHead>
                    <TableHead className="text-right">Actions</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {items.map((p) => (
                    <TableRow key={p.portfolio_id}>
                      <TableCell className="font-medium">{p.name}</TableCell>
                      <TableCell>{p.base_currency}</TableCell>
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
