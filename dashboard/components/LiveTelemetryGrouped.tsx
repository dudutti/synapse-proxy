"use client";
import { useMemo, useState, useEffect, useRef, useCallback } from "react";

// Shape of a single live telemetry row, matches the payload emitted by
// /api/analytics/stream (route.ts).
export interface LiveRequest {
  id: string;
  timestamp: string; // ISO
  reqModel: string;
  savedInput: number;
  savedOutput: number;
  type: string;
  agentId?: string;
  agentLabel?: string;
  sessionId?: string;
  // Multiturn tracking. turnCount is the number of user/assistant
  // exchanges already in the messages array (0 = first turn). We use
  // this to show "Tour N" badges in the UI and detect agent drift
  // (a conversation that grows turn count without progressing is
  // suspicious). convSignature is the conversation fingerprint
  // (sha1 of system prompt + tool names, 8 hex chars). Two requests
  // with the same convSignature are very likely part of the same
  // conversation, even without an explicit sessionId.
  turnCount?: number;
  convSignature?: string;
}

type GroupBy = "agent" | "session" | "model";

interface Props {
  records: LiveRequest[];
  initialGroupBy?: GroupBy;
  /** Optional callback fired when the user stops a recording on a group. */
  onSnapshot?: (groupKey: string, snapshot: SessionSnapshot) => void;
}

export interface SessionSnapshot {
  groupKey: string;
  groupBy: GroupBy;
  startedAt: number;
  endedAt: number;
  requests: LiveRequest[];
  totalTokensSaved: number;
  totalCostSaved: number;
  cacheHitRate: number;
}

const AGENT_ICONS: Record<string, string> = {
  hermes: "🤖",
  openclaw: "🦅",
  "claude-code": "🛠️",
  langchain: "🔗",
  llamaindex: "🦙",
  "multi-agent": "🐝",
  "tool-using-agent": "🔧",
  "generic-assistant": "💬",
  benchmark: "🧪",
  playground: "🎮",
  curl: "📡",
  "python-sdk": "🐍",
  "chat-direct": "💬",
  unknown: "❓",
};

function iconFor(id: string): string {
  return AGENT_ICONS[id] ?? "❓";
}

function formatTime(iso: string): string {
  try {
    // Show date + time so the user doesn't lose track when
    // scrolling through hours of telemetry. We use a short
    // format (DD/MM HH:MM:SS) which fits in the existing Time
    // column without widening it. For the current day we
    // collapse the date to a dash to save space.
    const d = new Date(iso);
    const now = new Date();
    const sameDay = d.toDateString() === now.toDateString();
    const time = d.toLocaleTimeString("fr-FR", { hour: "2-digit", minute: "2-digit", second: "2-digit" });
    if (sameDay) return time;
    const date = d.toLocaleDateString("fr-FR", { day: "2-digit", month: "2-digit" });
    return `${date} ${time}`;
  } catch {
    return iso;
  }
}

interface Group {
  key: string;
  label: string;
  icon: string;
  records: LiveRequest[];
  tokensSaved: number;
  cacheHitRate: number;
  lastSeen: number;
}

function buildGroups(records: LiveRequest[], by: GroupBy): Group[] {
  const map = new Map<string, LiveRequest[]>();
  for (const r of records) {
    let key: string;
    if (by === "agent") {
      // We used to fold every request without an agentId into a
      // single "unknown" bucket, which created the false impression
      // that requests were disappearing and reappearing. Now we
      // use a short hash of (timestamp + model) as a fallback key
      // so each unidentified request gets its own visible row,
      // and the UI can show "Anonymous (N req)" with a count
      // instead of a giant black hole. The hash is short and
      // stable per row.
      if (r.agentId && r.agentId.length > 0) {
        key = `agent:${r.agentId}`;
      } else {
        key = `anon:${(r.timestamp || "").slice(-8)}_${(r.reqModel || "?").replace(/[^a-z0-9]/gi, "")}`;
      }
    } else if (by === "session") {
      // Grouping priority for sessions. All three branches use the
      // SAME "session:" prefix so rows from the same conversation
      // end up in the same bucket, regardless of which signal
      // (explicit sessionId, server-computed convSignature, or
      // none) identified them. Previously we had two separate
      // prefixes (session:/conv:) which caused the first row of a
      // multiturn conversation (turn=0, no sessionId yet) to
      // land in a different bucket than its follow-ups.
      //  1. explicit sessionId (X-SynapseProxy-Session header or
      //     the dashboard's Record Session feature)
      //  2. server-computed convSignature (system prompt + tool
      //     names hash) — this is the "natural conversation"
      //     grouping that the multiturn detector provides, even
      //     when the agent never sent an explicit sessionId
      //  3. legacy fallback: rows that have neither field.
      //     Before the multiturn/firewall migration these were
      //     every row from old logs, which collapsed into 59+
      //     separate "no-session:<timestamp>" buckets — one per
      //     row, since we explicitly avoided the giant "unknown"
      //     black hole. We now collapse them into a single
      //     "legacy" bucket (using the model as a sub-key so
      //     different models don't get merged into one bucket
      //     that hides everything).
      if (r.sessionId && r.sessionId.length > 0) {
        key = `session:${r.sessionId}`;
      } else if (r.convSignature && r.convSignature.length > 0) {
        key = `session:${r.convSignature}`;
      } else {
        // Legacy row (no sessionId, no convSignature). Group by
        // model so different models don't get merged into a
        // single opaque bucket.
        const model = (r.reqModel || "unknown").replace(/[^a-z0-9]/gi, "");
        key = `legacy:${model}`;
      }
    } else {
      // Model grouping: reqModel should always be set, but if it
      // isn't, fall back to "unknown-model" (single bucket, fine
      // because empty model rows are rare).
      key = r.reqModel || "unknown-model";
    }
    if (!map.has(key)) map.set(key, []);
    map.get(key)!.push(r);
  }
  return Array.from(map.entries())
    .map(([key, recs]) => {
      const tokensSaved = recs.reduce((s, r) => s + r.savedInput + r.savedOutput, 0);
      const hits = recs.filter((r) => r.type.startsWith("Cache Hit")).length;
      const first = recs[0];
      return {
        key,
        label:
          by === "agent"
            ? first?.agentLabel || key
            : by === "session"
            ? first?.sessionId
              ? `Session ${first.sessionId.slice(0, 12)}${first.sessionId.length > 12 ? "\u2026" : ""}`
              : key
            : key,
        icon: by === "agent" ? iconFor(key) : by === "session" ? "🧵" : "🧠",
        records: recs,
        tokensSaved,
        cacheHitRate: recs.length ? Math.round((hits / recs.length) * 100) : 0,
        lastSeen: Math.max(...recs.map((r) => new Date(r.timestamp).getTime())),
      };
    })
    .sort((a, b) => b.lastSeen - a.lastSeen);
}

/** Convert tokens saved into an estimated dollar figure, mirroring the
 *  rough pricing used elsewhere in the dashboard (~ $0.30 / 1M tok saved
 *  as a blended average). For an exact figure the parent should pass its
 *  own pricing map. */
