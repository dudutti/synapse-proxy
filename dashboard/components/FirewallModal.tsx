"use client";

import React, { useState, useEffect } from "react";
import { motion } from "framer-motion";
import { Shield, X, AlertTriangle, ListChecks, EyeOff, Database, Wrench, RefreshCw } from "lucide-react";

// FirewallModal — Agent Firewall configuration for one virtual key.
//
// The modal mirrors the ApiKey fields added by the Firewall
// migration (dashboard/prisma/schema.prisma: enableL1, enableL2,
// enableL3, killSwitch, sessionTokenLimit, allowedTools,
// blockUnknownTools, redactPII). The proxy reads them on every
// request through proxy/internal/services/auth.go.
//
// State management: we keep a local `form` snapshot inside the
// modal so we can edit freely without mutating the parent's
// list state. The Save button calls onSave(form) which is
// implemented in settings/page.tsx -> saveFirewall (it Puts the
// values to /api/keys/[id], which writes Postgres and syncs
// Redis).
//
// UX note: we initialize `form` from `apiKey` every time the
// modal opens. This means re-opening the modal after a save
// shows the values that just got persisted (not stale values).
// We use a `key={showFirewallModal}` on the parent so React
// remounts the component on each open.

export type FirewallApiKey = {
  id: string;
  virtualKey: string;
  provider: string;
  enableL1?: boolean;
  enableL2?: boolean;
  enableL3?: boolean;
  killSwitch?: boolean;
  fingerprintLoopDetect?: boolean;
  sessionTokenLimit?: number | null;
  allowedTools?: string | null;
  blockUnknownTools?: boolean;
  redactPII?: boolean;
  toolTtls?: string | null;
};

type FirewallForm = {
  enableL1: boolean;
  enableL2: boolean;
  enableL3: boolean;
  killSwitch: boolean;
  fingerprintLoopDetect: boolean;
  sessionTokenLimit: number | null;
  allowedTools: string;
  blockUnknownTools: boolean;
  redactPII: boolean;
  toolTtls: string;
};

export type FirewallModalProps = {
  apiKey: FirewallApiKey;
  onClose: () => void;
  onSave: (form: FirewallForm) => void | Promise<void>;
};

