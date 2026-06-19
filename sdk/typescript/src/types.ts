/**
 * Type definitions for the Synapse Proxy SDK.
 *
 * Mirror the dashboard's API response shapes so users can iterate on
 * typed objects instead of plain dictionaries.
 */

export interface ChatMessage {
  role: string;
  content: string;
  name?: string;
}

export interface ChatCompletionUsage {
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
  /** Synapse extension: tokens served from the provider's cache. */
  cached_tokens: number;
}

export interface ChatCompletionChoice {
  index: number;
  message: ChatMessage;
  finish_reason: string;
}

export interface ChatCompletion {
  id: string;
  model: string;
  choices: ChatCompletionChoice[];
  usage: ChatCompletionUsage;
  /** Synapse extension: which cache level served this request. */
  cache_level: string;
}

export interface SessionRecording {
  id: string;
  group_by: string;
  started_at: string;
  stopped_at: string | null;
  record_count: number;
  tokens_saved: number;
  estimated_cost_saved: number;
}

export interface CacheLevelBreakdown {
  cache_level: string;
  hits: number;
  total: number;
}

export interface CacheStats {
  total_requests: number;
  total_tokens_saved: number;
  total_cost_saved: number;
  by_level: CacheLevelBreakdown[];
  window_days: number;
}

export interface SavingsReport {
  total_cost_saved: number;
  total_tokens_saved: number;
  total_real_cost: number;
  total_optimized_cost: number;
  by_class: Record<string, number>;
  by_provider: Record<string, number>;
  window_days: number;
}

export interface BenchmarkResult {
  winner: string;
  model_a: string;
  model_b: string;
  score_a: number;
  score_b: number;
  judge_model: string;
  judge_reason: string;
  cache_hit_rate_a: number;
  cache_hit_rate_b: number;
  cost_a: number;
  cost_b: number;
}