function estimateCostSaved(tokensSaved: number): number {
  return (tokensSaved / 1_000_000) * 0.3;
}

export function LiveTelemetryGrouped({ records, initialGroupBy = "agent", onSnapshot }: Props) {
  const [groupBy, setGroupBy] = useState<GroupBy>(initialGroupBy);
  const [expanded, setExpanded] = useState<Set<string>>(() => {
    if (typeof window === "undefined") return new Set();
    try {
      return new Set(JSON.parse(localStorage.getItem("opti:lt:expanded") || "[]"));
    } catch {
      return new Set();
    }
  });
  const [recording, setRecording] = useState<{ key: string; startedAt: number } | null>(null);
  const [snapshotModal, setSnapshotModal] = useState<SessionSnapshot | null>(null);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(5);
  const [filter, setFilter] = useState("");
  const [diffPayload, setDiffPayload] = useState<LiveRequest | null>(null);
  const [knownAgents, setKnownAgents] = useState<Set<string>>(() => {
    if (typeof window === "undefined") return new Set();
    try {
      return new Set(JSON.parse(localStorage.getItem("opti:lt:knownAgents") || "[]"));
    } catch {
      return new Set();
    }
  });
  const [toasts, setToasts] = useState<Array<{ id: string; label: string }>>([]);

  // Persist expanded set
  useEffect(() => {
    try {
      localStorage.setItem("opti:lt:expanded", JSON.stringify(Array.from(expanded)));
    } catch {}
  }, [expanded]);

  // Persist known agents
  useEffect(() => {
    try {
      localStorage.setItem("opti:lt:knownAgents", JSON.stringify(Array.from(knownAgents)));
    } catch {}
  }, [knownAgents]);

  // Filter records by free-text search across model/agent/session/type
  const filteredRecords = useMemo(() => {
    if (!filter.trim()) return records;
    const q = filter.toLowerCase();
    return records.filter(
      (r) =>
        r.reqModel.toLowerCase().includes(q) ||
        (r.agentId || "").toLowerCase().includes(q) ||
        (r.agentLabel || "").toLowerCase().includes(q) ||
        (r.sessionId || "").toLowerCase().includes(q) ||
        r.type.toLowerCase().includes(q)
    );
  }, [records, filter]);

  // Detect new agents and toast
  useEffect(() => {
    const seen = new Set(knownAgents);
    let next: Set<string> | null = null;
    for (const r of records) {
      if (r.agentId && !seen.has(r.agentId)) {
        seen.add(r.agentId);
        if (!next) next = new Set(knownAgents);
        next.add(r.agentId);
        const id = `t-${Date.now()}-${r.agentId}`;
        setToasts((prev) => [...prev, { id, label: `${r.agentLabel || r.agentId} detected` }]);
        setTimeout(() => setToasts((prev) => prev.filter((t) => t.id !== id)), 4000);
      }
    }
    if (next) setKnownAgents(next);
  }, [records]); // intentionally NOT depending on knownAgents to avoid loops

  const groups = useMemo(() => buildGroups(filteredRecords, groupBy), [filteredRecords, groupBy]);

  // Top-saver agent: the one with the highest total tokens saved in the
  // current window. Used to display a 🐍† badge on the winning group.
  const topSaverKey = useMemo(() => {
    if (groupBy !== "agent" || groups.length === 0) return null;
    let best: { key: string; saved: number } | null = null;
    for (const g of groups) {
      if (!best || g.tokensSaved > best.saved) best = { key: g.key, saved: g.tokensSaved };
    }
    return best && best.saved > 0 ? best.key : null;
  }, [groups, groupBy]);

  // Total saved in the current window (for the live counter).
  const totalSaved = useMemo(
    () => records.reduce((s, r) => s + r.savedInput + r.savedOutput, 0),
    [records]
  );
  const totalCost = useMemo(() => estimateCostSaved(totalSaved), [totalSaved]);

  const totalPages = Math.max(1, Math.ceil(groups.length / pageSize));
  const safePage = Math.min(page, totalPages);
  const pagedGroups = useMemo(
    () => groups.slice((safePage - 1) * pageSize, safePage * pageSize),
    [groups, safePage, pageSize]
  );

  // When the groupBy mode changes, reset to page 1 so the user is not
  // stranded on a now-empty page. (We intentionally do NOT reset on
  // pageSize change ”” the safePage computation below handles overflow.)
  useEffect(() => {
    setPage(1);
  }, [groupBy]); // eslint-disable-line react-hooks/exhaustive-deps

  const toggle = useCallback((k: string) => {
    setExpanded((prev) => {
      const n = new Set(prev);
      if (n.has(k)) n.delete(k);
      else n.add(k);
      return n;
    });
  }, []);

  // Snapshot a group's current state without starting a recording
  // timer. This is a debug/share tool, distinct from the global
  // "Record Session" button in the header which writes a persistent
  // tag to Redis for long-lived session tracking. The previous
  // implementation conflated the two: clicking "Record" on a
  // group looked like it would persist a session, but it only
  // captured a transient window of local React state. Now the
  // button opens the modal immediately with the current bucket
  // contents, so the user gets instant feedback and a clear
  // mental model (snapshot = what's here now; record = track
  // going forward).
  const snapshotGroup = useCallback((key: string) => {
    const group = groups.find((g) => g.key === key);
    if (!group) return;
    const now = Date.now();
    // Use the oldest record in the group as the snapshot start,
    // so the modal shows the full bucket history, not just the
    // last few seconds. If the user wants a narrower window they
    // can filter before snapshotting.
    const oldestTs = group.records.reduce((min, r) => {
      const t = new Date(r.timestamp).getTime();
      return t < min ? t : min;
    }, now);
    const snap: SessionSnapshot = {
      groupKey: key,
      groupBy,
      startedAt: oldestTs,
      endedAt: now,
      requests: group.records.slice(),
      totalTokensSaved: group.records.reduce(
        (s, r) => s + r.savedInput + r.savedOutput,
        0
      ),
      totalCostSaved: 0,
      cacheHitRate: 0,
    };
    snap.totalCostSaved = estimateCostSaved(snap.totalTokensSaved);
    const recs = snap.requests;
    snap.cacheHitRate = recs.length
      ? Math.round(
          (recs.filter((r) => r.type.startsWith("Cache Hit")).length /
            recs.length) *
            100
        )
      : 0;
    // We deliberately do NOT open the local "Snapshot Modal"
    // here — that was creating a duplicate modal on top of the
    // Session Summary modal that the parent opens via
    // onSnapshot. The Session Summary modal already contains the
    // request table + Export JSON / Export CSV buttons, plus the
    // 3 observability graphs (Context Window, System Prompt Diff,
    // Agent Flow Timeline) when there are 2+ requests. Opening
    // both at once was confusing the user; now there is only
    // one modal, the Session Summary, which is the right place
    // for everything.
    onSnapshot?.(key, snap);
  }, [groups, groupBy, onSnapshot]);

  // Legacy recorder kept for backward compat with the modal flow
  // that the parent (page.tsx) wires up via onSnapshot. We no
  // longer surface the start/stop UI on each group — the bucket
  // button now snapshots directly — but the parent may still pass
  // a callback that expects a SessionSnapshot with the same shape,
  // so we keep startRecord/stopRecord exported via the API for
  // any caller that wants the timer-based flow.
  const startRecord = useCallback((key: string) => {
    setRecording({ key, startedAt: Date.now() });
  }, []);

  const stopRecord = useCallback(() => {
    if (!recording) return;
    const group = groups.find((g) => g.key === recording.key);
    if (!group) {
      setRecording(null);
      return;
    }
    const endedAt = Date.now();
    const snap: SessionSnapshot = {
      groupKey: recording.key,
      groupBy,
      startedAt: recording.startedAt,
      endedAt,
      requests: group.records.filter(
        (r) => new Date(r.timestamp).getTime() >= recording.startedAt
      ),
      totalTokensSaved: group.records
        .filter((r) => new Date(r.timestamp).getTime() >= recording.startedAt)
        .reduce((s, r) => s + r.savedInput + r.savedOutput, 0),
      totalCostSaved: 0,
      cacheHitRate: 0,
    };
    snap.totalCostSaved = estimateCostSaved(snap.totalTokensSaved);
    const recs = snap.requests;
    snap.cacheHitRate = recs.length
      ? Math.round((recs.filter((r) => r.type.startsWith("Cache Hit")).length / recs.length) * 100)
      : 0;
    setRecording(null);
    setSnapshotModal(snap);
    onSnapshot?.(recording.key, snap);
  }, [recording, groups, groupBy, onSnapshot]);

  // Auto-stop recording if the group disappears
  useEffect(() => {
    if (recording && !groups.find((g) => g.key === recording.key)) {
      setRecording(null);
    }
  }, [recording, groups]);

  return (
    <div className="flex flex-col gap-2">
      {/* Header bar: group selector + search + live counter + record indicator */}
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-2 text-xs">
            <span className="text-zinc-500">Grouper par :</span>
            {(["agent", "session", "model"] as GroupBy[]).map((b) => (
              <button
                key={b}
                onClick={() => setGroupBy(b)}
                className={`rounded px-2 py-1 transition-colors ${
                  groupBy === b
                    ? "bg-emerald-500/20 text-emerald-300"
                    : "text-zinc-400 hover:bg-white/5 hover:text-white"
                }`}
              >
                {b === "agent" ? "Agent" : b === "session" ? "Session" : "Modèle"}
              </button>
            ))}
          </div>
          <input
            type="text"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="🔍 Filter (model, agent, session, type)…"
            className="w-64 rounded border border-white/10 bg-black/30 px-2 py-1 text-xs text-zinc-200 placeholder-zinc-600 focus:border-emerald-500/50 focus:outline-none"
          />
        </div>
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-3 text-xs text-zinc-400">
            <span>
              <span className="text-zinc-200">{records.length}</span> req
            </span>
            <span>
              <span className="text-emerald-300">+{totalSaved.toLocaleString()}</span> tok
            </span>
            <span className="text-emerald-300">${totalCost.toFixed(4)}</span>
          </div>
          {recording && (
            <button
              onClick={stopRecord}
              className="flex items-center gap-1.5 rounded border border-red-500/40 bg-red-500/10 px-2 py-1 text-xs text-red-300 hover:bg-red-500/20"
            >
              <span className="h-2 w-2 animate-pulse rounded-full bg-red-500" />
              Stop recording ({recording.key})
            </button>
          )}
        </div>
      </div>

      {/* Empty state */}
      {groups.length === 0 && (
        <div className="rounded-lg border border-white/5 bg-black/20 p-6 text-center text-xs text-zinc-500">
          No telemetry data yet. Waiting for the first request…
        </div>
      )}

      {/* Groups (accordions) */}
      {pagedGroups.map((g) => {
        const isOpen = expanded.has(g.key);
        const isRec = recording?.key === g.key;
        return (
          <div key={g.key} className="rounded-lg border border-white/5 bg-black/30">
            <button
              onClick={() => toggle(g.key)}
              className="flex w-full items-center justify-between px-4 py-3 text-left transition-colors hover:bg-white/5"
            >
              <div className="flex items-center gap-3">
                <span className="text-zinc-500">{isOpen ? "▼" : "▶"}</span>
                <span className="text-base">{g.icon}</span>
                <span className="font-medium text-zinc-100">{g.label}</span>
                {groupBy === "agent" && g.key === topSaverKey && (
                  <span
                    title="Top saver in the current window"
                    className="rounded bg-amber-500/15 px-1.5 py-0.5 text-[10px] text-amber-300"
                  >
                    🏆 Top saver
                  </span>
                )}
                {groupBy === "agent" && (g.key.startsWith("anon:") || g.key === "agent:chat-direct" || g.key === "agent:curl" || g.key === "agent:python-sdk") && (
                  <span
                    title="Add the X-Synapse-Proxy-Client header (e.g. 'Hermes-Agent/1.0') or send a system prompt containing your agent name to auto-tag requests."
                    className="cursor-help rounded bg-amber-500/10 px-1.5 py-0.5 text-[10px] text-amber-300"
                  >
                    ?
                  </span>
                )}
                <span className="text-xs text-zinc-500">
                  {g.records.length} req {"\u00b7"} {g.tokensSaved.toLocaleString()} tok saved {"\u00b7"}{" "}
                  <span className="text-emerald-300">${estimateCostSaved(g.tokensSaved).toFixed(4)}</span>
                </span>
                {g.cacheHitRate > 0 && (
                  <span className="rounded bg-emerald-500/10 px-2 py-0.5 text-xs text-emerald-400">
                    {g.cacheHitRate}% hit
                  </span>
                )}
              </div>
              <div className="flex items-center gap-2">
                {isRec ? (
                  <span className="flex items-center gap-1.5 text-xs text-red-400">
                    <span className="h-2 w-2 animate-pulse rounded-full bg-red-500" /> REC
                  </span>
                ) : (
                  <span
                    role="button"
                    onClick={(e) => {
                      e.stopPropagation();
                      snapshotGroup(g.key);
                    }}
                    className="rounded border border-white/10 px-2 py-1 text-xs text-zinc-300 hover:bg-white/5"
                    title="Snapshot this group's current state as a downloadable artifact. For long-lived recording, use the Record Session button in the header."
                  >
                    📸 Snapshot
                  </span>
                )}
                <span
                  role="button"
                  onClick={(e) => {
                    e.stopPropagation();
                    downloadGroupCSV(g);
                  }}
                  className="rounded border border-white/10 px-2 py-1 text-xs text-zinc-300 hover:bg-white/5"
                  title="Export this group's requests to CSV"
                >
                  ⬇ CSV
                </span>
                <span
                  role="button"
                  onClick={(e) => {
                    e.stopPropagation();
                    downloadGroupJSON(g);
                  }}
                  className="rounded border border-white/10 px-2 py-1 text-xs text-zinc-300 hover:bg-white/5"
                  title="Export this group's requests to JSON"
                >
                  ⬇ JSON
                </span>
              </div>
            </button>

            {isOpen && (
              <GroupBody records={g.records} onDiff={setDiffPayload} />
            )}
          </div>
        );
      })}

      {/* Pagination bar (visible whenever there is more than one group).
          Rendered as the LAST child of a flex-col so it naturally sits at
          the bottom of the scroll area defined in page.tsx. */}
      {groups.length > 1 && (
        <PaginationBar
          page={safePage}
          totalPages={totalPages}
          totalGroups={groups.length}
          pageSize={pageSize}
          onPageChange={setPage}
          onPageSizeChange={setPageSize}
        />
      )}

      {/* Snapshot modal */}
      {snapshotModal && (
        <SnapshotModal
          snapshot={snapshotModal}
          onClose={() => setSnapshotModal(null)}
        />
      )}

      {/* Diff modal (re-clickable from a row's Diff button) */}
      {diffPayload && (
        <DiffModal record={diffPayload} onClose={() => setDiffPayload(null)} />
      )}

      {/* Toasts (new agent detection) */}
      <ToastStack toasts={toasts} />
    </div>
  );
}

