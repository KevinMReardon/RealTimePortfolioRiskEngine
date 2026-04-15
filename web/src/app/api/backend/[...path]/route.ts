import { NextResponse } from "next/server";

export const runtime = "nodejs";

function backendBaseUrl() {
  const url =
    process.env.BACKEND_URL ??
    process.env.NEXT_PUBLIC_BACKEND_URL ??
    "http://127.0.0.1:8080";
  return url.replace(/\/$/, "");
}

async function proxy(req: Request, segments: string[]) {
  const targetPath = segments.join("/");
  const incomingUrl = new URL(req.url);
  const targetUrl = `${backendBaseUrl()}/${targetPath}${incomingUrl.search}`;

  const headers = new Headers(req.headers);
  headers.delete("host");
  headers.delete("connection");

  const hasBody = !["GET", "HEAD"].includes(req.method.toUpperCase());
  const upstream = await fetch(targetUrl, {
    method: req.method,
    headers,
    body: hasBody ? await req.text() : undefined,
    redirect: "manual",
  });

  const resHeaders = new Headers(upstream.headers);
  // Avoid leaking hop-by-hop headers across proxies.
  resHeaders.delete("connection");
  resHeaders.delete("transfer-encoding");

  return new NextResponse(upstream.body, {
    status: upstream.status,
    headers: resHeaders,
  });
}

export async function GET(
  req: Request,
  ctx: { params: Promise<{ path?: string[] }> },
) {
  const { path } = await ctx.params;
  return proxy(req, path ?? []);
}

export async function POST(
  req: Request,
  ctx: { params: Promise<{ path?: string[] }> },
) {
  const { path } = await ctx.params;
  return proxy(req, path ?? []);
}

export async function PUT(
  req: Request,
  ctx: { params: Promise<{ path?: string[] }> },
) {
  const { path } = await ctx.params;
  return proxy(req, path ?? []);
}

export async function PATCH(
  req: Request,
  ctx: { params: Promise<{ path?: string[] }> },
) {
  const { path } = await ctx.params;
  return proxy(req, path ?? []);
}

export async function DELETE(
  req: Request,
  ctx: { params: Promise<{ path?: string[] }> },
) {
  const { path } = await ctx.params;
  return proxy(req, path ?? []);
}
