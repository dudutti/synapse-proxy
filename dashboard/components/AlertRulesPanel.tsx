"use client";

import React, { useEffect, useState, useCallback } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { Bell, Plus, Trash2, Edit3, X, Check, AlertTriangle, AlertCircle, Info } from "lucide-react";

// Available metric kinds. Kept in sync with /api/admin/alerts validation.
const METRICS = [
  { id: "panic_rate",            label: "Panic rate",           unit: "panics/min", defaultThreshold: 0,   defaultOp: "gt",  help: "Triggers when the proxy recovers from any panic (currently only ProxyHandler wraps)." },
  { id: "error_rate",            label: "Upstream error rate",  unit: "%",          defaultThreshold: 5,   defaultOp: "gt",  help: "Upstream 4xx/5xx rate over the rolling window. Persisted as part of upstream_requests_total / upstream_errors_total." },
  { id: "cache_hit_rate",        label: "Cache hit rate",       unit: "%",          defaultThreshold: 30,  defaultOp: "lt",  help: "L1+L2+L3+LOOP hit rate. Persisted from proxy /metrics." },
  { id: "upstream_latency_p95",  label: "Upstream latency p95", unit: "ms",         defaultThreshold: 2000, defaultOp: "gt",  help: "Approximation via the ge_2s bucket: alerts when more than X% of upstream calls take >=2s." },
  { id: "pricing_gaps",           label: "Pricing gaps",         unit: "models",     defaultThreshold: 0,   defaultOp: "gt",  help: "(provider, model) combos seen in RequestLog but missing from ProviderModel \u2014 i.e. requests falling back to $1/MTok." },
] as const;

const OPS = [
  { id: "gt",  label: ">" },
  { id: "gte", label: "\u2265" },
  { id: "lt",  label: "<" },
  { id: "lte", label: "\u2264" },
] as const;

const SEVERITIES = [
  { id: "info",     label: "Info",     color: "blue"   },
  { id: "warning",  label: "Warning",  color: "amber"  },
  { id: "critical", label: "Critical", color: "rose"   },
] as const;

type AlertRule = {
  id: string;
  name: string;
  metric: string;
  operator: string;
  threshold: number;
  windowSec: number;
  enabled: boolean;
  severity: string;
  notifyEmail: string | null;
  notifySlack: string | null;
  lastFiredAt: string | null;
  createdAt: string;
};

type AlertEvent = {
  id: string;
  ruleId: string;
  observedValue: number;
  threshold: number;
  message: string;
  acknowledged: boolean;
  acknowledgedAt: string | null;
  acknowledgedBy: string | null;
  firedAt: string;
};

const SEV_COLORS: Record<string, string> = {
  info: "bg-blue-500/15 text-blue-300 border-blue-500/30",
  warning: "bg-amber-500/15 text-amber-300 border-amber-500/30",
  critical: "bg-rose-500/15 text-rose-300 border-rose-500/30",
};

const SEV_ICON: Record<string, React.ReactNode> = {
  info: <Info className="w-3.5 h-3.5" />,
  warning: <AlertTriangle className="w-3.5 h-3.5" />,
  critical: <AlertCircle className="w-3.5 h-3.5" />,
};

function fmtTime(iso: string | null): string {
  if (!iso) return "\u2014";
  const d = new Date(iso);
  const sec = Math.floor((Date.now() - d.getTime()) / 1000);
  if (sec < 60) return `${sec}s ago`;
  if (sec < 3600) return `${Math.floor(sec / 60)}m ago`;
  if (sec < 86400) return `${Math.floor(sec / 3600)}h ago`;
  return `${Math.floor(sec / 86400)}d ago`;
}

