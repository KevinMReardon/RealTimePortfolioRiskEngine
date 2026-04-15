import { redirect } from "next/navigation";

import { AppShell } from "@/components/layout/app-shell";
import { readSessionFromCookies } from "@/lib/auth/session";

export default async function DashboardLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const session = await readSessionFromCookies();
  if (!session) redirect("/login");

  return <AppShell user={session}>{children}</AppShell>;
}
