"use client";

import Link from "next/link";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { zodResolver } from "@hookform/resolvers/zod";

import { useSavedPortfolios } from "@/hooks/use-saved-portfolios";
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

const schema = z.object({
  id: z.string().uuid("Must be a UUID"),
  label: z.string().min(1, "Label is required"),
});

type FormValues = z.infer<typeof schema>;

export function PortfoliosManager() {
  const { items, upsert, remove } = useSavedPortfolios();

  const form = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: { id: "", label: "Primary book" },
  });

  return (
    <div className="mx-auto max-w-6xl space-y-6 animate-fade-in">
      <div className="space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight">Portfolios</h1>
        <p className="text-sm text-muted-foreground">
          v1 reads portfolios by UUID (`GET /v1/portfolios/:id`). Save IDs you care about — no directory API yet.
        </p>
      </div>

      <div className="grid gap-4 lg:grid-cols-3">
        <Card className="lg:col-span-1">
          <CardHeader>
            <CardTitle>Add portfolio</CardTitle>
            <CardDescription>Stored locally in your browser.</CardDescription>
          </CardHeader>
          <CardContent>
            <form
              className="space-y-3"
              onSubmit={form.handleSubmit((v) => {
                upsert({ id: v.id, label: v.label });
                form.reset({ id: "", label: v.label });
              })}
            >
              <div className="space-y-2">
                <Label htmlFor="label">Label</Label>
                <Input id="label" {...form.register("label")} />
                {form.formState.errors.label?.message ? (
                  <p className="text-xs text-destructive">
                    {form.formState.errors.label.message}
                  </p>
                ) : null}
              </div>
              <div className="space-y-2">
                <Label htmlFor="id">Portfolio UUID</Label>
                <Input id="id" placeholder="xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx" {...form.register("id")} />
                {form.formState.errors.id?.message ? (
                  <p className="text-xs text-destructive">
                    {form.formState.errors.id.message}
                  </p>
                ) : null}
              </div>
              <Button className="w-full" type="submit">
                Save
              </Button>
            </form>
          </CardContent>
        </Card>

        <Card className="lg:col-span-2">
          <CardHeader className="flex flex-row items-start justify-between gap-4 space-y-0">
            <div className="space-y-1">
              <CardTitle>Saved</CardTitle>
              <CardDescription>Quick access + deep links.</CardDescription>
            </div>
            <Badge variant="outline">{items.length} total</Badge>
          </CardHeader>
          <CardContent>
            {items.length ? (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Label</TableHead>
                    <TableHead>ID</TableHead>
                    <TableHead className="text-right">Actions</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {items.map((p) => (
                    <TableRow key={p.id}>
                      <TableCell className="font-medium">{p.label}</TableCell>
                      <TableCell className="font-mono text-xs">{p.id}</TableCell>
                      <TableCell className="text-right">
                        <div className="flex justify-end gap-2">
                          <Button asChild size="sm" variant="secondary">
                            <Link href={`/portfolios/${p.id}`}>Open</Link>
                          </Button>
                          <Button
                            size="sm"
                            variant="outline"
                            type="button"
                            onClick={() => remove(p.id)}
                          >
                            Remove
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
                  Paste a portfolio UUID from your environment to anchor the console. Reserved price-stream UUIDs are rejected by the API — use real portfolio IDs.
                </AlertDescription>
              </Alert>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
