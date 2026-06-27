// Shared cache labels and colors. P1.6 INTERSECTION.
//
// Before P1.6, the dashboard had three places that each
// labeled cache levels differently:
//   - app/page.tsx: "L1 Cache (exact)", "L2 Cache (semantic)",
//     "L3 Standard (compressed)", "Standard Routing (no opt)"
//   - app/api/analytics/route.ts: same as page.tsx
//   - app/api/analytics/stream/route.ts:
//     "Standard Routing", "Cache Hit (L1)"
//
// Same concept (cache hit/miss), 3 different label sets.
// The user couldn't tell at a glance whether "Cache Hit
// (L1)" was the same thing as "L1 Cache (exact)".
//
// This module is the single source of truth.

export type CacheLevel = "NONE" | "L0" | "L1" | "L2" | "L3" | "LOOP";

export type CacheLabel = {
  label: string;
  color: string; // tailwind hex
  shortLabel: string;
  description: string;
};

// Canonical labels. Every UI component MUST import from here.
export const CACHE_LABELS: Record<CacheLevel, CacheLabel> = {
  NONE: {
    label: "Standard Routing",
    color: "#334155",
    shortLabel: "MISS",
    description: "No cache hit; upstream called",
  },
  L0: {
    label: "L0 Coalesced",
    color: "#0ea5e9",
    shortLabel: "L0",
    description: "In-flight deduplication (same payload, parallel requests)",
  },
  L1: {
    label: "L1 Cache (exact)",
    color: "#3b82f6",
    shortLabel: "L1",
    description: "Exact-match cache hit (SHA-256)",
  },
  L2: {
    label: "L2 Cache (semantic)",
    color: "#10b981",
    shortLabel: "L2",
    description: "Semantic-similarity cache hit (vector search)",
  },
  L3: {
    label: "L3 Compressed",
    color: "#a855f7",
    shortLabel: "L3",
    description: "Compressed payload (LogCompressor / CCR)",
  },
  LOOP: {
    label: "Loop Detected",
    color: "#ef4444",
    shortLabel: "LOOP",
    description: "Kill switch fired; agent was in a loop",
  },
};

// Look up a label by raw cacheLevel string (defensive:
// dashboards sometimes receive unknown levels).
export function getCacheLabel(level: string): CacheLabel {
  return CACHE_LABELS[level as CacheLevel] || {
    label: level || "Unknown",
    color: "#6b7280",
    shortLabel: level || "?",
    description: "Unknown cache level",
  };
}