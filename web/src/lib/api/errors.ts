import type { APIErrorBody } from "@/lib/api/types";

/** Use when catching unknown errors — instanceof ApiError can fail across bundles */
export function errorHttpStatus(error: unknown): number | undefined {
  if (!error || typeof error !== "object") return undefined;
  const status = (error as { status?: unknown }).status;
  return typeof status === "number" ? status : undefined;
}

export class ApiError extends Error {
  readonly status: number;
  readonly body: APIErrorBody | null;

  constructor(message: string, status: number, body: APIErrorBody | null) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.body = body;
  }
}

export function isAPIErrorBody(v: unknown): v is APIErrorBody {
  if (!v || typeof v !== "object") return false;
  const o = v as Record<string, unknown>;
  return (
    typeof o.error_code === "string" &&
    typeof o.message === "string" &&
    typeof o.request_id === "string" &&
    typeof o.details === "object" &&
    o.details !== null
  );
}
