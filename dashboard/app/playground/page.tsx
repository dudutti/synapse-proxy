"use client";

import { useSession } from "next-auth/react";
import { useRouter } from "next/navigation";
import { useEffect, useState, useRef } from "react";
import Link from "next/link";
import { motion } from "framer-motion";
import { ArrowLeft, Send, Sparkles, Activity, Zap, Database, X, Link2, Unlink, Download, BarChart3 } from "lucide-react";
import { toast } from "sonner";
import ParticleBackground from "@/components/ParticleBackground";
import { MessageStats, ComparisonBar, type BubbleStats } from "./components/MessageStats";
import { ArtifactRenderer } from "./components/Artifact";
import { Sparkline } from "./components/Sparkline";

interface ApiKey {
  id: string;
  virtualKey: string;
  provider: string;
  benchmarkMode: boolean;
  semanticTolerance: number;
  defaultModel?: string;
}

interface PanelSettings {
  keyId: string;
  model: string;
}

interface ChatMsg {
  role: "user" | "assistant";
  content: string;
  latency?: number | null;
  isCached?: boolean;
  isStreaming?: boolean;
  stats?: BubbleStats;
}

const DEFAULT_PANEL: PanelSettings = { keyId: "", model: "" };

function useAvailableModels(virtualKey: string, keys: ApiKey[]) {
  const [models, setModels] = useState<any[]>([]);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!virtualKey) {
      setModels([]);
      return;
    }
    let cancelled = false;
    setLoading(true);
    fetch("/api/models", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ virtualKey }),
    })
      .then((r) => r.json())
      .then((data) => {
        if (cancelled) return;
        setLoading(false);
        if (data?.models && Array.isArray(data.models) && data.models.length > 0) {
          setModels(data.models);
        } else {
          setModels([]);
        }
      })
      .catch(() => {
        if (cancelled) return;
        setLoading(false);
        setModels([]);
      });
    return () => {
      cancelled = true;
    };
  }, [virtualKey]);

  const fallbackModel = virtualKey
    ? keys.find((k) => k.virtualKey === virtualKey)?.defaultModel || ""
    : "";

  return { models, loading, fallbackModel };
}

