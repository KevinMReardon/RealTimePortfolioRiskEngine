import type { APIErrorBody } from "@/lib/api/types";

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
