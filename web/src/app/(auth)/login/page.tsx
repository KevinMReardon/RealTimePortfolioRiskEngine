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
            This UI gates access with a session cookie. Wire it to your identity provider when
            you add real auth to the Go API.
          </p>
        </div>

        <Card className="animate-fade-in">
          <CardHeader>
            <CardTitle>Welcome back</CardTitle>
            <CardDescription>
              Use any email for now — password is ignored (demo UX).
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Suspense fallback={<Skeleton className="h-40 w-full" />}>
              <LoginForm />
            </Suspense>
          </CardContent>
        </Card>

        <p className="text-center text-xs text-muted-foreground sm:text-left">
          Backend proxy target is configured with{" "}
          <span className="font-mono">BACKEND_URL</span>.{" "}
          <Link className="underline underline-offset-4" href="/dashboard">
            Try the console
          </Link>
        </p>
      </div>
    </div>
  );
}