export function AlertRulesPanel() {
  const [rules, setRules] = useState<AlertRule[]>([]);
  const [events, setEvents] = useState<AlertEvent[]>([]);
  const [unackedCount, setUnackedCount] = useState(0);
  const [showForm, setShowForm] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const fetchAll = useCallback(async () => {
    try {
      const [r, e] = await Promise.all([
        fetch("/api/admin/alerts", { cache: "no-store" }).then((r) => r.json()),
        fetch("/api/admin/alerts/events?unacked=1", { cache: "no-store" }).then((r) => r.json()),
      ]);
      setRules(r.rules || []);
      setUnackedCount(r.unackedCount || 0);
      setEvents(e.events || []);
    } catch (e: any) {
      setError(e?.message || String(e));
    }
  }, []);

  useEffect(() => {
    fetchAll();
    const id = setInterval(fetchAll, 10000);
    return () => clearInterval(id);
  }, [fetchAll]);

  const createRule = async (data: any) => {
    const res = await fetch("/api/admin/alerts", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(data),
    });
    if (!res.ok) {
      const j = await res.json().catch(() => ({}));
      setError(j.error || `HTTP ${res.status}`);
      return;
    }
    setShowForm(false);
    setEditingId(null);
    setError(null);
    fetchAll();
  };

  const updateRule = async (id: string, data: any) => {
    const res = await fetch(`/api/admin/alerts/${id}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(data),
    });
    if (!res.ok) {
      const j = await res.json().catch(() => ({}));
      setError(j.error || `HTTP ${res.status}`);
      return;
    }
    setEditingId(null);
    setError(null);
    fetchAll();
  };

  const deleteRule = async (id: string) => {
    if (!confirm("Delete this alert rule?")) return;
    await fetch(`/api/admin/alerts/${id}`, { method: "DELETE" });
    fetchAll();
  };

  const ackEvent = async (id: string) => {
    await fetch("/api/admin/alerts/events", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ id }),
    });
    fetchAll();
  };

  return (
    <div className="rounded-2xl bg-black/60 border border-white/10 overflow-hidden">
      <div className="border-b border-white/10 bg-black/40 px-4 py-3 flex items-center justify-between">
        <div className="flex items-center gap-3">
          <Bell className="w-4 h-4 text-amber-400" />
          <h2 className="text-sm font-black uppercase tracking-widest text-zinc-300">
            Alert Rules
          </h2>
          {unackedCount > 0 && (
            <span className="text-[10px] font-bold px-2 py-0.5 rounded-full bg-rose-500/15 text-rose-300 border border-rose-500/30 animate-pulse">
              {unackedCount} unacked
            </span>
          )}
        </div>
        <button
          type="button"
          onClick={() => { setShowForm(!showForm); setEditingId(null); }}
          className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-bold bg-emerald-500/15 text-emerald-300 border border-emerald-500/30 hover:bg-emerald-500/25 transition"
        >
          <Plus className="w-3 h-3" />
          New Rule
        </button>
      </div>

      {error && (
        <div className="px-4 py-2 bg-rose-500/10 border-b border-rose-500/20 text-xs text-rose-300">
          {error}
        </div>
      )}

      {showForm && (
        <RuleForm
          onSubmit={createRule}
          onCancel={() => setShowForm(false)}
        />
      )}

      <div className="divide-y divide-white/5">
        {rules.length === 0 && !showForm && (
          <div className="px-4 py-8 text-center text-zinc-500 text-xs">
            <Bell className="w-6 h-6 mx-auto opacity-30 mb-2" />
            No alert rules configured. Click "New Rule" to start.
          </div>
        )}
        {rules.map((r) =>
          editingId === r.id ? (
            <RuleForm
              key={r.id}
              initial={r}
              onSubmit={(data) => updateRule(r.id, data)}
              onCancel={() => setEditingId(null)}
            />
          ) : (
            <RuleRow
              key={r.id}
              rule={r}
              onEdit={() => { setEditingId(r.id); setShowForm(false); }}
              onDelete={() => deleteRule(r.id)}
              onToggle={async (enabled) => updateRule(r.id, { enabled })}
            />
          )
        )}
      </div>

      {/* Unacknowledged events feed */}
      {events.length > 0 && (
        <div className="border-t border-white/10 bg-black/30">
          <div className="px-4 py-2 text-[10px] font-black uppercase tracking-widest text-zinc-500">
            Unacknowledged events ({events.length})
          </div>
          <div className="divide-y divide-white/[0.03] max-h-64 overflow-auto">
            {events.map((e) => (
              <div key={e.id} className="px-4 py-2 flex items-center gap-3 text-xs">
                <span className={`px-1.5 py-0.5 rounded text-[10px] font-bold border ${
                  (Number(e.observedValue) >= Number(e.threshold))
                    ? "bg-rose-500/15 text-rose-300 border-rose-500/30"
                    : "bg-emerald-500/15 text-emerald-300 border-emerald-500/30"
                }`}>
                  {e.observedValue.toFixed(2)}
                </span>
                <span className="flex-1 text-zinc-300 truncate">{e.message}</span>
                <span className="text-zinc-500 text-[10px]">{fmtTime(e.firedAt)}</span>
                <button
                  type="button"
                  onClick={() => ackEvent(e.id)}
                  className="flex items-center gap-1 px-2 py-1 rounded bg-emerald-500/15 text-emerald-300 border border-emerald-500/30 hover:bg-emerald-500/25 text-[10px] font-bold uppercase"
                  title="Acknowledge"
                >
                  <Check className="w-3 h-3" /> ACK
                </button>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

function RuleRow({
  rule,
  onEdit,
  onDelete,
  onToggle,
}: {
  rule: AlertRule;
  onEdit: () => void;
  onDelete: () => void;
  onToggle: (enabled: boolean) => void;
}) {
  const metric = METRICS.find((m) => m.id === rule.metric);
  const op = OPS.find((o) => o.id === rule.operator);
  return (
    <div className="px-4 py-3 flex items-center gap-4 hover:bg-white/[0.02]">
      <button
        type="button"
        onClick={() => onToggle(!rule.enabled)}
        className={`relative w-9 h-5 rounded-full transition ${rule.enabled ? "bg-emerald-500/30" : "bg-white/10"}`}
        title={rule.enabled ? "Disable" : "Enable"}
      >
        <span
          className={`absolute top-0.5 w-4 h-4 rounded-full bg-white transition-all ${rule.enabled ? "left-4 bg-emerald-300" : "left-0.5"}`}
        />
      </button>
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2 mb-0.5">
          <span className="text-sm font-bold text-zinc-200 truncate">{rule.name}</span>
          <span className={`text-[10px] font-bold px-1.5 py-0.5 rounded border ${SEV_COLORS[rule.severity] || SEV_COLORS.warning}`}>
            {SEV_ICON[rule.severity]}
            <span className="ml-1">{rule.severity}</span>
          </span>
          {rule.lastFiredAt && (
            <span className="text-[10px] text-zinc-500 font-mono">
              last fired {fmtTime(rule.lastFiredAt)}
            </span>
          )}
        </div>
        <div className="text-[11px] text-zinc-500 font-mono">
          {metric?.label || rule.metric} {op?.label} {rule.threshold}{metric?.unit ? ` ${metric.unit}` : ""} {"\u00b7"} window {rule.windowSec}s
          {rule.notifyEmail && <span className="ml-2">{"\u00b7"} email {rule.notifyEmail}</span>}
          {rule.notifySlack && <span className="ml-2">{"\u00b7"} slack</span>}
        </div>
      </div>
      <button
        type="button"
        onClick={onEdit}
        className="p-1.5 rounded text-zinc-500 hover:text-zinc-200 hover:bg-white/5"
        title="Edit"
      >
        <Edit3 className="w-3.5 h-3.5" />
      </button>
      <button
        type="button"
        onClick={onDelete}
        className="p-1.5 rounded text-zinc-500 hover:text-rose-400 hover:bg-rose-500/10"
        title="Delete"
      >
        <Trash2 className="w-3.5 h-3.5" />
      </button>
    </div>
  );
}

function RuleForm({
  initial,
  onSubmit,
  onCancel,
}: {
  initial?: AlertRule;
  onSubmit: (data: any) => void;
  onCancel: () => void;
}) {
  const [metric, setMetric] = useState(initial?.metric || METRICS[0].id);
  const m = METRICS.find((x) => x.id === metric)!;
  const [operator, setOperator] = useState(initial?.operator || m.defaultOp);
  const [threshold, setThreshold] = useState<number>(initial?.threshold ?? m.defaultThreshold);
  const [windowSec, setWindowSec] = useState(initial?.windowSec || 300);
  const [name, setName] = useState(initial?.name || "");
  const [severity, setSeverity] = useState(initial?.severity || "warning");
  const [notifyEmail, setNotifyEmail] = useState(initial?.notifyEmail || "");
  const [notifySlack, setNotifySlack] = useState(initial?.notifySlack || "");
  const [enabled, setEnabled] = useState(initial?.enabled !== false);

  // Reset threshold default when metric changes
  const onMetricChange = (newMetric: string) => {
    const nm = METRICS.find((x) => x.id === newMetric)!;
    setMetric(newMetric);
    setOperator(nm.defaultOp);
    setThreshold(nm.defaultThreshold);
  };

  const submit = () => {
    if (!name.trim()) return;
    onSubmit({
      name: name.trim(),
      metric,
      operator,
      threshold,
      windowSec,
      enabled,
      severity,
      notifyEmail: notifyEmail || null,
      notifySlack: notifySlack || null,
    });
  };

  return (
    <div className="px-4 py-4 bg-white/[0.02] border-b border-white/10 space-y-3">
      <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
        <div className="md:col-span-2">
          <label className="block text-[10px] font-bold uppercase tracking-wider text-zinc-500 mb-1">Name</label>
          <input
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="e.g. Cache hit rate under 30%"
            className="w-full bg-white/5 border border-white/10 rounded-lg px-3 py-1.5 text-sm text-zinc-200 placeholder-zinc-600 outline-none focus:border-emerald-500/50"
          />
        </div>
        <div>
          <label className="block text-[10px] font-bold uppercase tracking-wider text-zinc-500 mb-1">Severity</label>
          <select value={severity} onChange={(e) => setSeverity(e.target.value)} className="w-full bg-white/5 border border-white/10 rounded-lg px-3 py-1.5 text-sm text-zinc-200 outline-none focus:border-emerald-500/50">
            {SEVERITIES.map((s) => (
              <option key={s.id} value={s.id} className="bg-gray-900 text-white">{s.label}</option>
            ))}
          </select>
        </div>
      </div>
      <div className="grid grid-cols-1 md:grid-cols-4 gap-3">
        <div>
          <label className="block text-[10px] font-bold uppercase tracking-wider text-zinc-500 mb-1">Metric</label>
          <select value={metric} onChange={(e) => onMetricChange(e.target.value)} className="w-full bg-white/5 border border-white/10 rounded-lg px-3 py-1.5 text-sm text-zinc-200 outline-none focus:border-emerald-500/50">
            {METRICS.map((m) => (
              <option key={m.id} value={m.id} className="bg-gray-900 text-white">{m.label}</option>
            ))}
          </select>
          <p className="text-[10px] text-zinc-600 mt-1">{m.help}</p>
        </div>
        <div>
          <label className="block text-[10px] font-bold uppercase tracking-wider text-zinc-500 mb-1">Operator</label>
          <select value={operator} onChange={(e) => setOperator(e.target.value)} className="w-full bg-white/5 border border-white/10 rounded-lg px-3 py-1.5 text-sm text-zinc-200 outline-none focus:border-emerald-500/50">
            {OPS.map((o) => (
              <option key={o.id} value={o.id} className="bg-gray-900 text-white">{o.label}</option>
            ))}
          </select>
        </div>
        <div>
          <label className="block text-[10px] font-bold uppercase tracking-wider text-zinc-500 mb-1">
            Threshold {m.unit && <span className="text-zinc-700">({m.unit})</span>}
          </label>
          <input
            type="number"
            value={threshold}
            onChange={(e) => setThreshold(Number(e.target.value))}
            className="w-full bg-white/5 border border-white/10 rounded-lg px-3 py-1.5 text-sm text-zinc-200 outline-none focus:border-emerald-500/50 font-mono"
          />
        </div>
        <div>
          <label className="block text-[10px] font-bold uppercase tracking-wider text-zinc-500 mb-1">Window (sec)</label>
          <input
            type="number"
            value={windowSec}
            onChange={(e) => setWindowSec(Number(e.target.value))}
            className="w-full bg-white/5 border border-white/10 rounded-lg px-3 py-1.5 text-sm text-zinc-200 outline-none focus:border-emerald-500/50 font-mono"
          />
        </div>
      </div>
      <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
        <div>
          <label className="block text-[10px] font-bold uppercase tracking-wider text-zinc-500 mb-1">Notify email (optional)</label>
          <input
            type="email"
            value={notifyEmail}
            onChange={(e) => setNotifyEmail(e.target.value)}
            placeholder="oncall@yourcompany.com"
            className="w-full bg-white/5 border border-white/10 rounded-lg px-3 py-1.5 text-sm text-zinc-200 placeholder-zinc-600 outline-none focus:border-emerald-500/50"
          />
        </div>
        <div>
          <label className="block text-[10px] font-bold uppercase tracking-wider text-zinc-500 mb-1">Slack webhook (optional)</label>
          <input
            type="url"
            value={notifySlack}
            onChange={(e) => setNotifySlack(e.target.value)}
            placeholder="https://hooks.slack.com/\u2026"
            className="w-full bg-white/5 border border-white/10 rounded-lg px-3 py-1.5 text-sm text-zinc-200 placeholder-zinc-600 outline-none focus:border-emerald-500/50"
          />
        </div>
      </div>
      <div className="flex items-center justify-between pt-2">
        <label className="flex items-center gap-2 text-xs text-zinc-300 cursor-pointer">
          <input
            type="checkbox"
            checked={enabled}
            onChange={() => setEnabled(!enabled)}
            className="accent-emerald-500 w-3 h-3"
          />
          <span>Enabled immediately</span>
        </label>
        <div className="flex gap-2">
          <button
            type="button"
            onClick={onCancel}
            className="px-3 py-1.5 rounded-lg text-xs font-bold bg-white/5 text-zinc-300 border border-white/10 hover:bg-white/10"
          >
            <X className="w-3 h-3 inline mr-1" />
            Cancel
          </button>
          <button
            type="button"
            onClick={submit}
            disabled={!name.trim()}
            className="px-3 py-1.5 rounded-lg text-xs font-bold bg-emerald-500/20 text-emerald-300 border border-emerald-500/30 hover:bg-emerald-500/30 disabled:opacity-30 disabled:cursor-not-allowed"
          >
            <Check className="w-3 h-3 inline mr-1" />
            {initial ? "Update" : "Create"}
          </button>
        </div>
      </div>
    </div>
  );
}
