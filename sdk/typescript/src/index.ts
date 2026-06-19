/**
 * @synapse-proxy/sdk
 *
 * Drop-in OpenAI-compatible client for Synapse Proxy with session
 * recording, cache statistics, and A/B benchmark extensions.
 *
 * Standard OpenAI usage:
 * ```ts
 * import { SynapseProxy } from "@synapse-proxy/sdk";
 * const sp = new SynapseProxy({ apiKey: process.env.SYNAPSE_API_KEY });
 * const chat = await sp.chat.completions.create({
 *   model: "gpt-4o-mini",
 *   messages: [{ role: "user", content: "Hello, world!" }],
 * });
 * ```
 *
 * Synapse-specific extensions:
 * ```ts
 * const session = await sp.sessions.start({ groupBy: "agent" });
 * const stats   = await sp.cache.stats({ days: 7 });
 * const savings = await sp.savings.summary({ days: 30 });
 * const ab      = await sp.benchmark.run({
 *   models: ["gpt-4o-mini", "minimax-m3"],
 *   prompt: "Explain TCP slow start in 3 sentences.",
 * });
 * ```
 */

export {
  SynapseProxy,
  SessionsAPI,
  CacheAPI,
  SavingsAPI,
  BenchmarkAPI,
  SynapseProxyError,
  AuthenticationError,
  APIError,
  relativeUrl,
} from "./client.js";

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
} from "./types.js";

export type { SynapseProxyOptions } from "./client.js";