function SnapshotModal({
  snapshot,
  onClose,
}: {
  snapshot: SessionSnapshot;
  onClose: () => void;
}) {
  const durationSec = Math.max(1, Math.round((snapshot.endedAt - snapshot.startedAt) / 1000));
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4"
      onClick={onClose}
    >
      <div
        className="w-full max-w-2xl rounded-lg border border-white/10 bg-[#0a0a0a] p-6 text-zinc-100 shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold">📼 Session Snapshot</h2>
          <button
            onClick={onClose}
            className="rounded p-1 text-zinc-400 hover:bg-white/5 hover:text-white"
          >
            ✕
          </button>
        </div>
        <div className="mb-4 grid grid-cols-2 gap-4 text-sm md:grid-cols-4">
          <Stat label="Group" value={snapshot.groupKey} />
          <Stat label="Duration" value={`${durationSec}s`} />
          <Stat label="Requests" value={String(snapshot.requests.length)} />
          <Stat label="Hit rate" value={`${snapshot.cacheHitRate}%`} />
          <Stat label="Tokens saved" value={snapshot.totalTokensSaved.toLocaleString()} />
          <Stat label="Cost saved" value={`$${snapshot.totalCostSaved.toFixed(6)}`} />
        </div>
        <div className="mb-4 max-h-64 overflow-y-auto rounded border border-white/5">
          <table className="w-full text-xs">
            <thead className="bg-white/5 text-zinc-400">
              <tr>
                <th className="px-3 py-2 text-left">Time</th>
                <th className="px-3 py-2 text-left">Model</th>
                <th className="px-3 py-2 text-right">In</th>
                <th className="px-3 py-2 text-right">Out</th>
                <th className="px-3 py-2 text-left">Type</th>
              </tr>
            </thead>
            <tbody>
              {snapshot.requests.map((r) => (
                <tr key={r.id} className="border-t border-white/5">
                  <td className="px-3 py-1.5 font-mono text-zinc-400">{formatTime(r.timestamp)}</td>
                  <td className="px-3 py-1.5 font-mono text-emerald-300">{r.reqModel}</td>
                  <td className="px-3 py-1.5 text-right text-emerald-400">+{r.savedInput}</td>
                  <td className="px-3 py-1.5 text-right text-purple-400">+{r.savedOutput}</td>
                  <td className="px-3 py-1.5 text-zinc-300">{r.type}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <div className="flex justify-end gap-2">
          <button
            onClick={() => downloadJSON(snapshot)}
            className="rounded border border-white/10 px-3 py-1.5 text-xs hover:bg-white/5"
          >
            Export JSON
          </button>
          <button
            onClick={() => downloadCSV(snapshot)}
            className="rounded border border-white/10 px-3 py-1.5 text-xs hover:bg-white/5"
          >
            Export CSV
          </button>
          <button
            onClick={onClose}
            className="rounded bg-emerald-500/20 px-3 py-1.5 text-xs text-emerald-300 hover:bg-emerald-500/30"
          >
            Close
          </button>
        </div>
      </div>
    </div>
  );
}

function PaginationBar({
  page,
  totalPages,
  totalGroups,
  pageSize,
  onPageChange,
  onPageSizeChange,
}: {
  page: number;
  totalPages: number;
  totalGroups: number;
  pageSize: number;
  onPageChange: (p: number) => void;
  onPageSizeChange: (n: number) => void;
}) {
  const start = (page - 1) * pageSize + 1;
  const end = Math.min(page * pageSize, totalGroups);
  // Build a compact page list with ellipsis: 1 … 4 5 [6] 7 8 … 20
  const pageNumbers = useMemo(() => buildPageList(page, totalPages), [page, totalPages]);

  return (
    <div className="mt-2 flex flex-shrink-0 flex-wrap items-center justify-between gap-2 rounded-lg border border-white/10 bg-black/60 px-3 py-2 text-xs text-zinc-400 backdrop-blur">
      <div>
        Showing <span className="text-zinc-200">{start}{"\u2013"}{end}</span> of{" "}
        <span className="text-zinc-200">{totalGroups}</span> groups
      </div>
      <div className="flex items-center gap-1">
        <button
          onClick={() => onPageChange(1)}
          disabled={page === 1}
          className="rounded px-2 py-1 hover:bg-white/5 disabled:opacity-30"
          title="First page"
        >
          {"\u00ab"}
        </button>
        <button
          onClick={() => onPageChange(Math.max(1, page - 1))}
          disabled={page === 1}
          className="rounded px-2 py-1 hover:bg-white/5 disabled:opacity-30"
        >
          {"\u2039"} Prev
        </button>
        {pageNumbers.map((p, i) =>
          p === "\u2026" ? (
            <span key={`gap-${i}`} className="px-1 text-zinc-600">{"\u2026"}</span>
          ) : (
            <button
              key={p}
              onClick={() => onPageChange(p)}
              className={`min-w-[2rem] rounded px-2 py-1 text-center ${
                p === page
                  ? "bg-emerald-500/20 text-emerald-300"
                  : "hover:bg-white/5"
              }`}
            >
              {p}
            </button>
          )
        )}
        <button
          onClick={() => onPageChange(Math.min(totalPages, page + 1))}
          disabled={page === totalPages}
          className="rounded px-2 py-1 hover:bg-white/5 disabled:opacity-30"
        >
          Next {"\u203a"}
        </button>
        <button
          onClick={() => onPageChange(totalPages)}
          disabled={page === totalPages}
          className="rounded px-2 py-1 hover:bg-white/5 disabled:opacity-30"
          title="Last page"
        >
          {"\u00bb"}
        </button>
      </div>
      <div className="flex items-center gap-1">
        <span>Per page:</span>
        {[5, 10, 25, 50].map((n) => (
          <button
            key={n}
            onClick={() => onPageSizeChange(n)}
            className={`rounded px-1.5 py-0.5 ${
              pageSize === n
                ? "bg-emerald-500/20 text-emerald-300"
                : "hover:bg-white/5"
            }`}
          >
            {n}
          </button>
        ))}
      </div>
    </div>
  );
}

function buildPageList(page: number, total: number): (number | "…")[] {
  if (total <= 7) return Array.from({ length: total }, (_, i) => i + 1);
  const out: (number | "…")[] = [];
  const add = (v: number | "…") => out.push(v);
  add(1);
  if (page > 4) add("…");
  for (let p = Math.max(2, page - 1); p <= Math.min(total - 1, page + 1); p++) add(p);
  if (page < total - 3) add("…");
  add(total);
  return out;
}

function GroupBody({ records, onDiff }: { records: LiveRequest[]; onDiff: (r: LiveRequest) => void }) {
  const GROUP_PAGE_SIZE = 50;
  const [page, setPage] = useState(1);
  const totalPages = Math.max(1, Math.ceil(records.length / GROUP_PAGE_SIZE));
  const safePage = Math.min(page, totalPages);
  const start = (safePage - 1) * GROUP_PAGE_SIZE;
  const pageRecords = records.slice(start, start + GROUP_PAGE_SIZE);

  // When the records array changes (different group), reset to page 1
  useEffect(() => {
    setPage(1);
  }, [records]);

  if (records.length === 0) {
    return (
      <div className="border-t border-white/5 px-4 py-3 text-center text-xs text-zinc-500">
        No requests in this group.
      </div>
    );
  }

  return (
    <div className="border-t border-white/5">
      <table className="w-full text-xs">
        <thead className="text-zinc-500">
          <tr>
            <th className="px-4 py-2 text-left">Time</th>
            <th className="px-4 py-2 text-left">Model</th>
            <th className="px-4 py-2 text-right">In saved</th>
            <th className="px-4 py-2 text-right">Out saved</th>
            <th className="px-4 py-2 text-left">Type</th>
            <th className="px-4 py-2 text-right">Action</th>
          </tr>
        </thead>
        <tbody>
          {pageRecords.map((r) => (
            <tr key={r.id} className="border-t border-white/5 hover:bg-white/[0.02]">
              <td className="px-4 py-1.5 font-mono text-zinc-400">{formatTime(r.timestamp)}</td>
              <td className="px-4 py-1.5 font-mono text-emerald-300">{r.reqModel}</td>
              <td className="px-4 py-1.5 text-right text-emerald-400">+{r.savedInput}</td>
              <td className="px-4 py-1.5 text-right text-purple-400">+{r.savedOutput}</td>
              <td className="px-4 py-1.5">
                <span
                  className={`rounded px-2 py-0.5 text-xs font-bold border ${
                    r.type === "L0 Coalesced (in-flight)" || r.type.startsWith("L0") || r.type === "Cache Hit (L0)"
                      ? "bg-cyan-500/20 text-cyan-300 border-cyan-500/30"
                      : r.type === "L1 Cache (exact)" || r.type.startsWith("L1")
                      ? "bg-blue-500/20 text-blue-300 border-blue-500/30"
                      : r.type === "L2 Cache (semantic)" || r.type.startsWith("L2")
                      ? "bg-emerald-500/20 text-emerald-300 border-emerald-500/30"
                      : r.type === "L3 Standard (compressed)" || r.type.startsWith("L3") || r.type === "Cache Hit (L3)"
                      ? "bg-purple-500/20 text-purple-300 border-purple-500/30"
                      : r.type === "Cache Hit (LOOP)" || r.type.startsWith("LOOP")
                      ? "bg-amber-500/20 text-amber-300 border-amber-500/30"
                      : "bg-zinc-700/40 text-zinc-300 border-white/5"
                  }`}
                >
                  {r.type}
                </span>
              </td>
              <td className="px-4 py-1.5 text-right">
                <button
                  onClick={() => onDiff(r)}
                  className="rounded border border-white/10 px-2 py-0.5 text-[11px] text-zinc-300 hover:bg-white/5"
                >
                  Diff
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      {totalPages > 1 && (
        <div className="flex items-center justify-between gap-2 border-t border-white/5 bg-black/20 px-4 py-1.5 text-[11px] text-zinc-400">
          <span>
            Showing <span className="text-zinc-200">{start + 1} {"\u2013"} {Math.min(start + GROUP_PAGE_SIZE, records.length)}</span> of {records.length}
          </span>
          <div className="flex items-center gap-1">
            <button
              onClick={() => setPage((p) => Math.max(1, p - 1))}
              disabled={safePage === 1}
              className="rounded px-2 py-0.5 hover:bg-white/5 disabled:opacity-30"
            >
              {"\u2039"} Prev
            </button>
            <span className="px-1 text-zinc-500">
              {safePage} / {totalPages}
            </span>
            <button
              onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
              disabled={safePage === totalPages}
              className="rounded px-2 py-0.5 hover:bg-white/5 disabled:opacity-30"
            >
              Next {"\u203a"}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded border border-white/5 bg-white/[0.02] p-3">
      <div className="text-xs text-zinc-500">{label}</div>
      <div className="mt-1 font-mono text-sm text-zinc-100">{value}</div>
    </div>
  );
}

function downloadJSON(snap: SessionSnapshot) {
  const blob = new Blob([JSON.stringify(snap, null, 2)], { type: "application/json" });
  triggerDownload(blob, `session-${snap.groupKey}-${snap.startedAt}.json`);
}

function downloadCSV(snap: SessionSnapshot) {
  const rows = [
    ["time", "model", "savedInput", "savedOutput", "type"].join(","),
    ...snap.requests.map((r) =>
      [r.timestamp, r.reqModel, r.savedInput, r.savedOutput, r.type]
        .map((v) => `"${String(v).replace(/"/g, '""')}"`)
        .join(",")
    ),
  ];
  const blob = new Blob([rows.join("\n")], { type: "text/csv" });
  triggerDownload(blob, `session-${snap.groupKey}-${snap.startedAt}.csv`);
}

function triggerDownload(blob: Blob, name: string) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
}

// --- per-group exports -------------------------------------------------

function downloadGroupCSV(g: { key: string; records: LiveRequest[] }) {
  const rows = [
    ["time", "model", "savedInput", "savedOutput", "type", "agentId", "sessionId"].join(","),
    ...g.records.map((r) =>
      [r.timestamp, r.reqModel, r.savedInput, r.savedOutput, r.type, r.agentId || "", r.sessionId || ""]
        .map((v) => `"${String(v).replace(/"/g, '""')}"`)
        .join(",")
    ),
  ];
  const blob = new Blob([rows.join("\n")], { type: "text/csv" });
  const safeKey = g.key.replace(/[^a-zA-Z0-9_-]/g, "_");
  triggerDownload(blob, `group-${safeKey}-${Date.now()}.csv`);
}

function downloadGroupJSON(g: { key: string; records: LiveRequest[] }) {
  const safeKey = g.key.replace(/[^a-zA-Z0-9_-]/g, "_");
  const blob = new Blob([JSON.stringify(g, null, 2)], { type: "application/json" });
  triggerDownload(blob, `group-${safeKey}-${Date.now()}.json`);
}

// --- diff modal --------------------------------------------------------

function DiffModal({ record, onClose }: { record: LiveRequest; onClose: () => void }) {
  const [fullLog, setFullLog] = useState<{
    originalPayload: string | null;
    optimizedPayload: string | null;
    responsePayload: string | null;
    cacheLevel: string;
    promptTokensOrig: number;
    completionTokensOrig: number;
    promptTokensOpt: number;
    completionTokensOpt: number;
    costSaved: number;
  } | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [viewMode, setViewMode] = useState<"split" | "unified">("unified");

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    fetch(`/api/telemetry/${record.id}`)
      .then(async (r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        return r.json();
      })
      .then((data) => {
        if (cancelled) return;
        setFullLog(data);
        setLoading(false);
      })
      .catch((e) => {
        if (cancelled) return;
        setError(e.message);
        setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [record.id]);

  // Pretty-print JSON if possible, with stable key order. Without
  // sorting, two semantically identical payloads with shuffled keys
  // (Go map encoding, proxy re-serialization, etc.) would render as
  // a wall of false-positive line changes.
  const pretty = (raw: string | null | undefined): string => {
    if (!raw) return "(empty)";
    try {
      const sortKeys = (v: any): any => {
        if (Array.isArray(v)) return v.map(sortKeys);
        if (v && typeof v === "object") {
          return Object.keys(v)
            .sort()
            .reduce((acc: any, k) => {
              acc[k] = sortKeys(v[k]);
              return acc;
            }, {});
        }
        return v;
      };
      return JSON.stringify(sortKeys(JSON.parse(raw)), null, 2);
    } catch {
      return raw;
    }
  };

  // Tokenize a line into alternating word/space runs so we can
  // highlight sub-line changes in the optimized column.
  const tokenize = (line: string): string[] => {
    // Split keeping the whitespace delimiters: ["hello", " ", "world", ",", " foo"]
    return line.match(/\s+|[^\s]+/g) || [];
  };

  // Word-level LCS for intra-line diff (GitHub-style: only one side
  // gets highlighted per pair ”” left = removed, right = added).
  const wordDiff = (
    aLine: string,
    bLine: string
  ): { leftParts: Array<{ text: string; removed: boolean }>; rightParts: Array<{ text: string; added: boolean }> } => {
    const aT = tokenize(aLine);
    const bT = tokenize(bLine);
    const m = aT.length;
    const n = bT.length;
    // Cap inputs to keep diff cost bounded on huge tool-output lines.
    const cap = 400;
    const a = aT.slice(0, cap);
    const b = bT.slice(0, cap);
    const dp: number[][] = Array.from({ length: m + 1 }, () =>
      new Array(n + 1).fill(0)
    );
    for (let i = m - 1; i >= 0; i--) {
      for (let j = n - 1; j >= 0; j--) {
        if (a[i] === b[j]) dp[i][j] = dp[i + 1][j + 1] + 1;
        else dp[i][j] = Math.max(dp[i + 1][j], dp[i][j + 1]);
      }
    }
    const left: Array<{ text: string; removed: boolean }> = [];
    const right: Array<{ text: string; added: boolean }> = [];
    let i = 0,
      j = 0;
    while (i < m && j < n) {
      if (a[i] === b[j]) {
        left.push({ text: a[i], removed: false });
        right.push({ text: b[j], added: false });
        i++;
        j++;
      } else if (dp[i + 1][j] >= dp[i][j + 1]) {
        left.push({ text: a[i], removed: true });
        i++;
      } else {
        right.push({ text: b[j], added: true });
        j++;
      }
    }
    while (i < m) {
      left.push({ text: a[i++], removed: true });
    }
    while (j < n) {
      right.push({ text: b[j++], added: true });
    }
    return { leftParts: left, rightParts: right };
  };

  // Build a per-line diff (green = added in optimized, red = removed
  // from original, unchanged lines = dim grey). For lines that have
  // a "neighbour" on the other side, run a word-level diff to
  // highlight the exact tokens that changed (GitHub-style).
  const lineDiff = useMemo(() => {
    if (!fullLog) return null;
    const a = pretty(fullLog.originalPayload).split("\n");
    const b = pretty(fullLog.optimizedPayload).split("\n");

    // Quick line-level diff via Longest Common Subsequence
    const m = a.length;
    const n = b.length;
    const dp: number[][] = Array.from({ length: m + 1 }, () =>
      new Array(n + 1).fill(0)
    );
    for (let i = m - 1; i >= 0; i--) {
      for (let j = n - 1; j >= 0; j--) {
        if (a[i] === b[j]) dp[i][j] = dp[i + 1][j + 1] + 1;
        else dp[i][j] = Math.max(dp[i + 1][j], dp[i][j + 1]);
      }
    }
    type Row =
      | { kind: "same"; aLine: string; bLine: string }
      | { kind: "add"; bLine: string }
      | { kind: "del"; aLine: string }
      | { kind: "mod"; aLine: string; bLine: string; left: ReturnType<typeof wordDiff>["leftParts"]; right: ReturnType<typeof wordDiff>["rightParts"] };
    const out: Row[] = [];
    let i = 0,
      j = 0;
    while (i < m && j < n) {
      if (a[i] === b[j]) {
        out.push({ kind: "same", aLine: a[i], bLine: b[j] });
        i++;
        j++;
      } else if (dp[i + 1][j] >= dp[i][j + 1] && dp[i + 1][j] > dp[i][j + 1]) {
        out.push({ kind: "del", aLine: a[i] });
        i++;
      } else if (dp[i][j + 1] > dp[i + 1][j]) {
        out.push({ kind: "add", bLine: b[j] });
        j++;
      } else {
        // Modified line: same row, different content ”” run word diff
        const w = wordDiff(a[i], b[j]);
        out.push({ kind: "mod", aLine: a[i], bLine: b[j], left: w.leftParts, right: w.rightParts });
        i++;
        j++;
      }
    }
    while (i < m) {
      out.push({ kind: "del", aLine: a[i++] });
    }
    while (j < n) {
      out.push({ kind: "add", bLine: b[j++] });
    }

    // Build stats for the legend
    const stats = { same: 0, added: 0, removed: 0, modified: 0 };
    for (const r of out) {
      if (r.kind === "same") stats.same++;
      else if (r.kind === "add") stats.added++;
      else if (r.kind === "del") stats.removed++;
      else stats.modified++;
    }
    return { rows: out, stats };
  }, [fullLog]);

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4"
      onClick={onClose}
    >
      <div
        className="w-full max-w-[96vw] h-[96vh] rounded-lg border border-white/10 bg-[#0a0a0a] p-6 text-zinc-100 shadow-2xl flex flex-col"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold">🔍 Request diff</h2>
          <button
            onClick={onClose}
            className="rounded p-1 text-zinc-400 hover:bg-white/5 hover:text-white"
          >
            ✕
          </button>
        </div>
        <div className="mb-3 grid grid-cols-2 gap-3 text-xs text-zinc-400 md:grid-cols-5">
          <div>
            <div className="text-zinc-500">Time</div>
            <div className="font-mono text-zinc-200">{formatTime(record.timestamp)}</div>
          </div>
          <div>
            <div className="text-zinc-500">Model</div>
            <div className="font-mono text-emerald-300">{record.reqModel}</div>
          </div>
          <div>
            <div className="text-zinc-500">Agent</div>
            <div className="text-zinc-200">{record.agentLabel || record.agentId || "\u2014"}</div>
          </div>
          <div>
            <div className="text-zinc-500">Tour</div>
            <div className="font-mono text-violet-300">
              {record.turnCount != null && record.turnCount >= 1 ? (
                <span title={`Conversation fingerprint: ${record.convSignature || "n/a"}`}>
                  {record.turnCount}
                  <span className="text-zinc-500 ml-1">/{record.turnCount + 1}</span>
                </span>
              ) : (
                <span className="text-zinc-500">{"\u2014"}</span>
              )}
            </div>
          </div>
          <div>
            <div className="text-zinc-500">Cache</div>
            <div
              className={`font-mono ${
                fullLog?.cacheLevel === "L0"
                  ? "text-cyan-300"
                  : fullLog?.cacheLevel === "L1"
                  ? "text-blue-300"
                  : fullLog?.cacheLevel === "L2"
                  ? "text-emerald-300"
                  : fullLog?.cacheLevel === "L3"
                  ? "text-purple-300"
                  : "text-zinc-400"
              }`}
            >
              {fullLog?.cacheLevel || record.type}
            </div>
          </div>
          <div>
            <div className="text-zinc-500">Saved</div>
            <div className="text-emerald-300">
              {fullLog
                ? `+${(fullLog.promptTokensOrig - fullLog.promptTokensOpt).toLocaleString()} in / +${(fullLog.completionTokensOrig - fullLog.completionTokensOpt).toLocaleString()} out`
                : `+${record.savedInput} in / +${record.savedOutput} out`}
            </div>
          </div>
        </div>

        {loading && (
          <div className="flex-1 flex items-center justify-center text-zinc-500 text-sm">
            Loading payloads{"\u2026"}
          </div>
        )}

        {error && (
          <div className="flex-1 flex items-center justify-center text-red-400 text-sm">
            Failed to load payload: {error}
          </div>
        )}

        {fullLog && !loading && !error && lineDiff && (
          <div className="flex-1 flex flex-col min-h-0">
            {/* Toolbar: view mode + stats legend + download full payloads */}
            <div className="mb-2 flex items-center justify-between gap-3">
              <div className="flex items-center gap-2 text-xs">
                <span className="px-2 py-0.5 rounded bg-emerald-500/15 text-emerald-300">
                  +{lineDiff.stats.added + lineDiff.stats.modified} additions
                </span>
                <span className="px-2 py-0.5 rounded bg-red-500/15 text-red-300">
                  −{lineDiff.stats.removed + lineDiff.stats.modified} deletions
                </span>
                <span className="px-2 py-0.5 rounded bg-zinc-700/40 text-zinc-400">
                  {lineDiff.stats.same} unchanged
                </span>
              </div>
              <div className="flex items-center gap-2">
                <a
                  href={`/api/telemetry/${record.id}/payload?field=originalPayload`}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="rounded border border-white/10 px-2 py-1 text-xs text-zinc-300 hover:bg-white/5"
                  title={
                    (fullLog as any).payloadsTruncated?.original
                      ? `Full payload is ${((fullLog as any).payloadsTruncated?.originalFullLength / 1024).toFixed(1)} KB \u2014 only the first 100 KB are shown above.`
                      : "Download full original payload"
                  }
                >
                  ⬇ Original
                  {(fullLog as any).payloadsTruncated?.original && (
                    <span className="ml-1 text-amber-400">
                      ({((fullLog as any).payloadsTruncated?.originalFullLength / 1024).toFixed(0)} KB)
                    </span>
                  )}
                </a>
                <a
                  href={`/api/telemetry/${record.id}/payload?field=optimizedPayload`}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="rounded border border-white/10 px-2 py-1 text-xs text-zinc-300 hover:bg-white/5"
                  title={
                    (fullLog as any).payloadsTruncated?.optimized
                      ? `Full payload is ${((fullLog as any).payloadsTruncated?.optimizedFullLength / 1024).toFixed(1)} KB \u2014 only the first 100 KB are shown above.`
                      : "Download full optimized payload"
                  }
                >
                  ⬇ Optimized
                  {(fullLog as any).payloadsTruncated?.optimized && (
                    <span className="ml-1 text-amber-400">
                      ({((fullLog as any).payloadsTruncated?.optimizedFullLength / 1024).toFixed(0)} KB)
                    </span>
                  )}
                </a>
                <div className="flex items-center gap-1 text-xs border-l border-white/10 pl-2">
                  <button
                    onClick={() => setViewMode("unified")}
                    className={`px-2 py-1 rounded ${
                      viewMode === "unified"
                        ? "bg-emerald-500/20 text-emerald-200"
                        : "text-zinc-400 hover:bg-white/5"
                    }`}
                  >
                    Unified
                  </button>
                  <button
                    onClick={() => setViewMode("split")}
                    className={`px-2 py-1 rounded ${
                      viewMode === "split"
                        ? "bg-emerald-500/20 text-emerald-200"
                        : "text-zinc-400 hover:bg-white/5"
                    }`}
                  >
                    Side-by-side
                  </button>
                </div>
              </div>
            </div>

            {viewMode === "unified" ? (
              <pre className="flex-1 overflow-auto whitespace-pre-wrap break-all rounded border border-white/5 bg-black/40 p-3 text-[11px] font-mono leading-relaxed">
                {lineDiff.rows.map((d, idx) => {
                  if (d.kind === "same") {
                    return (
                      <div key={idx} className="text-zinc-500 hover:bg-white/5">
                        <span className="inline-block w-8 text-right pr-2 text-zinc-600 select-none">
                          {" "}
                        </span>
                        <span className="inline-block w-4 text-zinc-600 select-none"> </span>
                        {d.aLine}
                      </div>
                    );
                  }
                  if (d.kind === "del") {
                    return (
                      <div key={idx} className="bg-red-500/15 text-red-200 -mx-3 px-3">
                        <span className="inline-block w-8 text-right pr-2 text-red-400/60 select-none">
                          −
                        </span>
                        <span className="inline-block w-4 text-red-400/60 select-none"></span>
                        {d.aLine}
                      </div>
                    );
                  }
                  if (d.kind === "add") {
                    return (
                      <div key={idx} className="bg-emerald-500/15 text-emerald-200 -mx-3 px-3">
                        <span className="inline-block w-8 text-right pr-2 text-emerald-400/60 select-none">
                          +
                        </span>
                        <span className="inline-block w-4 text-emerald-400/60 select-none"></span>
                        {d.bLine}
                      </div>
                    );
                  }
                  // mod: show BOTH old and new lines, with intra-line highlights
                  return (
                    <div key={idx}>
                      <div className="bg-red-500/15 text-red-200 -mx-3 px-3">
                        <span className="inline-block w-8 text-right pr-2 text-red-400/60 select-none">
                          −
                        </span>
                        <span className="inline-block w-4 text-red-400/60 select-none"></span>
                        {d.left.map((p, k) => (
                          <span
                            key={k}
                            className={p.removed ? "bg-red-500/40 text-white rounded-sm" : ""}
                          >
                            {p.text}
                          </span>
                        ))}
                      </div>
                      <div className="bg-emerald-500/15 text-emerald-200 -mx-3 px-3">
                        <span className="inline-block w-8 text-right pr-2 text-emerald-400/60 select-none">
                          +
                        </span>
                        <span className="inline-block w-4 text-emerald-400/60 select-none"></span>
                        {d.right.map((p, k) => (
                          <span
                            key={k}
                            className={p.added ? "bg-emerald-500/40 text-white rounded-sm" : ""}
                          >
                            {p.text}
                          </span>
                        ))}
                      </div>
                    </div>
                  );
                })}
              </pre>
            ) : (
              <div className="flex-1 grid grid-cols-1 md:grid-cols-2 gap-3 min-h-0">
                {/* Original (left) */}
                <div className="flex flex-col min-h-0">
                  <div className="mb-1 text-xs text-zinc-500 flex items-center justify-between">
                    <span>Original payload</span>
                    <span className="text-zinc-600">
                      {(fullLog.promptTokensOrig + fullLog.completionTokensOrig).toLocaleString()} tokens
                    </span>
                  </div>
                  <pre className="flex-1 overflow-auto whitespace-pre-wrap break-all rounded border border-white/5 bg-black/40 p-3 text-[11px] font-mono leading-relaxed">
                    {lineDiff.rows.map((d, idx) => {
                      if (d.kind === "add") {
                        return (
                          <div
                            key={idx}
                            className="text-zinc-700 select-none"
                            title="(removed in optimized)"
                          >
                            {"       "}
                          </div>
                        );
                      }
                      if (d.kind === "same") {
                        return (
                          <div key={idx} className="text-zinc-400">
                            <span className="inline-block w-10 text-right pr-2 text-zinc-600 select-none">
                              {idx + 1}
                            </span>
                            {d.aLine}
                          </div>
                        );
                      }
                      if (d.kind === "mod") {
                        return (
                          <div key={idx} className="bg-red-500/15 text-red-200 -mx-3 px-3">
                            <span className="inline-block w-10 text-right pr-2 text-red-400/60 select-none">
                              {idx + 1}
                            </span>
                            {d.left.map((p, k) => (
                              <span
                                key={k}
                                className={p.removed ? "bg-red-500/40 text-white rounded-sm" : ""}
                              >
                                {p.text}
                              </span>
                            ))}
                          </div>
                        );
                      }
                      return (
                        <div key={idx} className="bg-red-500/15 text-red-200 -mx-3 px-3">
                          <span className="inline-block w-10 text-right pr-2 text-red-400/60 select-none">
                            {idx + 1}
                          </span>
                          {d.aLine}
                        </div>
                      );
                    })}
                  </pre>
                </div>

                {/* Optimized (right) */}
                <div className="flex flex-col min-h-0">
                  <div className="mb-1 text-xs text-zinc-500 flex items-center justify-between">
                    <span>Optimized payload</span>
                    <span className="text-zinc-600">
                      {(fullLog.promptTokensOpt + fullLog.completionTokensOpt).toLocaleString()} tokens
                    </span>
                  </div>
                  <pre className="flex-1 overflow-auto whitespace-pre-wrap break-all rounded border border-white/5 bg-black/40 p-3 text-[11px] font-mono leading-relaxed">
                    {lineDiff.rows.map((d, idx) => {
                      if (d.kind === "del") {
                        return (
                          <div
                            key={idx}
                            className="text-zinc-700 select-none"
                            title="(added in optimized)"
                          >
                            {"       "}
                          </div>
                        );
                      }
                      if (d.kind === "same") {
                        return (
                          <div key={idx} className="text-zinc-400">
                            <span className="inline-block w-10 text-right pr-2 text-zinc-600 select-none">
                              {idx + 1}
                            </span>
                            {d.bLine}
                          </div>
                        );
                      }
                      if (d.kind === "mod") {
                        return (
                          <div key={idx} className="bg-emerald-500/15 text-emerald-200 -mx-3 px-3">
                            <span className="inline-block w-10 text-right pr-2 text-emerald-400/60 select-none">
                              {idx + 1}
                            </span>
                            {d.right.map((p, k) => (
                              <span
                                key={k}
                                className={p.added ? "bg-emerald-500/40 text-white rounded-sm" : ""}
                              >
                                {p.text}
                              </span>
                            ))}
                          </div>
                        );
                      }
                      return (
                        <div key={idx} className="bg-emerald-500/15 text-emerald-200 -mx-3 px-3">
                          <span className="inline-block w-10 text-right pr-2 text-emerald-400/60 select-none">
                            {idx + 1}
                          </span>
                          {d.bLine}
                        </div>
                      );
                    })}
                  </pre>
                </div>
              </div>
            )}
          </div>
        )}

        {fullLog?.responsePayload && (
          <details className="mt-3 text-xs text-zinc-400">
            <summary className="cursor-pointer hover:text-zinc-200">
              Show upstream response payload
            </summary>
            <pre className="mt-2 max-h-40 overflow-auto rounded border border-white/5 bg-black/40 p-3 text-[11px] text-zinc-300">
              {pretty(fullLog.responsePayload)}
            </pre>
          </details>
        )}

        <div className="mt-4 flex justify-end">
          <button
            onClick={onClose}
            className="rounded bg-emerald-500/20 px-3 py-1.5 text-xs text-emerald-300 hover:bg-emerald-500/30"
          >
            Close
          </button>
        </div>
      </div>
    </div>
  );
}

// --- toast stack -------------------------------------------------------

function ToastStack({ toasts }: { toasts: Array<{ id: string; label: string }> }) {
  if (toasts.length === 0) return null;
  return (
    <div className="pointer-events-none fixed bottom-6 right-6 z-50 flex flex-col gap-2">
      {toasts.map((t) => (
        <div
          key={t.id}
          className="pointer-events-auto animate-in fade-in slide-in-from-right rounded-lg border border-emerald-500/30 bg-emerald-500/10 px-4 py-2 text-sm text-emerald-200 shadow-2xl backdrop-blur"
          style={{ animation: "fadein 0.2s ease-out" }}
        >
          ✨ New agent: {t.label}
        </div>
      ))}
    </div>
  );
}
