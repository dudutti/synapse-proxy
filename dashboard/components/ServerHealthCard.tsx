"use client";

import React, { useEffect, useState, useMemo } from "react";
import { motion, AnimatePresence } from "framer-motion";

// Animated gauge ”” counter smoothly tweens to its target value with a
// glow that intensifies as the value rises. Designed for HUD-style
// dashboards where instant eye-tracking matters more than precision.
function Gauge({
  label,
  value,
  max = 100,
  unit = "",
  formatter = (v: number) => v.toFixed(0),
  color = "emerald",
  warnAt,
  critAt,
  icon,
}: {
  label: string;
  value: number;
  max?: number;
  unit?: string;
  formatter?: (v: number) => string;
  color?: "emerald" | "blue" | "purple" | "amber" | "cyan" | "rose";
  warnAt?: number;
  critAt?: number;
  icon?: React.ReactNode;
}) {
  const pct = Math.min(100, Math.max(0, (value / max) * 100));
  const isWarn = warnAt !== undefined && value >= warnAt;
  const isCrit = critAt !== undefined && value >= critAt;

  const PALETTES: Record<string, { fg: string; glow: string }> = {
    emerald: { fg: "#34d399", glow: "rgba(52,211,153,0.35)" },
    blue:    { fg: "#60a5fa", glow: "rgba(96,165,250,0.35)" },
    purple:  { fg: "#a78bfa", glow: "rgba(167,139,250,0.35)" },
    amber:   { fg: "#fbbf24", glow: "rgba(251,191,36,0.35)" },
    cyan:    { fg: "#22d3ee", glow: "rgba(34,211,238,0.35)" },
    rose:    { fg: "#fb7185", glow: "rgba(251,113,133,0.35)" },
  };
  const active = isCrit ? "rose" : isWarn ? "amber" : color;
  const activePalette = PALETTES[active] || PALETTES.emerald;

  // Animate the counter smoothly toward target
  const [displayed, setDisplayed] = useState(value);
  useEffect(() => {
    const start = displayed;
    const delta = value - start;
    if (Math.abs(delta) < 0.01) return;
    const duration = Math.min(800, 200 + Math.abs(delta) * 5);
    const startTime = performance.now();
    let raf = 0;
    const tick = (now: number) => {
      const t = Math.min(1, (now - startTime) / duration);
      const eased = 1 - Math.pow(1 - t, 3); // easeOutCubic
      setDisplayed(start + delta * eased);
      if (t < 1) raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [value]);

  return (
    <div className="relative p-5 rounded-2xl bg-black/40 border border-white/10 overflow-hidden group hover:border-white/20 transition-colors">
      {/* Glow background that intensifies with value */}
      <div
        className="absolute inset-0 opacity-20 pointer-events-none transition-opacity duration-500"
        style={{
          background: `radial-gradient(circle at 50% 100%, ${activePalette.glow} 0%, transparent 70%)`,
        }}
      />

      <div className="relative z-10">
        <div className="flex items-center justify-between mb-1">
          <div className="flex items-center gap-2">
            {icon && <span className="text-zinc-400">{icon}</span>}
            <span className="text-[10px] font-bold text-zinc-500 uppercase tracking-[0.18em]">
              {label}
            </span>
          </div>
          {isCrit && (
            <span className="text-[9px] font-black px-1.5 py-0.5 rounded bg-rose-500/20 text-rose-300 uppercase tracking-wider animate-pulse">
              CRIT
            </span>
          )}
          {isWarn && !isCrit && (
            <span className="text-[9px] font-black px-1.5 py-0.5 rounded bg-amber-500/20 text-amber-300 uppercase tracking-wider">
              WARN
            </span>
          )}
        </div>

        <div className="flex items-baseline gap-1 mt-2">
          <div
            className="text-3xl font-black tabular-nums"
            style={{
              color: activePalette.fg,
              textShadow: `0 0 18px ${activePalette.glow}`,
            }}
          >
            {formatter(displayed)}
          </div>
          {unit && (
            <span className="text-xs text-zinc-500 font-bold uppercase">{unit}</span>
          )}
        </div>

        {/* Progress bar */}
        <div className="mt-3 h-1.5 w-full bg-white/5 rounded-full overflow-hidden">
          <motion.div
            className="h-full rounded-full"
            style={{
              background: `linear-gradient(90deg, ${activePalette.fg} 0%, ${activePalette.fg} 100%)`,
              boxShadow: `0 0 10px ${activePalette.glow}`,
            }}
            animate={{ width: `${pct}%` }}
            transition={{ duration: 0.5, ease: "easeOut" }}
          />
        </div>
      </div>
    </div>
  );
}

// Compact bar gauge ”” like Gauge but inline, for dense lists.
function BarGauge({
  label,
  value,
  max = 100,
  color = "emerald",
}: {
  label: string;
  value: number;
  max?: number;
  color?: "emerald" | "blue" | "purple" | "amber" | "cyan" | "rose";
}) {
  const pct = Math.min(100, Math.max(0, (value / max) * 100));
  const palette = {
    emerald: "#34d399",
    blue: "#60a5fa",
    purple: "#a78bfa",
    amber: "#fbbf24",
    cyan: "#22d3ee",
    rose: "#fb7185",
  }[color];
  return (
    <div className="space-y-1">
      <div className="flex justify-between text-[10px] font-bold">
        <span className="text-zinc-500 uppercase tracking-wider">{label}</span>
        <span className="tabular-nums text-white">{value.toLocaleString()}</span>
      </div>
      <div className="h-1 w-full bg-white/5 rounded-full overflow-hidden">
        <motion.div
          className="h-full rounded-full"
          style={{ background: palette, boxShadow: `0 0 8px ${palette}66` }}
          animate={{ width: `${pct}%` }}
          transition={{ duration: 0.5, ease: "easeOut" }}
        />
      </div>
    </div>
  );
}

// Sparkline ”” same shape as the Playground sparkline but designed for
// 30 datapoints of system history.
function Sparkline({
  values,
  color = "#34d399",
  height = 32,
  width = 120,
  fillOpacity = 0.18,
}: {
  values: number[];
  color?: string;
  height?: number;
  width?: number;
  fillOpacity?: number;
}) {
  const { line, fill } = useMemo(() => {
    if (!values || values.length === 0) return { line: "", fill: "" };
    const max = Math.max(...values, 1);
    const min = Math.min(...values, 0);
    const range = max - min || 1;
    const step = values.length > 1 ? width / (values.length - 1) : width;
    const pts = values.map((v, i) => [i * step, height - ((v - min) / range) * height] as const);
    const linePath = pts.map(([x, y], i) => (i === 0 ? `M ${x},${y}` : `L ${x},${y}`)).join(" ");
    const fillPath = `${linePath} L ${width},${height} L 0,${height} Z`;
    return { line: linePath, fill: fillPath };
  }, [values, height, width]);

  if (!line) {
    return (
      <div className="inline-block bg-white/5 rounded" style={{ width, height }} />
    );
  }
  return (
    <svg width={width} height={height} className="inline-block">
      <path d={fill} fill={color} fillOpacity={fillOpacity} />
      <path d={line} stroke={color} strokeWidth={1.5} fill="none" />
    </svg>
  );
}

// =====================================================================
// Top-level ServerHealthCard ”” aggregates Prometheus + DB stats into
// a single dense HUD panel.
// =====================================================================

export type StatusData = {
  ok: boolean;
  fetched_at: string;
  proxy: {
    url: string;
    reachable: boolean;
    error?: string;
    metrics: Record<
      string,
      { type: string; help: string; samples: Record<string, number> }
    >;
  };
  database: {
    totalUsers: number;
    activeApiKeys: number;
    zeroLogKeys: number;
    benchmarkKeys: number;
    totalRequestLogs: number;
    logsLastHour: number;
    logsLast24h: number;
    totalCostSaved: number;
    totalTokensSaved: number;
    totalPricingModels: number;
    modelsWithoutPricing: number;
    distinctAgents: number;
    distinctProviders: number;
  };
};

const REFRESH_MS = 5000;

export function ServerHealthCard() {
  const [data, setData] = useState<StatusData | null>(null);
  const [history, setHistory] = useState<{
    costSaved: number[];
    tokensSaved: number[];
    requestsPerMin: number[];
    upstreamErrors: number[];
  }>({ costSaved: [], tokensSaved: [], requestsPerMin: [], upstreamErrors: [] });
  const [error, setError] = useState<string | null>(null);
  const [lastFetchDuration, setLastFetchDuration] = useState<number | null>(null);

  useEffect(() => {
    let cancelled = false;
    const fetchOnce = async () => {
      const t0 = performance.now();
      try {
        const res = await fetch("/api/admin/status", { cache: "no-store" });
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        const json: StatusData = await res.json();
        if (cancelled) return;
        setData(json);
        setError(null);
        setLastFetchDuration(performance.now() - t0);

        setHistory((h) => ({
          costSaved: [...h.costSaved, json.database.totalCostSaved].slice(-30),
          tokensSaved: [...h.tokensSaved, json.database.totalTokensSaved].slice(-30),
          requestsPerMin: [...h.requestsPerMin, json.database.logsLastHour].slice(-30),
          upstreamErrors: [
            ...h.upstreamErrors,
            json.proxy.metrics?.["synapse_proxy_upstream_errors_total"]?.samples?._total ?? 0,
          ].slice(-30),
        }));
      } catch (e: any) {
        if (!cancelled) setError(e?.message || String(e));
      }
    };
    fetchOnce();
    const id = setInterval(fetchOnce, REFRESH_MS);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, []);

  if (error && !data) {
    return (
      <div className="p-6 rounded-2xl bg-rose-500/10 border border-rose-500/30 text-rose-300">
        <div className="text-xs font-black uppercase tracking-widest mb-1">SUPERADMIN :: STATUS</div>
        <div className="text-sm">Failed to reach /api/admin/status: {error}</div>
      </div>
    );
  }

  if (!data) {
    return (
      <div className="h-[400px] flex items-center justify-center text-zinc-500 font-mono animate-pulse">
        <Activity className="w-4 h-4 inline mr-2" /> Establishing uplink...
      </div>
    );
  }

  const m = data.proxy.metrics || {};
  const db = data.database;
  const cacheHits =
    (m["synapse_proxy_cache_hits_total"]?.samples || {}) as Record<string, number>;
  const totalCacheHits = Object.values(cacheHits).reduce((a, b) => a + (b || 0), 0);
  const totalRequests =
    (m["synapse_proxy_upstream_requests_total"]?.samples?._total ?? 0) + totalCacheHits;
  const cacheHitRate =
    totalRequests > 0 ? (totalCacheHits / totalRequests) * 100 : 0;
  const totalCostSavedDollars =
    (m["synapse_proxy_cost_saved_total"]?.samples || {});
  const totalCostSavedMillicents = Object.values(totalCostSavedDollars).reduce(
    (a, b) => a + (b || 0),
    0
  );
  const totalCostSavedDollarsLive = totalCostSavedMillicents / 1000;
  const panics = m["synapse_proxy_panics_total"]?.samples || {};
  const totalPanics = Object.values(panics).reduce((a, b) => a + (b || 0), 0);

  return (
    <div className="space-y-6">
      {/* Top header */}
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-cyan-400 text-xs font-black tracking-[0.25em] uppercase drop-shadow-[0_0_8px_rgba(34,211,238,0.5)]">
            SUPERADMIN :: Live Telemetry
          </h2>
          <p className="text-zinc-500 text-xs mt-1">
            Streaming every {REFRESH_MS / 1000}s · proxy + DB aggregates ·{" "}
            <span className="text-zinc-400">
              {lastFetchDuration !== null ? `${lastFetchDuration.toFixed(0)}ms` : "..."}
            </span>
          </p>
        </div>
        <div className="flex items-center gap-2">
          <ProxyStatusBadge reachable={data.proxy.reachable} error={data.proxy.error} />
        </div>
      </div>

      {/* Cache hit gauges */}
      <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-6 gap-3">
        <Gauge
          label="Hit Rate"
          value={cacheHitRate}
          max={100}
          unit="%"
          color="emerald"
          warnAt={50}
          critAt={20}
          icon={<GaugeIcon />}
        />
        <Gauge
          label="L1"
          value={cacheHits["L1"] || 0}
          max={Math.max(1, totalCacheHits)}
          color="blue"
          icon={<HashIcon />}
        />
        <Gauge
          label="L2"
          value={cacheHits["L2"] || 0}
          max={Math.max(1, totalCacheHits)}
          color="emerald"
          icon={<HashIcon />}
        />
        <Gauge
          label="L3"
          value={cacheHits["L3"] || 0}
          max={Math.max(1, totalCacheHits)}
          color="purple"
          icon={<HashIcon />}
        />
        <Gauge
          label="Upstream"
          value={m["synapse_proxy_upstream_requests_total"]?.samples?._total ?? 0}
          max={Math.max(1, totalRequests)}
          color="cyan"
          icon={<ArrowUpIcon />}
        />
        <Gauge
          label="Errors"
          value={m["synapse_proxy_upstream_errors_total"]?.samples?._total ?? 0}
          max={Math.max(1, totalRequests)}
          color="rose"
          warnAt={Math.max(1, totalRequests * 0.05)}
          critAt={Math.max(1, totalRequests * 0.15)}
          icon={<AlertIcon />}
        />
      </div>

      {/* Secondary gauges */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
        <Gauge
          label="$ Saved (proxy)"
          value={totalCostSavedDollarsLive}
          max={Math.max(100, totalCostSavedDollarsLive * 1.2)}
          unit="$"
          formatter={(v) => `$${v.toFixed(4)}`}
          color="amber"
          icon={<DollarIcon />}
        />
        <Gauge
          label="$ Saved (DB)"
          value={db.totalCostSaved}
          max={Math.max(100, db.totalCostSaved * 1.2)}
          unit="$"
          formatter={(v) => `$${v.toFixed(0)}`}
          color="amber"
          icon={<DollarIcon />}
        />
        <Gauge
          label="Users"
          value={db.totalUsers}
          max={Math.max(100, db.totalUsers * 1.2)}
          color="blue"
          icon={<UserIcon />}
        />
        <Gauge
          label="Panics"
          value={totalPanics}
          max={Math.max(5, totalPanics * 1.5)}
          color={totalPanics > 0 ? "rose" : "emerald"}
          warnAt={1}
          critAt={5}
          icon={<AlertIcon />}
        />
      </div>

      {/* Sparkline history */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
        <SparklinePanel
          label="$ saved (DB) · last 30 samples"
          values={history.costSaved}
          color="#fbbf24"
          formatter={(v) => `$${v.toFixed(0)}`}
          current={db.totalCostSaved}
        />
        <SparklinePanel
          label="Tokens saved · last 30 samples"
          values={history.tokensSaved}
          color="#a78bfa"
          formatter={(v) => v.toLocaleString()}
          current={db.totalTokensSaved}
        />
        <SparklinePanel
          label="Requests / hour · last 30 samples"
          values={history.requestsPerMin}
          color="#22d3ee"
          current={db.logsLastHour}
        />
        <SparklinePanel
          label="Upstream errors · last 30 samples"
          values={history.upstreamErrors}
          color="#fb7185"
          current={m["synapse_proxy_upstream_errors_total"]?.samples?._total ?? 0}
        />
      </div>

      {/* Forecast ”” linear regression over the sparkline history,
          projected 30 days forward. Shows trend + 95% confidence
          interval. Green when savings rise (or errors fall); red
          otherwise. Hidden when we have fewer than 3 data points. */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
        <ForecastCard
          title="$ Saved (DB cumulative)"
          values={history.costSaved}
          format={(v) => `$${v.toFixed(0)}`}
          unit="$"
          color="#fbbf24"
        />
        <ForecastCard
          title="Tokens saved"
          values={history.tokensSaved}
          format={(v) => v.toLocaleString()}
          color="#a78bfa"
        />
        <ForecastCard
          title="Requests / hour"
          values={history.requestsPerMin}
          format={(v) => v.toLocaleString()}
          color="#22d3ee"
        />
      </div>

      {/* Detailed breakdown */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        {/* Database panel */}
        <div className="p-5 rounded-2xl bg-black/40 border border-white/10">
          <h3 className="text-xs font-black uppercase tracking-widest text-zinc-400 mb-4">
            <DatabaseIcon /> Database aggregates
          </h3>
          <div className="space-y-3">
            <BarGauge label="Total RequestLog rows" value={db.totalRequestLogs} color="cyan" max={Math.max(100, db.totalRequestLogs)} />
            <BarGauge label="Logs last hour" value={db.logsLastHour} color="emerald" max={Math.max(50, db.logsLastHour)} />
            <BarGauge label="Logs last 24h" value={db.logsLast24h} color="blue" max={Math.max(500, db.logsLast24h)} />
            <BarGauge label="Active API keys" value={db.activeApiKeys} color="blue" max={Math.max(50, db.activeApiKeys)} />
            <BarGauge label="Zero-log keys" value={db.zeroLogKeys} color="purple" max={Math.max(20, db.zeroLogKeys || 1)} />
            <BarGauge label="Benchmark keys" value={db.benchmarkKeys} color="amber" max={Math.max(20, db.benchmarkKeys || 1)} />
            <BarGauge label="Distinct agents" value={db.distinctAgents} color="purple" max={Math.max(20, db.distinctAgents)} />
            <BarGauge label="Distinct providers" value={db.distinctProviders} color="blue" max={Math.max(20, db.distinctProviders)} />
          </div>
        </div>

        {/* Pricing coverage */}
        <div className="p-5 rounded-2xl bg-black/40 border border-white/10">
          <h3 className="text-xs font-black uppercase tracking-widest text-zinc-400 mb-4">
            <DollarIcon /> Pricing coverage
          </h3>
          <div className="space-y-3">
            <BarGauge
              label="ProviderModel entries"
              value={db.totalPricingModels}
              color="emerald"
              max={Math.max(50, db.totalPricingModels)}
            />
            <BarGauge
              label="Models in use but NOT priced"
              value={db.modelsWithoutPricing}
              color="rose"
              max={Math.max(5, db.modelsWithoutPricing || 1)}
            />
            <div className="pt-3 text-xs text-zinc-500 leading-relaxed">
              When a <code className="text-rose-300">modelsWithoutPricing</code>{" "}
              hits the proxy, <code className="text-rose-300">pricing.go</code>{" "}
              falls back to <strong className="text-zinc-300">$1/MTok</strong> and
              logs a one-time warning. Seed them in <code>ProviderModel</code>{" "}
              or add aliases to <code>pricing.go</code>.
            </div>
          </div>
        </div>

        {/* Upstream latency histogram */}
        <div className="p-5 rounded-2xl bg-black/40 border border-white/10">
          <h3 className="text-xs font-black uppercase tracking-widest text-zinc-400 mb-4">
            <ClockIcon /> Upstream latency distribution
          </h3>
          <div className="space-y-2">
            {(["le_10ms", "le_100ms", "le_500ms", "le_2s", "ge_2s"] as const).map(
              (label) => {
                const v =
                  m["synapse_proxy_upstream_latency_seconds_bucket"]?.samples?.[
                    `"${label}"`
                  ] ?? 0;
                return (
                  <BarGauge
                    key={label}
                    label={label.replace("le_", "< ").replace("ge_", "â‰¥ ")}
                    value={v}
                    color={
                      label === "le_10ms" || label === "le_100ms"
                        ? "emerald"
                        : label === "le_500ms"
                        ? "blue"
                        : label === "le_2s"
                        ? "amber"
                        : "rose"
                    }
                    max={Math.max(50, (m["synapse_proxy_upstream_requests_total"]?.samples?._total ?? 1))}
                  />
                );
              }
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

// ===== Sub-components =====

function SparklinePanel({
  label,
  values,
  color,
  formatter,
  current,
}: {
  label: string;
  values: number[];
  color: string;
  formatter?: (v: number) => string;
  current: number;
}) {
  return (
    <div className="p-4 rounded-2xl bg-black/40 border border-white/10">
      <div className="text-[10px] font-bold uppercase tracking-widest text-zinc-500 mb-2">
        {label}
      </div>
      <div className="flex items-end justify-between gap-3">
        <Sparkline values={values} color={color} />
        <div className="text-right">
          <div className="text-2xl font-black tabular-nums" style={{ color }}>
            {formatter ? formatter(current) : current.toLocaleString()}
          </div>
          <div className="text-[9px] text-zinc-600 uppercase tracking-wider">
            now
          </div>
        </div>
      </div>
    </div>
  );
}

// =====================================================================
// Cost forecast ”” linear regression over the sparkline history,
// projected 30 days forward. Confidence band uses the residual
// standard deviation scaled by sqrt(forecast horizon).
//
// This is intentionally simple (least-squares over the last 30
// samples) ”” anything fancier would imply false precision in the
// dashboard. The point is to show trend + magnitude, not exact $.
// =====================================================================

function linearProjection(values: number[], horizonSamples: number) {
  const n = values.length;
  if (n < 3) return null;

  const xs = values.map((_, i) => i);
  const sumX = xs.reduce((a, b) => a + b, 0);
  const sumY = values.reduce((a, b) => a + b, 0);
  const sumXY = values.reduce((acc, v, i) => acc + i * v, 0);
  const sumXX = xs.reduce((acc, x) => acc + x * x, 0);
  const meanX = sumX / n;
  const meanY = sumY / n;

  const denom = sumXX - n * meanX * meanX;
  if (Math.abs(denom) < 1e-9) return null;

  const slope = (sumXY - n * meanX * meanY) / denom;
  const intercept = meanY - slope * meanX;

  // Residual standard deviation ”” used to widen the band with horizon
  let ssRes = 0;
  for (let i = 0; i < n; i++) {
    const pred = intercept + slope * i;
    ssRes += (values[i] - pred) ** 2;
  }
  const sigma = Math.sqrt(ssRes / Math.max(1, n - 2));

  const forecast = intercept + slope * (n - 1 + horizonSamples);
  const bandLow = forecast - sigma * Math.sqrt(horizonSamples) * 1.96;
  const bandHigh = forecast + sigma * Math.sqrt(horizonSamples) * 1.96;

  return { slope, current: values[n - 1], forecast, bandLow, bandHigh, sigma };
}

function ForecastCard({
  title,
  values,
  format,
  unit = "",
  periodSec = 5,
  horizonDays = 30,
  color = "#34d399",
}: {
  title: string;
  values: number[];
  format: (v: number) => string;
  unit?: string;
  periodSec?: number;
  horizonDays?: number;
  color?: string;
}) {
  const horizonSamples = Math.round((horizonDays * 24 * 3600) / periodSec);
  const proj = linearProjection(values, horizonSamples);

  if (!proj) {
    return (
      <div className="p-4 rounded-2xl bg-black/40 border border-white/10">
        <div className="text-[10px] font-bold uppercase tracking-widest text-zinc-500 mb-2">
          {title} · 30-day forecast
        </div>
        <div className="text-[11px] text-zinc-600">Not enough data yet (need â‰¥3 samples)</div>
      </div>
    );
  }

  const trendUp = proj.slope > 0;
  const isSavings = title.toLowerCase().includes("saved") || title.toLowerCase().includes("saving");
  // For savings/cost-saved: rising = green; for errors/requests: rising = amber
  const goodTrend = isSavings ? trendUp : !trendUp;

  const cardCls = goodTrend
    ? "bg-emerald-500/[0.04] border-emerald-500/30"
    : "bg-rose-500/[0.04] border-rose-500/30";
  const fg = goodTrend ? "#34d399" : "#fb7185";

  return (
    <div className={`p-4 rounded-2xl border ${cardCls}`}>
      <div className="text-[10px] font-bold uppercase tracking-widest text-zinc-500 mb-2">
        {title} · 30-day forecast
      </div>
      <div className="flex items-end justify-between gap-3">
        <div>
          <div className="text-2xl font-black tabular-nums" style={{ color: fg }}>
            {format(proj.forecast)}
            {unit && <span className="text-xs text-zinc-500 ml-1 font-bold">{unit}</span>}
          </div>
          <div className="text-[10px] text-zinc-500 mt-1">
            {trendUp ? "↑" : "↓"} {format(Math.abs(proj.slope * horizonSamples))}{unit} trend
          </div>
        </div>
        <div className="text-right">
          <div className="text-[9px] uppercase tracking-wider text-zinc-600">95% range</div>
          <div className="text-[11px] font-mono text-zinc-400">
            {format(proj.bandLow)} ”“ {format(proj.bandHigh)}{unit}
          </div>
        </div>
      </div>
    </div>
  );
}

function ProxyStatusBadge({ reachable, error }: { reachable: boolean; error?: string }) {
  return (
    <AnimatePresence mode="wait">
      {reachable ? (
        <motion.div
          key="ok"
          initial={{ opacity: 0, scale: 0.9 }}
          animate={{ opacity: 1, scale: 1 }}
          exit={{ opacity: 0, scale: 0.9 }}
          className="flex items-center gap-2 px-3 py-1.5 rounded-full bg-emerald-500/15 border border-emerald-500/30"
        >
          <motion.span
            className="w-2 h-2 rounded-full bg-emerald-400"
            animate={{ opacity: [1, 0.4, 1] }}
            transition={{ duration: 1.5, repeat: Infinity }}
          />
          <span className="text-xs font-bold text-emerald-300 uppercase tracking-wider">
            Proxy reachable
          </span>
        </motion.div>
      ) : (
        <motion.div
          key="err"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          className="flex items-center gap-2 px-3 py-1.5 rounded-full bg-rose-500/15 border border-rose-500/30"
          title={error}
        >
          <span className="w-2 h-2 rounded-full bg-rose-400" />
          <span className="text-xs font-bold text-rose-300 uppercase tracking-wider">
            Proxy down
          </span>
        </motion.div>
      )}
    </AnimatePresence>
  );
}

// ===== Tiny inline SVG icons =====
// Kept inline to avoid adding another icon dependency for a HUD that
// already has Globe + framer-motion + particle backgrounds.
function GaugeIcon() {
  return (
    <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <path d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
      <path d="M12 3v3M12 18v3M3 12h3M18 12h3M5.6 5.6l2.1 2.1M16.3 16.3l2.1 2.1M5.6 18.4l2.1-2.1M16.3 7.7l2.1-2.1" />
    </svg>
  );
}
function HashIcon() {
  return <span className="text-xs font-black">#</span>;
}
function ArrowUpIcon() {
  return (
    <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <path d="M5 12l7-7 7 7M12 19V5" />
    </svg>
  );
}
function AlertIcon() {
  return (
    <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <path d="M12 9v2m0 4h.01M10.3 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z" />
    </svg>
  );
}
function DollarIcon() {
  return <span className="text-xs font-black">$</span>;
}
function UserIcon() {
  return (
    <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <path d="M20 21v-2a4 4 0 00-4-4H8a4 4 0 00-4 4v2M12 11a4 4 0 100-8 4 4 0 000 8z" />
    </svg>
  );
}
function DatabaseIcon() {
  return (
    <svg className="w-3 h-3 inline mr-1" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <ellipse cx="12" cy="5" rx="9" ry="3" />
      <path d="M21 12c0 1.66-4 3-9 3s-9-1.34-9-3M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5" />
    </svg>
  );
}
function ClockIcon() {
  return (
    <svg className="w-3 h-3 inline mr-1" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <circle cx="12" cy="12" r="10" />
      <path d="M12 6v6l4 2" />
    </svg>
  );
}
function Activity(props: React.SVGProps<SVGSVGElement>) {
  return (
    <svg {...props} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <path d="M22 12h-4l-3 9L9 3l-3 9H2" />
    </svg>
  );
}
