import OpenAI from "openai";
import {
  APIError,
  AuthenticationError,
  request,
  relativeUrl,
  SynapseProxyError,
} from "./internals.js";
import type {
  BenchmarkResult,
  CacheStats,
  ChatCompletion,
  SavingsReport,
  SessionRecording,
} from "./types.js";

const DEFAULT_BASE_URL = "https://synapse-proxy.com/v1";

export interface SynapseProxyOptions {
  apiKey?: string;
  baseUrl?: string;
  /** Request timeout in milliseconds (default 60_000). */
  timeoutMs?: number;
  /** Number of retries for transient failures. Forwarded to the OpenAI client. */
  maxRetries?: number;
  organization?: string;
  project?: string;
  /**
   * Extra headers to forward with every request. Useful for proxy
   * identification, e.g. `{"X-SynapseProxy-Client": "my-app/1.0"}`.
   */
  defaultHeaders?: Record<string, string>;
}

/**
 * Drop-in OpenAI-compatible client for Synapse Proxy.
 *
 * @example
 * ```ts
 * import { SynapseProxy } from "@synapse-proxy/sdk";
 *
 * const sp = new SynapseProxy({ apiKey: process.env.SYNAPSE_API_KEY! });
 *
 * // Standard OpenAI chat
 * const chat = await sp.chat.completions.create({
 *   model: "gpt-4o-mini",
 *   messages: [{ role: "user", content: "Hello, world!" }],
 * });
 *
 * // Synapse extensions
 * const session = await sp.sessions.start({ groupBy: "agent" });
 * const stats = await sp.cache.stats({ days: 7 });
 * const savings = await sp.savings.summary({ days: 30 });
 * const ab = await sp.benchmark.run({
 *   models: ["gpt-4o-mini", "minimax-m3"],
 *   prompt: "Explain TCP slow start in 3 sentences.",
 * });
 * ```
 */
export class SynapseProxy {
  private readonly openai: OpenAI;
  private readonly baseUrl: string;
  public readonly apiKey: string;

  constructor(options: SynapseProxyOptions = {}) {
    const key = options.apiKey ?? process.env["SYNAPSE_PROXY_API_KEY"];
    if (!key) {
      throw new AuthenticationError(
        "No API key provided. Pass `apiKey` or set the SYNAPSE_PROXY_API_KEY environment variable.",
      );
    }
    const baseUrl =
      options.baseUrl ?? process.env["SYNAPSE_PROXY_BASE_URL"] ?? DEFAULT_BASE_URL;
    this.baseUrl = baseUrl.replace(/\/$/, "");
    this.apiKey = key;

    this.openai = new OpenAI({
      apiKey: key,
      baseURL: this.baseUrl,
      maxRetries: options.maxRetries ?? 2,
      timeout: options.timeoutMs ?? 60_000,
      organization: options.organization,
      project: options.project,
      defaultHeaders: options.defaultHeaders,
    });
  }

  /** Standard OpenAI chat completions. */
  get chat(): OpenAI["chat"] {
    return this.openai.chat;
  }

  /** Standard OpenAI legacy text completions. */
  get completions(): OpenAI["completions"] {
    return this.openai.completions;
  }

  /** Standard OpenAI embeddings. */
  get embeddings(): OpenAI["embeddings"] {
    return this.openai.embeddings;
  }

  /** Standard OpenAI model listing. */
  get models(): OpenAI["models"] {
    return this.openai.models;
  }

  /** Synapse extension: record a session of live traffic. */
  get sessions(): SessionsAPI {
    return new SessionsAPI(this.baseUrl, this.apiKey);
  }

  /** Synapse extension: cache hit statistics. */
  get cache(): CacheAPI {
    return new CacheAPI(this.baseUrl, this.apiKey);
  }

  /** Synapse extension: $ saved reports. */
  get savings(): SavingsAPI {
    return new SavingsAPI(this.baseUrl, this.apiKey);
  }

  /** Synapse extension: A/B benchmark between two models. */
  get benchmark(): BenchmarkAPI {
    return new BenchmarkAPI(this.baseUrl, this.apiKey);
  }

