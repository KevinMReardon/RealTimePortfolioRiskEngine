"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { zodResolver } from "@hookform/resolvers/zod";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";

const schema = z.object({
  display_name: z.string().min(1),
  work_email: z.string().email(),
  password: z.string().min(8),
});

type FormValues = z.infer<typeof schema>;

export function RegisterForm() {
  const router = useRouter();
  const [error, setError] = React.useState<string | null>(null);
  const [pending, setPending] = React.useState(false);

  const form = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: { display_name: "", work_email: "", password: "" },
  });

  async function onSubmit(values: FormValues) {
    setPending(true);
    setError(null);
    try {
      const registerRes = await fetch("/api/backend/v1/auth/register", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(values),
      });
      if (!registerRes.ok) {
        const body = (await registerRes.json().catch(() => null)) as null | {
          message?: string;
        };
        throw new Error(body?.message ?? "Registration failed");
      }
      const loginRes = await fetch("/api/backend/v1/auth/login", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          work_email: values.work_email,
          password: values.password,
        }),
      });
      if (!loginRes.ok) {
        throw new Error("Account created, but login failed");
      }
      router.push("/dashboard");
      router.refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Registration failed");
    } finally {
      setPending(false);
    }
  }

  return (
    <form className="space-y-4" onSubmit={form.handleSubmit(onSubmit)}>
      {error ? (
        <Alert variant="destructive">
          <AlertTitle>Couldn’t create account</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}
      <div className="space-y-2">
        <Label htmlFor="display_name">Display name</Label>
        <Input id="display_name" placeholder="Alex Chen" {...form.register("display_name")} />
      </div>
      <div className="space-y-2">
        <Label htmlFor="work_email">Work email</Label>
        <Input id="work_email" autoComplete="email" placeholder="you@company.com" {...form.register("work_email")} />
      </div>
      <div className="space-y-2">
        <Label htmlFor="password">Password</Label>
        <Input id="password" type="password" autoComplete="new-password" placeholder="••••••••" {...form.register("password")} />
      </div>
      <Button className="w-full" type="submit" disabled={pending}>
        {pending ? "Creating account…" : "Create account"}
      </Button>
    </form>
  );
}