export default function FirewallModal({ apiKey, onClose, onSave }: FirewallModalProps) {
  // Local snapshot. Initialised from apiKey on mount; user edits
  // freely until Save. The parent re-mounts us on each open
  // (via key={apiKey.id}) so this is always fresh.
  const [form, setForm] = useState<FirewallForm>({
    enableL1: apiKey.enableL1 ?? true,
    enableL2: apiKey.enableL2 ?? true,
    enableL3: apiKey.enableL3 ?? true,
    killSwitch: apiKey.killSwitch ?? false,
    fingerprintLoopDetect: apiKey.fingerprintLoopDetect ?? false,
    sessionTokenLimit: apiKey.sessionTokenLimit ?? null,
    allowedTools: apiKey.allowedTools ?? "",
    blockUnknownTools: apiKey.blockUnknownTools ?? false,
    redactPII: apiKey.redactPII ?? false,
    toolTtls: apiKey.toolTtls ?? "{}",
  });

  const [toolTtlsList, setToolTtlsList] = useState<Array<{ tool: string; ttl: number }>>(() => {
    try {
      const parsed = apiKey.toolTtls ? JSON.parse(apiKey.toolTtls) : {};
      return Object.entries(parsed).map(([tool, ttl]) => ({ tool, ttl: Number(ttl) }));
    } catch {
      return [];
    }
  });

  const [discoveredTools, setDiscoveredTools] = useState<string[]>([]);
  const [deniedTools, setDeniedTools] = useState<Set<string>>(new Set());
  const [toolsLoading, setToolsLoading] = useState(false);

  const refreshTools = async () => {
    setToolsLoading(true);
    try {
      const res = await fetch(`/api/admin/discovered-tools?vk=${encodeURIComponent(apiKey.virtualKey)}`);
      if (!res.ok) return;
      const data = await res.json();
      setDiscoveredTools(data.discovered || []);
      setDeniedTools(new Set(data.denied || []));
    } catch {
    } finally {
      setToolsLoading(false);
    }
  };

  const toggleTool = async (tool: string, deny: boolean) => {
    try {
      const res = await fetch(`/api/admin/discovered-tools?vk=${encodeURIComponent(apiKey.virtualKey)}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ tool, deny }),
      });
      if (!res.ok) return;
      const data = await res.json();
      setDeniedTools(new Set(data.denied || []));
    } catch {
    }
  };

  // Submit handler. We coerce the numeric field on the way out
  // because empty <input type="number"> gives us "" not null.
  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();

    const toolTtlsObj: Record<string, number> = {};
    toolTtlsList.forEach((item) => {
      const name = item.tool.trim();
      if (name) {
        toolTtlsObj[name] = Math.max(0, Number(item.ttl));
      }
    });

    const cleaned: FirewallForm = {
      ...form,
      sessionTokenLimit:
        form.sessionTokenLimit === null || Number.isNaN(form.sessionTokenLimit)
          ? null
          : Number(form.sessionTokenLimit),
      // allowedTools: split on commas, trim, drop empties, rejoin
      // so the user can paste "a,b , c " and we store "a,b,c".
      allowedTools: form.allowedTools
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean)
        .join(","),
      toolTtls: JSON.stringify(toolTtlsObj),
    };
    await onSave(cleaned);
  };

  // ESC closes the modal — keeps the keyboard-only workflow happy.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  // Auto-refresh discovered tools every 5s. We do this on a
  // background interval so the operator sees new tools appear
  // in real time as the agent uses them, without having to
  // close/reopen the modal.
  useEffect(() => {
    refreshTools();
    const interval = setInterval(refreshTools, 15000);
    return () => clearInterval(interval);
  }, [apiKey.virtualKey]);

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/80 backdrop-blur-sm"
      onClick={(e) => {
        // Click outside the panel = close. We don't propagate
        // clicks from inside the panel up here.
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <motion.div
        initial={{ opacity: 0, scale: 0.95 }}
        animate={{ opacity: 1, scale: 1 }}
        className="bg-[#0f0f0f] border border-cyan-500/20 p-6 rounded-2xl w-full max-w-2xl shadow-2xl relative max-h-[90vh] overflow-y-auto"
      >
        <div
          className="absolute top-4 right-4 cursor-pointer text-gray-500 hover:text-white"
          onClick={onClose}
        >
          <X className="w-5 h-5" />
        </div>

        <h2 className="text-xl font-bold text-white mb-1 flex items-center gap-2">
          <Shield className="text-cyan-400 w-6 h-6" />
          Agent Firewall
        </h2>
        <p className="text-xs text-gray-400 mb-6 font-mono">
          {apiKey.virtualKey} · {apiKey.provider}
        </p>

        <form onSubmit={handleSubmit} className="space-y-5">
          {/* Cache stages */}
          <fieldset className="border border-white/10 rounded-xl p-4">
            <legend className="text-xs font-bold text-gray-400 uppercase tracking-wide px-2 flex items-center gap-2">
              <Database className="w-3.5 h-3.5" /> Cache Stages
            </legend>
            <div className="grid grid-cols-3 gap-3 mt-2">
              <ToggleField
                label="L1 (exact hash)"
                checked={form.enableL1}
                onChange={(v) => setForm({ ...form, enableL1: v })}
                color="emerald"
              />
              <ToggleField
                label="L2 (semantic)"
                checked={form.enableL2}
                onChange={(v) => setForm({ ...form, enableL2: v })}
                color="emerald"
              />
              <ToggleField
                label="L3 (comp. preserved)"
                checked={form.enableL3}
                onChange={(v) => setForm({ ...form, enableL3: v })}
                color="emerald"
              />
            </div>
            <p className="text-[11px] text-gray-500 mt-3">
              Disable a stage to skip it on every request. Useful for benchmarking
              or to force upstream calls for a specific key.
            </p>
          </fieldset>

          {/* Tool Cache TTLs (Granular TTLs) */}
          <fieldset className="border border-white/10 rounded-xl p-4 bg-[#141414]/30">
            <legend className="text-xs font-bold text-gray-400 uppercase tracking-wide px-2 flex items-center gap-2">
              <Database className="w-3.5 h-3.5 text-cyan-400" /> Tool-Specific Cache TTLs
            </legend>
            <p className="text-[11px] text-gray-500 mt-1 mb-4">
              Override the global cache age limit per tool. Setting a TTL of 0s disables caching for that tool.
            </p>
            <div className="space-y-2.5">
              {toolTtlsList.map((item, idx) => (
                <div key={idx} className="flex items-center gap-3 bg-black/20 p-2 rounded-lg border border-white/5">
                  <div className="flex-1">
                    <input
                      type="text"
                      value={item.tool}
                      onChange={(e) => {
                        const newList = [...toolTtlsList];
                        newList[idx].tool = e.target.value;
                        setToolTtlsList(newList);
                      }}
                      placeholder="e.g. web_search"
                      className="w-full bg-black/40 border border-white/10 rounded-lg px-3 py-1.5 text-sm font-mono text-white focus:border-cyan-500 outline-none"
                    />
                  </div>
                  <div className="w-32 flex items-center gap-2">
                    <input
                      type="number"
                      min={0}
                      value={item.ttl}
                      onChange={(e) => {
                        const newList = [...toolTtlsList];
                        newList[idx].ttl = e.target.value === "" ? 0 : Number(e.target.value);
                        setToolTtlsList(newList);
                      }}
                      placeholder="60"
                      className="w-full bg-black/40 border border-white/10 rounded-lg px-3 py-1.5 text-sm font-mono text-white focus:border-cyan-500 outline-none"
                    />
                    <span className="text-xs text-gray-500 font-mono">s</span>
                  </div>
                  <button
                    type="button"
                    onClick={() => {
                      setToolTtlsList(toolTtlsList.filter((_, i) => i !== idx));
                    }}
                    className="p-1.5 text-gray-500 hover:text-red-400 hover:bg-red-500/10 rounded-lg transition-all"
                    title="Remove"
                  >
                    <X className="w-4 h-4" />
                  </button>
                </div>
              ))}
              {toolTtlsList.length === 0 && (
                <p className="text-xs text-gray-600 italic py-1 text-center">
                  No tool TTL overrides configured. Global cache defaults apply.
                </p>
              )}
              <button
                type="button"
                onClick={() => setToolTtlsList([...toolTtlsList, { tool: "", ttl: 60 }])}
                className="w-full py-2 border border-dashed border-white/10 hover:border-cyan-500/50 hover:bg-cyan-500/[0.02] text-xs font-semibold text-gray-400 hover:text-cyan-400 rounded-lg transition-all flex items-center justify-center gap-1.5 mt-2"
              >
                + Add Tool TTL Override
              </button>
            </div>
          </fieldset>

          {/* Kill switch + soft loop */}
          <fieldset className="border border-red-500/20 rounded-xl p-4 bg-red-500/[0.02]">
            <legend className="text-xs font-bold text-red-300 uppercase tracking-wide px-2 flex items-center gap-2">
              <AlertTriangle className="w-3.5 h-3.5" /> Kill Switch
            </legend>
            <ToggleField
              label="Stop agent on loop detection"
              checked={form.killSwitch}
              onChange={(v) => setForm({ ...form, killSwitch: v })}
              color="red"
              help="When the proxy detects a repeated payload hash in the same session, return HTTP 400 with a clear error so the agent stops drifting. Otherwise the loop is logged but requests pass through."
            />
            <div className="mt-3 pt-3 border-t border-white/5">
              <ToggleField
                label="Soft Loop Detect (tool fingerprint)"
                checked={form.fingerprintLoopDetect}
                onChange={(v) => setForm({ ...form, fingerprintLoopDetect: v })}
                color="amber"
                help="When the same (tool, args) tuple repeats 4× in 30s AND the cache has nothing to re-serve, return HTTP 429 + Retry-After: 60. Read-only tools (todo/plan/think/read_*/list_*/...) are exempt."
              />
            </div>
          </fieldset>

          {/* Tool filter */}
          <fieldset className="border border-white/10 rounded-xl p-4">
            <legend className="text-xs font-bold text-gray-400 uppercase tracking-wide px-2 flex items-center gap-2">
              <ListChecks className="w-3.5 h-3.5" /> Tool Filter
            </legend>
            <div className="space-y-3 mt-2">
              <ToggleField
                label="Block tool calls outside the whitelist"
                checked={form.blockUnknownTools}
                onChange={(v) => setForm({ ...form, blockUnknownTools: v })}
                color="amber"
                help="When enabled, requests whose tool_calls contain a function name not listed below return HTTP 400."
              />
              <div>
                <label className="text-xs text-gray-400 font-bold uppercase mb-1 block">
                  Allowed tools (comma-separated)
                </label>
                <input
                  type="text"
                  value={form.allowedTools}
                  onChange={(e) =>
                    setForm({ ...form, allowedTools: e.target.value })
                  }
                  placeholder="search_web,read_file,send_email"
                  className="w-full bg-black/40 border border-white/10 rounded-lg px-3 py-2 text-sm font-mono focus:border-cyan-500 outline-none"
                />
                <p className="text-[11px] text-gray-500 mt-1">
                  Function names exactly as they appear in the assistant
                  message&apos;s tool_calls. Leave empty to allow all when
                  Block Unknown Tools is off.
                </p>
              </div>
            </div>
          </fieldset>

          {/* Token limit per session */}
          <fieldset className="border border-white/10 rounded-xl p-4">
            <legend className="text-xs font-bold text-gray-400 uppercase tracking-wide px-2">
              Session Token Cap
            </legend>
            <div className="mt-2">
              <input
                type="number"
                min={0}
                value={form.sessionTokenLimit ?? ""}
                onChange={(e) =>
                  setForm({
                    ...form,
                    sessionTokenLimit:
                      e.target.value === "" ? null : Number(e.target.value),
                  })
                }
                placeholder="No limit"
                className="w-full bg-black/40 border border-white/10 rounded-lg px-3 py-2 text-sm font-mono focus:border-cyan-500 outline-none"
              />
              <p className="text-[11px] text-gray-500 mt-1">
                Cap the cumulative prompt tokens per session. When exceeded,
                the request still goes upstream (with isSimulated = true) so
                the dashboard can show &quot;what you would have saved&quot; for
                premium. Leave empty for no cap.
              </p>
            </div>
          </fieldset>

          {/* PII redaction */}
          <fieldset className="border border-white/10 rounded-xl p-4">
            <legend className="text-xs font-bold text-gray-400 uppercase tracking-wide px-2 flex items-center gap-2">
              <EyeOff className="w-3.5 h-3.5" /> Privacy
            </legend>
            <div className="mt-2">
              <ToggleField
                label="Redact PII in transit (emails)"
                checked={form.redactPII}
                onChange={(v) => setForm({ ...form, redactPII: v })}
                color="amber"
                help="The proxy strips email-like substrings from the request body before forwarding upstream. Useful for compliance or to keep LLM logs clean."
              />
            </div>
          </fieldset>

          {/* Discovered tools */}
          <fieldset className="border border-white/10 rounded-xl p-4">
            <legend className="text-xs font-bold text-gray-400 uppercase tracking-wide px-2 flex items-center gap-2">
              <Wrench className="w-3.5 h-3.5" /> Discovered Tools
              {toolsLoading && <RefreshCw className="w-3 h-3 animate-spin text-gray-500" />}
            </legend>
            <p className="text-[11px] text-gray-500 mt-2 mb-3">
              Tools your agent has called at least once. Uncheck to deny (HTTP 403 on next call). Auto-refreshes every 5s.
            </p>
            <div className="grid grid-cols-2 gap-2 max-h-48 overflow-y-auto">
              {discoveredTools.length === 0 && !toolsLoading && (
                <p className="col-span-2 text-xs text-gray-600 italic">
                  No tools discovered yet. Send a request with tool_calls to populate this list.
                </p>
              )}
              {discoveredTools.map((tool) => (
                <label key={tool} className="flex items-center gap-2 px-2 py-1.5 rounded-lg hover:bg-white/5 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={!deniedTools.has(tool)}
                    onChange={(e) => toggleTool(tool, !e.target.checked)}
                    className="w-4 h-4 accent-cyan-500 cursor-pointer"
                  />
                  <span className={`text-sm font-mono ${deniedTools.has(tool) ? "text-red-400 line-through" : "text-gray-200"}`}>
                    {tool}
                  </span>
                </label>
              ))}
            </div>
          </fieldset>

          {/* Footer */}
          <div className="flex items-center justify-between pt-2">
            <button
              type="button"
              onClick={onClose}
              className="px-5 py-2 text-sm font-bold text-gray-400 hover:text-white transition-colors"
            >
              Cancel
            </button>
            <button
              type="submit"
              className="px-6 py-2 bg-cyan-500 hover:bg-cyan-400 text-black font-bold rounded-lg transition-colors shadow-[0_0_20px_rgba(34,211,238,0.3)]"
            >
              Save Firewall
            </button>
          </div>
        </form>
      </motion.div>
    </div>
  );
}

// ToggleField — small reusable switch+label so the modal stays
// tidy. We keep the markup here instead of pulling in a UI
// library because the dashboard is small and we already use
// lucide-react + framer-motion. Toggling uses native checkbox
// for accessibility (focus ring, screen reader announce).
function ToggleField({
  label,
  checked,
  onChange,
  help,
  color,
}: {
  label: string;
  checked: boolean;
  onChange: (v: boolean) => void;
  help?: string;
  color: "emerald" | "red" | "amber";
}) {
  const accent =
    color === "red"
      ? "accent-red-500"
      : color === "amber"
      ? "accent-amber-500"
      : "accent-emerald-500";
  return (
    <label className="flex items-start gap-3 cursor-pointer">
      <input
        type="checkbox"
        checked={checked}
        onChange={(e) => onChange(e.target.checked)}
        className={`mt-1 w-4 h-4 ${accent} cursor-pointer`}
      />
      <div className="flex-1">
        <span className="text-sm font-medium text-gray-200">{label}</span>
        {help && <p className="text-[11px] text-gray-500 mt-0.5">{help}</p>}
      </div>
    </label>
  );
}