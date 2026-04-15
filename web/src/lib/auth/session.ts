import { cookies } from "next/headers";

export const AUTH_COOKIE = "rpre_session";

export type SessionPayload = {
  email: string;
  name?: string;
};

export function encodeSession(payload: SessionPayload) {
  return Buffer.from(JSON.stringify(payload), "utf8").toString("base64url");
}

export function decodeSession(token: string): SessionPayload | null {
  try {
    const json = Buffer.from(token, "base64url").toString("utf8");
    const parsed = JSON.parse(json) as unknown;
    if (!parsed || typeof parsed !== "object") return null;
    const email = (parsed as { email?: unknown }).email;
    if (typeof email !== "string" || !email) return null;
    const name = (parsed as { name?: unknown }).name;
    return { email, name: typeof name === "string" ? name : undefined };
  } catch {
    return null;
  }
}

export async function readSessionFromCookies(): Promise<SessionPayload | null> {
  const jar = await cookies();
  const raw = jar.get(AUTH_COOKIE)?.value;
  if (!raw) return null;
  return decodeSession(raw);
}
