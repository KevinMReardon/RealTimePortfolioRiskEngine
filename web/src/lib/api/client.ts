import { ApiError, isAPIErrorBody } from "@/lib/api/errors";

export const API_PREFIX = "/api/backend";

async function parseJsonSafe(res: Response): Promise<unknown | null> {
  const text = await res.text();
  if (!text) return null;
  try {
    return JSON.parse(text) as unknown;
  } catch {
    return null;
  }
}

export async function apiFetch<T>(
  path: string,
  init?: RequestInit,
): Promise<T> {
  const url = `${API_PREFIX}${path.startsWith("/") ? "" : "/"}${path}`;
  const res = await fetch(url, {
    ...init,
    headers: {
      "content-type": "application/json",
      ...(init?.headers ?? {}),
    },
    cache: "no-store",
  });

  const parsed = await parseJsonSafe(res);

  if (!res.ok) {
    const body = isAPIErrorBody(parsed) ? parsed : null;
    const message =
      body?.message ??
      (typeof parsed === "object" &&
 parsed &&
      "message" in parsed &&
      typeof (parsed as { message?: unknown }).message === "string"
        ? (parsed as { message: string }).message
        : `Request failed (${res.status})`);
    throw new ApiError(message, res.status, body);
  }

  return parsed as T;
}
