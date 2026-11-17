/**
 * Loopback HTTP. The account bearer is sent only to the account origin.
 * Redirects are refused. Query strings cannot carry a session.
 */

import { ApiError, assertNoSecretQuery } from "./model.ts";
import type { ByteResponse } from "./playback.ts";

const JSON_LIMIT = 1 << 20;

export async function getJson(url: string, signal: AbortSignal, token = ""): Promise<unknown> {
  assertNoSecretQuery(url);
  const headers = new Headers({ Accept: "application/json" });
  if (token) headers.set("Authorization", `Bearer ${token}`);
  const response = await fetch(url, { method: "GET", headers, signal, redirect: "error", credentials: "omit" });
  return readBody(response);
}

export async function postJson(url: string, body: unknown, signal: AbortSignal, token = "", ceremony = ""): Promise<unknown> {
  assertNoSecretQuery(url);
  const headers = new Headers({ Accept: "application/json", "Content-Type": "application/json" });
  if (token) headers.set("Authorization", `Bearer ${token}`);
  if (ceremony) headers.set("X-SVN-Ceremony", ceremony);
  const response = await fetch(url, {
    method: "POST",
    headers,
    body: JSON.stringify(body),
    signal,
    redirect: "error",
    credentials: "omit",
  });
  return readBody(response);
}

export async function getBytes(url: string, signal: AbortSignal, limit: number): Promise<ByteResponse> {
  assertNoSecretQuery(url);
  const timed = AbortSignal.any([signal, AbortSignal.timeout(800)]);
  let response: Response;
  try {
    response = await fetch(url, { method: "GET", signal: timed, redirect: "error", credentials: "omit" });
  } catch {
    if (signal.aborted) throw new ApiError("CANCELLED", 0);
    throw new ApiError("UNAVAILABLE", 0);
  }
  if (signal.aborted) throw new ApiError("CANCELLED", 0);
  const reader = response.body?.getReader();
  if (!reader) return { status: response.status, body: new Uint8Array() };
  const chunks: Uint8Array[] = [];
  let size = 0;
  for (;;) {
    const step = await reader.read();
    if (step.done) break;
    size += step.value.byteLength;
    if (size > limit) {
      await reader.cancel();
      throw new ApiError("REFUSED", response.status, "limit");
    }
    chunks.push(step.value);
  }
  const body = new Uint8Array(size);
  let offset = 0;
  for (const chunk of chunks) {
    body.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return { status: response.status, body };
}

async function readBody(response: Response): Promise<unknown> {
  const text = await response.text();
  if (text.length > JSON_LIMIT) throw new ApiError("REFUSED", response.status, "limit");
  if (!response.ok) {
    let code = "UNAVAILABLE";
    try {
      const parsed = JSON.parse(text) as { error?: { code?: string } };
      if (parsed.error?.code) code = parsed.error.code;
    } catch {
      code = response.status === 401 ? "UNAUTHENTICATED" : "UNAVAILABLE";
    }
    throw new ApiError(code, response.status);
  }
  try {
    return JSON.parse(text) as unknown;
  } catch {
    throw new ApiError("DECODE", response.status);
  }
}
