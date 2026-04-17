import { cookies } from "next/headers";

export const AUTH_COOKIE = "rpre_session";

export type SessionPayload = {
  user_id: string;
  work_email: string;
  display_name: string;
};

function backendBaseUrl() {
  const url =
    process.env.BACKEND_URL ??
    process.env.NEXT_PUBLIC_BACKEND_URL ??
    "http://127.0.0.1:8080";
  return url.replace(/\/$/, "");
}

export async function readSessionFromCookies(): Promise<SessionPayload | null> {
  const jar = await cookies();
  const cookieHeader = jar
    .getAll()
    .map((c) => `${c.name}=${c.value}`)
    .join("; ");
  if (!cookieHeader) return null;
  const res = await fetch(`${backendBaseUrl()}/v1/auth/me`, {
    method: "GET",
    headers: { cookie: cookieHeader },
    cache: "no-store",
  });
  if (!res.ok) return null;
  const body = (await res.json().catch(() => null)) as SessionPayload | null;
  if (!body || typeof body.work_email !== "string" || typeof body.display_name !== "string") {
    return null;
  }
  return body;
}