function PanelHeader({
  label,
  color,
  icon,
  settings,
  setSettings,
  keys,
  bypass,
  setBypass,
  isControl,
}: {
  label: string;
  color: "emerald" | "gray";
  icon: React.ReactNode;
  settings: PanelSettings;
  setSettings: (s: PanelSettings) => void;
  keys: ApiKey[];
  bypass: boolean;
  setBypass: (b: boolean) => void;
  isControl: boolean;
}) {
  const { models, loading, fallbackModel } = useAvailableModels(settings.keyId, keys);

  useEffect(() => {
    if (!settings.keyId) {
      if (settings.model !== "") setSettings({ ...settings, model: "" });
      return;
    }
    if (models.length > 0) {
      const found = models.find((m) => m.id === settings.model);
      if (!found) {
        setSettings({ ...settings, model: models[0].id });
      }
    } else if (!loading && fallbackModel && settings.model !== fallbackModel) {
      setSettings({ ...settings, model: fallbackModel });
    } else if (!loading && !fallbackModel && settings.model !== "") {
      setSettings({ ...settings, model: "" });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [settings.keyId, models.length, loading, fallbackModel]);

  const accentText = color === "emerald" ? "text-emerald-400" : "text-gray-400";
  const accentBorder = color === "emerald" ? "border-emerald-500/30" : "border-gray-500/30";

  return (
    <div className="p-4 border-b border-white/5 bg-white/5 space-y-3">
      <div className="flex items-center justify-between">
        <h2 className={`text-sm font-bold ${accentText} flex items-center gap-2`}>
          {icon} {label}
        </h2>
        {isControl && (
          <label className="flex items-center gap-2 cursor-pointer bg-white/5 border border-white/10 px-2 py-1 rounded-lg transition hover:bg-white/10 text-[10px] text-gray-400">
            <input
              type="checkbox"
              checked={bypass}
              onChange={() => setBypass(!bypass)}
              className="accent-gray-400 w-3 h-3 cursor-pointer"
            />
            <span className="font-bold uppercase tracking-wider">Bypass cache</span>
          </label>
        )}
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <select
          value={settings.keyId}
          onChange={(e) => setSettings({ keyId: e.target.value, model: "" })}
          className={`bg-white/5 border border-white/10 ${accentText} text-xs rounded-lg px-2 py-1.5 outline-none font-mono focus:${accentBorder} transition w-44`}
        >
          <option value="" className="bg-gray-900 text-white">
            — Select a key —
          </option>
          {keys.map((k) => (
            <option key={k.id} value={k.virtualKey} className="bg-gray-900 text-white">
              {k.provider.toUpperCase()} ({k.virtualKey.substring(0, 15)}…)
            </option>
          ))}
        </select>

        {settings.keyId &&
          (models.length > 0 ? (
            <select
              value={settings.model}
              onChange={(e) => setSettings({ ...settings, model: e.target.value })}
              className="bg-white/5 border border-white/10 text-white text-xs rounded-lg px-2 py-1.5 outline-none focus:border-blue-500/50 transition w-44"
            >
              {models.map((m) => (
                <option key={m.id} value={m.id} className="bg-gray-900 text-white">
                  {m.name || m.id}
                </option>
              ))}
            </select>
          ) : (
            <input
              type="text"
              value={settings.model}
              onChange={(e) => setSettings({ ...settings, model: e.target.value })}
              placeholder={loading ? "Loading…" : "Model ID"}
              className="bg-white/5 border border-white/10 text-white text-xs rounded-lg px-2 py-1.5 outline-none focus:border-blue-500/50 transition w-44"
            />
          ))}
      </div>
    </div>
  );
}

const ChatBubble = ({ msg, isControl }: { msg: ChatMsg; isControl: boolean }) => (
  <motion.div
    initial={{ opacity: 0, y: 10 }}
    animate={{ opacity: 1, y: 0 }}
    className={`flex ${msg.role === 'user' ? 'justify-end' : 'justify-start'}`}
  >
    <div className={`max-w-[85%] rounded-2xl p-5 ${
      msg.role === 'user'
        ? 'bg-blue-600/20 border border-blue-500/30 text-white rounded-br-none backdrop-blur-md shadow-lg shadow-blue-500/10'
        : 'bg-white/5 border border-white/10 text-gray-200 rounded-bl-none backdrop-blur-md shadow-lg flex flex-col'
    }`}>
      {msg.role === 'assistant' && (
        <div className="flex items-center gap-3 mb-3 border-b border-white/5 pb-2">
          {isControl ? <Activity className="w-4 h-4 text-gray-400" /> : <Sparkles className="w-4 h-4 text-emerald-400" />}
          <span className="text-xs font-bold text-gray-400 uppercase tracking-widest">Assistant</span>

          <div className="ml-auto flex items-center gap-2">
            <span className={`text-xs font-mono ${isControl ? 'text-gray-500' : 'text-emerald-500/80'}`}>
              {msg.latency !== null && msg.latency !== undefined ? `${msg.latency}ms` : (msg.isStreaming ? '...' : 'Error')}
            </span>
            {msg.isCached ? (
              <span className="flex items-center gap-1 bg-emerald-500/20 border border-emerald-500/30 text-emerald-400 text-[10px] uppercase font-bold px-2 py-0.5 rounded-full shadow-[0_0_10px_rgba(16,185,129,0.2)]">
                <Zap className="w-3 h-3" /> Hit
              </span>
            ) : (
              <span className="bg-gray-800 text-gray-400 text-[10px] uppercase font-bold px-2 py-0.5 rounded-full border border-gray-700">
                API
              </span>
            )}
          </div>
        </div>
      )}
      {msg.role === 'assistant' ? (
        <ArtifactRenderer content={msg.content} />
      ) : (
        <div className="whitespace-pre-wrap text-sm leading-relaxed">
          {msg.content}
        </div>
      )}
      {msg.isStreaming && (
        <span className={`inline-block w-2 h-5 ml-0.5 animate-pulse rounded-sm ${isControl ? 'bg-gray-500' : 'bg-emerald-400'}`} />
      )}
      {msg.role === 'assistant' && msg.stats && <MessageStats stats={msg.stats} isControl={isControl} />}
    </div>
  </motion.div>
);

export default function PlaygroundPage() {
  const { status } = useSession();
  const router = useRouter();

  const [keys, setKeys] = useState<ApiKey[]>([]);
  const [sideBySide, setSideBySide] = useState(true);
  const [syncPanels, setSyncPanels] = useState(true);

  const [opti, setOpti] = useState<PanelSettings>(DEFAULT_PANEL);
  const [ctrl, setCtrl] = useState<PanelSettings>(DEFAULT_PANEL);
  const [ctrlBypass, setCtrlBypass] = useState(true);

  const [prompt, setPrompt] = useState("");

  const [messagesOpti, setMessagesOpti] = useState<ChatMsg[]>([]);
  const [messagesCtrl, setMessagesCtrl] = useState<ChatMsg[]>([]);
  const [isTyping, setIsTyping] = useState(false);

  // Sparkline history: one entry per assistant message per panel.
  const [latencyHistoryOpti, setLatencyHistoryOpti] = useState<number[]>([]);
  const [latencyHistoryCtrl, setLatencyHistoryCtrl] = useState<number[]>([]);
  const [savingsHistory, setSavingsHistory] = useState<number[]>([]);

  const messagesEndRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (status === "unauthenticated") {
      router.push("/login");
    } else if (status === "authenticated") {
      fetch("/api/keys")
        .then((res) => res.json())
        .then((data) => {
          if (Array.isArray(data)) {
            setKeys(data);
            if (data.length > 0) {
              const first = data[0].virtualKey;
              setOpti({ keyId: first, model: data[0].defaultModel || "" });
              setCtrl({ keyId: first, model: data[0].defaultModel || "" });
            }
          }
        });
    }
  }, [status, router]);

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messagesOpti, messagesCtrl, isTyping]);

  const streamChat = async (
    userMsg: string,
    settings: PanelSettings,
    bypass: boolean,
    setMsgs: (updater: (prev: ChatMsg[]) => ChatMsg[]) => void,
    onStats: (stats: BubbleStats) => void
  ) => {
    if (!settings.keyId) {
      setMsgs((prev) => [
        ...prev,
        { role: "assistant", content: "Select a key first.", isStreaming: false },
      ]);
      return;
    }

    const startTime = Date.now();
    setMsgs((prev) => [
      ...prev,
      { role: "assistant", content: "", isCached: false, isStreaming: true },
    ]);

    try {
      const res = await fetch("/api/playground/chat", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          virtualKey: settings.keyId,
          model: settings.model,
          messages: [{ role: "user", content: userMsg }],
          stream: true,
          bypass,
        }),
      });

      const latencyHeader = res.headers.get("X-Latency-Ms");
      const contentType = res.headers.get("Content-Type") || "";

      if (contentType.includes("text/event-stream") && res.body) {
        const reader = res.body.getReader();
        const decoder = new TextDecoder();
        let buffer = "";
        let fullContent = "";

        while (true) {
          const { done, value } = await reader.read();
          if (done) break;

          buffer += decoder.decode(value, { stream: true });
          const lines = buffer.split("\n");
          buffer = lines.pop() || "";

          let i = 0;
          while (i < lines.length) {
            const trimmed = lines[i].trim();
            // SSE event: "event: stats\ndata: {...}"
            if (trimmed.startsWith("event: stats")) {
              // Next line should be "data: {json}"
              const dataLine = lines[i + 1]?.trim() || "";
              if (dataLine.startsWith("data: ")) {
                try {
                  const stats = JSON.parse(dataLine.slice(6));
                  onStats(stats);
                  const latency = stats.latencyMs ?? parseInt(latencyHeader || "0");
                  setMsgs((prev) => {
                    const next = [...prev];
                    next[next.length - 1] = {
                      ...next[next.length - 1],
                      latency,
                      isCached: !bypass && latency < 150,
                      isStreaming: false,
                      stats: {
                        cacheLevel: stats.cacheLevel || "NONE",
                        tokensIn: stats.tokensIn || 0,
                        tokensOut: stats.tokensOut || 0,
                        costSaved: stats.costSaved || 0,
                        costWithout: stats.costWithout || 0,
                        costWith: stats.costWith || 0,
                        latencyMs: latency,
                      },
                    };
                    return next;
                  });
                } catch {}
                i += 2;
                continue;
              }
            }
            if (trimmed.startsWith("data: ")) {
              const data = trimmed.slice(6);
              if (data !== "[DONE]") {
                try {
                  const chunk = JSON.parse(data);
                  if (chunk.choices?.[0]?.delta?.content) {
                    fullContent += chunk.choices[0].delta.content;
                    setMsgs((prev) => {
                      const next = [...prev];
                      next[next.length - 1] = { ...next[next.length - 1], content: fullContent };
                      return next;
                    });
                  }
                } catch {}
              }
            }
            i++;
          }
        }

        // If no stats event arrived (e.g. bypass returned upstream error),
        // finalize with whatever latency we measured client-side.
        setMsgs((prev) => {
          const last = prev[prev.length - 1];
          if (last?.isStreaming) {
            const latency = parseInt(latencyHeader || "0") || Date.now() - startTime;
            return [
              ...prev.slice(0, -1),
              { ...last, latency, isCached: !bypass && latency < 150, isStreaming: false },
            ];
          }
          return prev;
        });
      } else {
        const data = await res.json();
        const latency = latencyHeader ? parseInt(latencyHeader) : Date.now() - startTime;
        const stats: BubbleStats = data.stats || {};

        let extractedContent = "Error";
        if (data.response) {
          try {
            const parsed = JSON.parse(data.response);
            if (parsed.choices?.[0]?.message?.content) {
              extractedContent = parsed.choices[0].message.content;
            } else if (parsed.base_resp?.status_msg) {
              extractedContent = `Minimax API Error: ${parsed.base_resp.status_msg}`;
            } else if (parsed.error?.message) {
              extractedContent = `API Error: ${parsed.error.message}`;
            } else {
              extractedContent = `Unexpected API Response:\n${JSON.stringify(parsed, null, 2)}`;
            }
          } catch {
            extractedContent = "Failed to parse API response";
          }
        }

        onStats({ ...stats, latencyMs: latency });
        setMsgs((prev) => {
          const next = [...prev];
          next[next.length - 1] = {
            ...next[next.length - 1],
            content: extractedContent,
            latency,
            isCached: !bypass && latency < 150,
            isStreaming: false,
            stats: { ...stats, latencyMs: latency },
          };
          return next;
        });
      }
    } catch {
      setMsgs((prev) => {
        const next = [...prev];
        next[next.length - 1] = {
          ...next[next.length - 1],
          content: "Error fetching response.",
          isStreaming: false,
        };
        return next;
      });
    }
  };

  const handleSend = async (e?: React.FormEvent) => {
    e?.preventDefault();
    if (!prompt.trim()) return;
    if (!opti.keyId && !ctrl.keyId) return;

    const userMsg = prompt;
    setPrompt("");

    setMessagesOpti((prev) => [...prev, { role: "user", content: userMsg }]);
    if (sideBySide) setMessagesCtrl((prev) => [...prev, { role: "user", content: userMsg }]);

    setIsTyping(true);

    const promises: Promise<void>[] = [];
    if (opti.keyId) {
      promises.push(streamChat(
        userMsg, opti, false, setMessagesOpti,
        (stats) => {
          setLatencyHistoryOpti((prev) => [...prev.slice(-49), stats.latencyMs || 0]);
          setSavingsHistory((prev) => [...prev.slice(-49), stats.costSaved || 0]);
        }
      ));
    }
    if (sideBySide && ctrl.keyId) {
      promises.push(streamChat(
        userMsg, ctrl, ctrlBypass, setMessagesCtrl,
        (stats) => setLatencyHistoryCtrl((prev) => [...prev.slice(-49), stats.latencyMs || 0])
      ));
    }

    await Promise.all(promises);
    setIsTyping(false);
  };

  const handleClear = () => {
    setMessagesOpti([]);
    setMessagesCtrl([]);
    setLatencyHistoryOpti([]);
    setLatencyHistoryCtrl([]);
    setSavingsHistory([]);
  };

  const handleExport = () => {
    const session = {
      timestamp: new Date().toISOString(),
      settings: { opti, ctrl, ctrlBypass, sideBySide, syncPanels },
      messages: { opti: messagesOpti, ctrl: messagesCtrl },
      sparklines: { latencyOpti: latencyHistoryOpti, latencyCtrl: latencyHistoryCtrl, savings: savingsHistory },
    };
    const blob = new Blob([JSON.stringify(session, null, 2)], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `playground-session-${Date.now()}.json`;
    a.click();
    URL.revokeObjectURL(url);
    toast.success("Session exported as JSON");
  };

  // Last assistant stats per panel — used to power the comparison bar
  // between the two chat threads.
  const lastOptiStats = [...messagesOpti].reverse().find((m) => m.role === "assistant" && m.stats)?.stats || null;
  const lastCtrlStats = [...messagesCtrl].reverse().find((m) => m.role === "assistant" && m.stats)?.stats || null;

  const effectiveCtrl = syncPanels ? opti : ctrl;
  const setEffectiveCtrl = (s: PanelSettings) => {
    if (syncPanels) setOpti(s);
    else setCtrl(s);
  };

  if (status === "loading") return <div className="min-h-screen bg-[#050505]" />;

  return (
    <div className="min-h-screen bg-[#050505] text-white font-sans flex flex-col relative overflow-hidden">
      <ParticleBackground />

      {/* Header */}
      <header className="p-6 border-b border-white/10 bg-black/40 backdrop-blur-xl relative z-10 flex justify-between items-center shrink-0">
        <div className="flex items-center gap-4">
          <div className="flex items-center gap-3">
            <div className="w-10 h-10 rounded-xl bg-gradient-to-br from-blue-500 to-indigo-600 flex items-center justify-center shadow-lg shadow-blue-500/20">
              <Database className="w-5 h-5 text-white" />
            </div>
            <div>
              <h1 className="text-xl font-bold tracking-tight text-white">Playground</h1>
              <p className="text-gray-400 text-xs">A/B test any (key, model) combo: Synapse Proxy vs Direct</p>
            </div>
          </div>
        </div>

        <div className="flex gap-3 items-center">
          <button
            type="button"
            onClick={handleExport}
            disabled={messagesOpti.length === 0 && messagesCtrl.length === 0}
            className="flex items-center gap-1 px-3 py-1.5 rounded-lg border border-white/10 bg-white/5 text-gray-300 hover:bg-white/10 text-xs font-bold transition disabled:opacity-30 disabled:cursor-not-allowed"
            title="Export session (settings + messages + sparklines) as JSON"
          >
            <Download className="w-3 h-3" />
            Export
          </button>

          <button
            type="button"
            onClick={handleClear}
            disabled={messagesOpti.length === 0 && messagesCtrl.length === 0}
            className="flex items-center gap-1 px-3 py-1.5 rounded-lg border border-white/10 bg-white/5 text-gray-300 hover:bg-white/10 text-xs font-bold transition disabled:opacity-30 disabled:cursor-not-allowed"
            title="Clear chat history and sparklines"
          >
            Clear
          </button>

          <label
            className={`flex items-center gap-2 cursor-pointer px-3 py-1.5 rounded-lg transition border text-xs font-bold ${
              sideBySide
                ? "bg-emerald-500/10 border-emerald-500/30 text-emerald-300"
                : "bg-white/5 border-white/10 text-gray-400"
            }`}
          >
            <input
              type="checkbox"
              checked={sideBySide}
              onChange={() => setSideBySide(!sideBySide)}
              className="accent-emerald-500 w-4 h-4 cursor-pointer"
            />
            Side-by-Side A/B
          </label>

          <button
            type="button"
            onClick={() => setSyncPanels(!syncPanels)}
            title={syncPanels ? "Right panel mirrors left panel — click to unlink" : "Panels are independent — click to link"}
            className={`flex items-center gap-2 px-3 py-1.5 rounded-lg border text-xs font-bold transition ${
              syncPanels
                ? "bg-blue-500/10 border-blue-500/30 text-blue-300"
                : "bg-amber-500/10 border-amber-500/30 text-amber-300"
            }`}
          >
            {syncPanels ? <Link2 className="w-3 h-3" /> : <Unlink className="w-3 h-3" />}
            {syncPanels ? "Linked" : "Independent"}
          </button>

          <Link
            href="/"
            className="ml-2 flex items-center gap-2 px-4 py-2 bg-red-500/10 text-red-400 hover:bg-red-500/20 hover:text-red-300 transition rounded-lg text-sm font-bold border border-red-500/20"
          >
            <X className="w-4 h-4" /> Close
          </Link>
        </div>
      </header>

      {/* Sparklines strip */}
      {(latencyHistoryOpti.length > 0 || latencyHistoryCtrl.length > 0) && (
        <div className="px-6 py-3 border-b border-white/5 bg-black/20 flex flex-wrap items-center gap-6 text-xs">
          <div className="flex items-center gap-2">
            <BarChart3 className="w-3 h-3 text-emerald-400" />
            <span className="text-gray-400 uppercase tracking-wider text-[10px]">Sparklines</span>
          </div>
          {latencyHistoryOpti.length > 0 && (
            <div className="flex items-center gap-2">
              <span className="text-emerald-400 font-bold">Opti latency</span>
              <Sparkline values={latencyHistoryOpti} color="#34d399" />
              <span className="text-gray-500 font-mono">{latencyHistoryOpti[latencyHistoryOpti.length - 1]}ms</span>
            </div>
          )}
          {latencyHistoryCtrl.length > 0 && (
            <div className="flex items-center gap-2">
              <span className="text-gray-400 font-bold">Direct latency</span>
              <Sparkline values={latencyHistoryCtrl} color="#9ca3af" />
              <span className="text-gray-500 font-mono">{latencyHistoryCtrl[latencyHistoryCtrl.length - 1]}ms</span>
            </div>
          )}
          {savingsHistory.length > 0 && (
            <div className="flex items-center gap-2">
              <span className="text-emerald-400 font-bold">$ saved</span>
              <Sparkline values={savingsHistory} color="#10b981" />
              <span className="text-emerald-300 font-mono">${savingsHistory[savingsHistory.length - 1].toFixed(5)}</span>
            </div>
          )}
        </div>
      )}

      {/* A vs B comparison bar */}
      {sideBySide && lastOptiStats && lastCtrlStats && (
        <div className="px-6 py-2 border-b border-white/5 bg-black/20">
          <ComparisonBar optiStats={lastOptiStats} ctrlStats={lastCtrlStats} bypass={ctrlBypass} />
        </div>
      )}

      {/* Chat Area */}
      <main className="flex-1 overflow-hidden relative z-10 flex p-6 gap-6">
        {/* Synapse Proxy Panel (left) — always optimized */}
        <div className={`flex flex-col flex-1 bg-black/20 border border-white/5 rounded-3xl overflow-hidden transition-all duration-500 ${!sideBySide && "max-w-4xl mx-auto"}`}>
          <PanelHeader
            label="Synapse Proxy (Optimized)"
            color="emerald"
            icon={<Sparkles className="w-4 h-4" />}
            settings={opti}
            setSettings={setOpti}
            keys={keys}
            bypass={false}
            setBypass={() => {}}
            isControl={false}
          />

          <div className="flex-1 overflow-y-auto p-6 scroll-smooth space-y-6">
            {messagesOpti.length === 0 && (
              <div className="h-full flex flex-col items-center justify-center text-center opacity-50">
                <Database className="w-12 h-12 mb-4 text-emerald-500/50" />
                <h3 className="text-lg font-bold text-gray-300">Synapse Proxy</h3>
                <p className="text-xs text-gray-500 mt-2 max-w-sm">
                  First request hits the API. Subsequent similar requests hit the L1 or L2 cache with 0 costs.
                </p>
              </div>
            )}
            {messagesOpti.map((msg, idx) => (
              <ChatBubble key={idx} msg={msg} isControl={false} />
            ))}
            <div ref={messagesEndRef} />
          </div>
        </div>

        {/* Control Panel (right) */}
        {sideBySide && (
          <div className="flex flex-col flex-1 bg-black/20 border border-white/5 rounded-3xl overflow-hidden transition-all duration-500">
            <PanelHeader
              label="Direct API (Control)"
              color="gray"
              icon={<Activity className="w-4 h-4" />}
              settings={effectiveCtrl}
              setSettings={setEffectiveCtrl}
              keys={keys}
              bypass={ctrlBypass}
              setBypass={setCtrlBypass}
              isControl={true}
            />

            <div className="flex-1 overflow-y-auto p-6 scroll-smooth space-y-6">
              {messagesCtrl.length === 0 && (
                <div className="h-full flex flex-col items-center justify-center text-center opacity-50">
                  <Activity className="w-12 h-12 mb-4 text-gray-500" />
                  <h3 className="text-lg font-bold text-gray-300">Direct Connection</h3>
                  <p className="text-xs text-gray-500 mt-2 max-w-sm">
                    Bypasses the proxy optimizations. Same key+model as the left panel for a true A/B, or unlink the panels to compare two different providers or models.
                  </p>
                </div>
              )}
              {messagesCtrl.map((msg, idx) => (
                <ChatBubble key={idx} msg={msg} isControl={true} />
              ))}
              <div ref={messagesEndRef} />
            </div>
          </div>
        )}
      </main>

      {/* Input Area */}
      <footer className="p-6 border-t border-white/10 bg-[#050505]/80 backdrop-blur-xl relative z-10 shrink-0">
        <form onSubmit={handleSend} className="max-w-4xl mx-auto relative">
          <textarea
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                handleSend();
              }
            }}
            placeholder={keys.length === 0 ? "Generate an API Key in settings first..." : "Send a prompt to test the cache..."}
            disabled={keys.length === 0 || isTyping}
            className="w-full bg-white/5 border border-white/10 rounded-2xl py-4 pl-6 pr-16 text-white placeholder-gray-500 focus:outline-none focus:border-blue-500/50 focus:bg-white/10 transition-all resize-none overflow-hidden min-h-[60px]"
            rows={1}
            style={{
              height: prompt.split("\n").length > 1 ? `${Math.min(prompt.split("\n").length * 24 + 40, 200)}px` : "60px",
            }}
          />
          <button
            type="submit"
            disabled={!prompt.trim() || keys.length === 0 || isTyping || (!opti.keyId && !ctrl.keyId)}
            className="absolute right-3 bottom-3 p-2 bg-gradient-to-r from-blue-600 to-indigo-600 text-white rounded-xl hover:from-blue-500 hover:to-indigo-500 disabled:opacity-50 disabled:cursor-not-allowed transition shadow-lg"
          >
            <Send className="w-5 h-5" />
          </button>
        </form>
        <p className="text-center text-xs text-gray-600 mt-4">
          {syncPanels
            ? "Linked panels — same key+model, Synapse Proxy vs Direct."
            : "Independent panels — compare two different keys or models."}
        </p>
      </footer>
    </div>
  );
}
