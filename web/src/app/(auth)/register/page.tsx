import Link from "next/link";
import { CheckCircle2, Lock, Shield } from "lucide-react";

import { RegisterForm } from "@/components/auth/register-form";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";

export default function RegisterPage() {
  return (
    <div className="min-h-dvh bg-background px-4 py-10 sm:px-6">
      <div className="mx-auto grid w-full max-w-5xl gap-6 lg:grid-cols-[1.1fr_0.9fr]">
        <div className="rounded-2xl border bg-card/40 p-6 sm:p-8">
          <div className="mb-8 space-y-2">
            <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
              Pulse Risk Console
            </div>
            <h1 className="text-3xl font-semibold tracking-tight sm:text-4xl">
              Set up your workspace
            </h1>
            <p className="max-w-xl text-sm text-muted-foreground sm:text-base">
              Create your account to track portfolios privately and keep watchlists
              and dashboards scoped to you.
            </p>
          </div>

          <div className="space-y-3">
            <div className="flex items-start gap-3 rounded-lg border bg-background/70 p-3">
              <Shield className="mt-0.5 h-4 w-4 text-primary" aria-hidden />
              <div>
                <div className="text-sm font-medium">Private portfolio access</div>
                <p className="text-xs text-muted-foreground">
                  Only portfolios owned by your account are visible in the app.
                </p>
              </div>
            </div>
            <div className="flex items-start gap-3 rounded-lg border bg-background/70 p-3">
              <CheckCircle2 className="mt-0.5 h-4 w-4 text-primary" aria-hidden />
              <div>
                <div className="text-sm font-medium">Faster onboarding</div>
                <p className="text-xs text-muted-foreground">
                  Create an account once, then sign in directly for future sessions.
                </p>
              </div>
            </div>
            <div className="flex items-start gap-3 rounded-lg border bg-background/70 p-3">
              <Lock className="mt-0.5 h-4 w-4 text-primary" aria-hidden />
              <div>
                <div className="text-sm font-medium">Secure session cookies</div>
                <p className="text-xs text-muted-foreground">
                  Authentication is handled by the backend with HttpOnly sessions.
                </p>
              </div>
            </div>
          </div>
        </div>

        <Card className="animate-fade-in border-primary/30 shadow-sm">
          <CardHeader>
            <CardTitle>Create your account</CardTitle>
            <CardDescription>
              Use your work email and a strong password.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <RegisterForm />
          </CardContent>
          <p className="mt-5 text-center text-xs text-muted-foreground">
            Already have an account?{" "}
            <Link className="underline underline-offset-4" href="/login">
              Sign in
            </Link>
          </p>
        </Card>
      </div>
    </div>
  );
}

