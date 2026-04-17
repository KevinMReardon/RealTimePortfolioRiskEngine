"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import {
  ArrowLeftRight,
  BarChart3,
  LayoutDashboard,
  LineChart,
  LogOut,
  Shield,
} from "lucide-react";

import type { SessionPayload } from "@/lib/auth/session";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

const nav = [
  { href: "/dashboard", label: "Overview", icon: LayoutDashboard },
  { href: "/portfolios", label: "Portfolios", icon: LineChart },
  { href: "/ingest/trade", label: "Record trade", icon: ArrowLeftRight },
  { href: "/ingest/price", label: "Price data", icon: BarChart3 },
] as const;

export function AppShell({
  user,
  children,
}: {
  user: SessionPayload;
  children: React.ReactNode;
}) {
  const pathname = usePathname();
  const router = useRouter();

  async function logout() {
    await fetch("/api/backend/v1/auth/logout", { method: "POST" });
    router.push("/login");
    router.refresh();
  }

  return (
    <div className="min-h-dvh pb-20 lg:grid lg:grid-cols-[260px_1fr] lg:pb-0">
      <aside className="hidden border-r bg-card lg:block">
        <div className="flex h-16 items-center gap-2 px-6">
          <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-primary text-primary-foreground shadow-sm">
            <Shield className="h-4 w-4" aria-hidden />
          </div>
          <div className="leading-tight">
            <div className="text-sm font-semibold tracking-tight">Pulse</div>
            <div className="text-xs text-muted-foreground">Risk console</div>
          </div>
        </div>
        <Separator />
        <nav className="space-y-1 p-3" aria-label="Primary">
          {nav.map((item) => {
            const active =
              pathname === item.href || pathname.startsWith(`${item.href}/`);
            const Icon = item.icon;
            return (
              <Link
                key={item.href}
                href={item.href}
                className={cn(
                  "flex items-center gap-2 rounded-lg px-3 py-2 text-sm transition-colors",
                  active
                    ? "bg-muted text-foreground"
                    : "text-muted-foreground hover:bg-muted/60 hover:text-foreground",
                )}
              >
                <Icon className="h-4 w-4" aria-hidden />
                {item.label}
              </Link>
            );
          })}
        </nav>
        <div className="px-6 py-6 text-xs text-muted-foreground">
          Anchored to your Go API via{" "}
          <span className="font-mono text-[11px]">/api/backend</span>.
        </div>
      </aside>

      <div className="min-w-0">
        <header className="sticky top-0 z-40 border-b bg-background/80 backdrop-blur">
          <div className="flex h-16 items-center justify-between gap-3 px-4 sm:px-6">
            <div className="min-w-0">
              <div className="truncate text-sm font-semibold tracking-tight">
                Real-time portfolio risk
              </div>
              <div className="truncate text-xs text-muted-foreground">
                Read positions, run scenarios, and explain risk — without leaving context.
              </div>
            </div>

            <div className="flex items-center gap-2">
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="outline" size="sm" className="gap-2">
                    <span className="max-w-[160px] truncate">{user.work_email}</span>
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="w-56">
                  <DropdownMenuLabel>
                    <div className="text-xs font-normal text-muted-foreground">
                      Signed in
                    </div>
                    <div className="truncate text-sm">{user.display_name}</div>
                    <div className="truncate text-xs text-muted-foreground">{user.work_email}</div>
                  </DropdownMenuLabel>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem onSelect={() => void logout()}>
                    <LogOut className="h-4 w-4" aria-hidden />
                    Log out
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          </div>
        </header>

        <main className="px-4 py-6 sm:px-6">{children}</main>
      </div>

      <nav
        className="fixed inset-x-0 bottom-0 z-50 border-t bg-background/90 backdrop-blur lg:hidden"
        aria-label="Mobile primary"
      >
        <div className="grid grid-cols-4 gap-1 px-2 py-2">
          {nav.map((item) => {
            const active =
              pathname === item.href || pathname.startsWith(`${item.href}/`);
            const Icon = item.icon;
            return (
              <Link
                key={item.href}
                href={item.href}
                className={cn(
                  "flex flex-col items-center justify-center gap-1 rounded-md py-2 text-[11px]",
                  active
                    ? "text-foreground"
                    : "text-muted-foreground hover:text-foreground",
                )}
              >
                <Icon className="h-4 w-4" aria-hidden />
                <span className="max-w-[72px] truncate">{item.label}</span>
              </Link>
            );
          })}
        </div>
      </nav>
    </div>
  );
}