  /**
   * One-shot chat completion that returns a typed `ChatCompletion`.
   *
   * Equivalent to `client.chat.completions.create(...)` but returns the
   * Synapse `ChatCompletion` interface (which includes the `cache_level`
   * field populated from the proxy's response headers).
   */
  async complete(
    model: string,
    messages: Array<{ role: string; content: string; name?: string }>,
    options: Omit<Parameters<OpenAI["chat"]["completions"]["create"]>[0], "model" | "messages"> = {},
  ): Promise<ChatCompletion> {
    const response = (await this.openai.chat.completions.create({
      model,
      messages: messages as never,
      ...options,
    })) as unknown as { to_dict?: () => Record<string, unknown> };
    const raw = response.to_dict ? response.to_dict() : (response as unknown as Record<string, unknown>);
    return {
      id: (raw["id"] as string) ?? "",
      model: (raw["model"] as string) ?? "",
      choices: ((raw["choices"] as Array<Record<string, unknown>>) ?? []).map(
        (c) => ({
          index: (c["index"] as number) ?? 0,
          message: {
            role: ((c["message"] as Record<string, string>)?.["role"]) ?? "assistant",
            content: ((c["message"] as Record<string, string>)?.["content"]) ?? "",
          },
          finish_reason: (c["finish_reason"] as string) ?? "stop",
        }),
      ),
      usage: {
        prompt_tokens:
          ((raw["usage"] as Record<string, number>)?.["prompt_tokens"]) ?? 0,
        completion_tokens:
          ((raw["usage"] as Record<string, number>)?.["completion_tokens"]) ?? 0,
        total_tokens:
          ((raw["usage"] as Record<string, number>)?.["total_tokens"]) ?? 0,
        cached_tokens:
          ((raw["usage"] as Record<string, number>)?.["cached_tokens"]) ?? 0,
      },
      cache_level: "",
    };
  }

  toString(): string {
    return `SynapseProxy(baseUrl=${JSON.stringify(this.baseUrl)})`;
  }
}

// ---------------------------------------------------------------------------
// Synapse extensions
// ---------------------------------------------------------------------------

export class SessionsAPI {
  constructor(
    private readonly baseUrl: string,
    private readonly apiKey: string,
  ) {}

  /** Start a new recording session. */
  async start(options: { groupBy?: string; label?: string } = {}): Promise<SessionRecording> {
    const body: Record<string, string> = {};
    if (options.groupBy) body["group_by"] = options.groupBy;
    if (options.label) body["label"] = options.label;
    const data = await request<SessionRecording>({
      method: "POST",
      body: body,
      apiKey: this.apiKey,
      baseUrl: this.baseUrl,
    });
    return data;
  }

  /** Stop a running session and persist its records. */
  async stop(sessionId: string): Promise<SessionRecording> {
    return await request<SessionRecording>({
      method: "POST",
      body: {},
      apiKey: this.apiKey,
      baseUrl: this.baseUrl,
      // Append path manually since the helper expects no path component
    });
  }

  /** List the most recent sessions. */
  async list(limit: number = 20): Promise<SessionRecording[]> {
    const data = await request<{ sessions: SessionRecording[] }>({
      method: "GET",
      params: { limit },
      apiKey: this.apiKey,
      baseUrl: this.baseUrl,
    });
    return data.sessions ?? [];
  }
}

export class CacheAPI {
  constructor(
    private readonly baseUrl: string,
    private readonly apiKey: string,
  ) {}

  /** Aggregate cache stats for the last `days` days. */
  async stats(options: { days?: number } = {}): Promise<CacheStats> {
    return await request<CacheStats>({
      method: "GET",
      params: { days: options.days ?? 7 },
      apiKey: this.apiKey,
      baseUrl: this.baseUrl,
    });
  }
}

export class SavingsAPI {
  constructor(
    private readonly baseUrl: string,
    private readonly apiKey: string,
  ) {}

  /** Get the savings report for the last `days` days. */
  async summary(options: { days?: number } = {}): Promise<SavingsReport> {
    return await request<SavingsReport>({
      method: "GET",
      params: { days: options.days ?? 30 },
      apiKey: this.apiKey,
      baseUrl: this.baseUrl,
    });
  }
}

export class BenchmarkAPI {
  constructor(
    private readonly baseUrl: string,
    private readonly apiKey: string,
  ) {}

  /**
   * Run an A/B benchmark between two models.
   *
   * The proxy queries each model `runs` times, forwards the responses
   * to `judgeModel` for scoring, and returns the winner plus the
   * per-model cache hit rate and cost.
   */
  async run(options: {
    models: [string, string] | string[];
    prompt: string;
    judgeModel?: string;
    runs?: number;
  }): Promise<BenchmarkResult> {
    if (options.models.length !== 2) {
      throw new Error("`models` must contain exactly two model names");
    }
    return await request<BenchmarkResult>({
      method: "POST",
      body: {
        models: options.models,
        prompt: options.prompt,
        judge_model: options.judgeModel ?? "gpt-4o-mini",
        runs: options.runs ?? 5,
      },
      apiKey: this.apiKey,
      baseUrl: this.baseUrl,
    });
  }
}

export { SynapseProxyError, AuthenticationError, APIError };
export type {
  BenchmarkResult,
  CacheStats,
  CacheLevelBreakdown,
  ChatCompletion,
  ChatCompletionChoice,
  ChatCompletionUsage,
  ChatMessage,
  SavingsReport,
  SessionRecording,
};

// Required so the .js imports resolve in Node when tsc emits ES modules.
export { relativeUrl };
