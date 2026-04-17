import Link from "next/link";
import { Suspense } from "react";

import { LoginForm } from "@/components/auth/login-form";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";

export default function LoginPage() {
  return (
    <div className="min-h-dvh bg-background px-4 py-10 sm:px-6">
      <div className="mx-auto flex w-full max-w-md flex-col gap-6">
        <div className="space-y-2 text-center sm:text-left">
          <div className="text-xs font-medium text-muted-foreground">
            Pulse Risk Console
          </div>
          <h1 className="text-2xl font-semibold tracking-tight">Sign in</h1>
          <p className="text-sm text-muted-foreground">
            Sign in with your account to see only portfolios you own.
          </p>
        </div>

        <Card className="animate-fade-in">
          <CardHeader>
            <CardTitle>Welcome back</CardTitle>
            <CardDescription>
              Use your work email and password.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Suspense fallback={<Skeleton className="h-40 w-full" />}>
              <LoginForm />
            </Suspense>
          </CardContent>
        </Card>

        <p className="text-center text-xs text-muted-foreground sm:text-left">
          Need an account?{" "}
          <Link className="underline underline-offset-4" href="/register">
            Create one
          </Link>
        </p>
      </div>
    </div>
  );
}
