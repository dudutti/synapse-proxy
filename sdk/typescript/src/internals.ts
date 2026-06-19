/**
 * Internal HTTP wrapper for the Synapse REST extensions.
 *
 * We use the standard fetch API (Node 18+) and a thin error mapper
 * to keep the SDK dependency surface small. The OpenAI client handles
 * the chat completions, so this file only deals with the dashboard's
 * analytics endpoints.
 */

export class SynapseProxyError extends Error {
  status?: number;
  body?: unknown;
  constructor(message: string, status?: number, body?: unknown) {
    super(message);
    this.name = "SynapseProxyError";
    this.status = status;
    this.body = body;
  }
}

export class AuthenticationError extends SynapseProxyError {
  constructor(message: string, status?: number, body?: unknown) {
    super(message, status, body);
    this.name = "AuthenticationError";
  }
}

export class APIError extends SynapseProxyError {
  constructor(message: string, status?: number, body?: unknown) {
    super(message, status, body);
    this.name = "APIError";
  }
}

const DEFAULTS = {
  timeoutMs: 60_000,
};

/**
 * Build the absolute URL for a Synapse extension endpoint.
 *
 * The base URL points to /v1 (e.g. https://synapse-proxy.com/v1). The
 * dashboard's analytics API sits at /api/analytics/* on the same host,
 * just one path up.
 */
export function relativeUrl(baseUrl: string, path: string): string {
  const cleanBase = baseUrl.replace(/\/$/, "");
  if (cleanBase.endsWith("/v1")) {
    return cleanBase.slice(0, -3) + path;
  }
  return cleanBase + path;
}

interface RequestOpts {
  method: "GET" | "POST";
  body?: unknown;
  params?: Record<string, string | number | boolean>;
  apiKey: string;
  baseUrl: string;
  timeoutMs?: number;
}

export async function request<T>(opts: RequestOpts): Promise<T> {
  const url = new URL(relativeUrl(opts.baseUrl, ""));
  if (opts.params) {
    for (const [k, v] of Object.entries(opts.params)) {
      url.searchParams.set(k, String(v));
    }
  }
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), opts.timeoutMs ?? DEFAULTS.timeoutMs);
  try {
    const res = await fetch(url.toString(), {
      method: opts.method,
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${opts.apiKey}`,
      },
      body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
      signal: controller.signal,
    });
    const text = await res.text();
    let parsed: unknown = undefined;
    if (text) {
      try {
        parsed = JSON.parse(text);
      } catch {
        parsed = { raw: text };
      }
    }
    if (!res.ok) {
      if (res.status === 401 || res.status === 403) {
        throw new AuthenticationError(
          `Authentication failed: ${JSON.stringify(parsed)}`,
          res.status,
          parsed,
        );
      }
      throw new APIError(
        `Synapse Proxy returned ${res.status}: ${JSON.stringify(parsed)}`,
        res.status,
        parsed,
      );
    }
    return parsed as T;
  } catch (e) {
    if (e instanceof SynapseProxyError) {
      throw e;
    }
    throw new SynapseProxyError(
      `Network error: ${(e as Error).message}`,
    );
  } finally {
    clearTimeout(timeout);
  }
}
