"use client";

import React, { useEffect, useState } from "react";
import { motion } from "framer-motion";

interface Totals {
  users: number;
  apiKeys: number;
  requestLogs: number;
  tokensSaved: number;
  costSaved: number;
  distinctAgents: number;
}

const formatNumber = (n: number) => new Intl.NumberFormat("en-US").format(n);
const formatCurrency = (n: number) =>
  new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "USD",
    minimumFractionDigits: 0,
    maximumFractionDigits: 0,
  }).format(n);

// Tiny animated counter
function Counter({ value, prefix = "", suffix = "" }: { value: number; prefix?: string; suffix?: string }) {
  const [displayed, setDisplayed] = useState(value);
  useEffect(() => {
    const start = displayed;
    const delta = value - start;
    if (Math.abs(delta) < 1) return;
    const duration = 800;
    const startTime = performance.now();
    let raf = 0;
    const tick = (now: number) => {
      const t = Math.min(1, (now - startTime) / duration);
      const eased = 1 - Math.pow(1 - t, 3);
      setDisplayed(start + delta * eased);
      if (t < 1) raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [value]);
  return (
    <span className="tabular-nums">
      {prefix}
      {formatNumber(Math.round(displayed))}
      {suffix}
    </span>
  );
}

export function PublicStatusCard({ totals }: { totals: Totals }) {
  return (
    <div className="grid grid-cols-2 md:grid-cols-3 gap-3">
      <StatCard
        label="Users"
        value={totals.users}
        color="emerald"
        unit=""
        glow="rgba(52,211,153,0.18)"
      />
      <StatCard
        label="API Keys"
        value={totals.apiKeys}
        color="blue"
        unit=""
        glow="rgba(96,165,250,0.18)"
      />
      <StatCard
        label="Agents Detected"
        value={totals.distinctAgents}
        color="purple"
        unit=""
        glow="rgba(167,139,250,0.18)"
      />
      <StatCard
        label="Requests Served"
        value={totals.requestLogs}
        color="cyan"
        unit=""
        glow="rgba(34,211,238,0.18)"
      />
      <StatCard
        label="Tokens Purged"
        value={totals.tokensSaved}
        color="purple"
        unit=""
        glow="rgba(167,139,250,0.18)"
      />
      <StatCard
        label="$ Saved Globally"
        value={totals.costSaved}
        color="amber"
        unit=""
        glow="rgba(251,191,36,0.18)"
        formatter={formatCurrency}
      />
    </div>
  );
}

const COLORS: Record<string, { fg: string; border: string }> = {
  emerald: { fg: "#34d399", border: "rgba(52,211,153,0.30)" },
  blue: { fg: "#60a5fa", border: "rgba(96,165,250,0.30)" },
  purple: { fg: "#a78bfa", border: "rgba(167,139,250,0.30)" },
  cyan: { fg: "#22d3ee", border: "rgba(34,211,238,0.30)" },
  amber: { fg: "#fbbf24", border: "rgba(251,191,36,0.30)" },
};

function StatCard({
  label,
  value,
  color,
  unit,
  glow,
  formatter = formatNumber,
}: {
  label: string;
  value: number;
  color: "emerald" | "blue" | "purple" | "cyan" | "amber";
  unit: string;
  glow: string;
  formatter?: (n: number) => string;
}) {
  const c = COLORS[color];
  return (
    <motion.div
      initial={{ opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.3 }}
      className="relative p-5 rounded-2xl bg-black/40 border overflow-hidden group hover:scale-[1.02] transition-transform"
      style={{ borderColor: c.border }}
    >
      <div
        className="absolute inset-0 opacity-100 pointer-events-none transition-opacity"
        style={{
          background: `radial-gradient(circle at 50% 100%, ${glow} 0%, transparent 70%)`,
        }}
      />
      <div className="relative z-10">
        <div className="text-[10px] font-black uppercase tracking-[0.2em] text-zinc-500 mb-2">
          {label}
        </div>
        <div
          className="text-4xl font-black tabular-nums"
          style={{
            color: c.fg,
            textShadow: `0 0 20px ${glow}`,
          }}
        >
          {formatter(value)}
        </div>
      </div>
    </motion.div>
  );
}
