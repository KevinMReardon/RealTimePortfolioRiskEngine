import { NextResponse } from "next/server";
import { AUTH_COOKIE, encodeSession } from "@/lib/auth/session";

export async function POST(req: Request) {
  const body = (await req.json().catch(() => null)) as null | {
    email?: unknown;
    password?: unknown;
    name?: unknown;
  };

  const email = typeof body?.email === "string" ? body.email.trim() : "";
  if (!email) {
    return NextResponse.json({ error: "Email is required" }, { status: 400 });
  }

  const name = typeof body?.name === "string" ? body.name.trim() : undefined;

  // NOTE: The Go API currently has no auth endpoints. This cookie session exists to
  // model a real SaaS login UX and gate the console routes via middleware.
  const token = encodeSession({ email, name });

  const res = NextResponse.json({ ok: true });
  res.cookies.set(AUTH_COOKIE, token, {
    httpOnly: true,
    sameSite: "lax",
    secure: process.env.NODE_ENV === "production",
    path: "/",
    maxAge: 60 * 60 * 24 * 14,
  });
  return res;
}
