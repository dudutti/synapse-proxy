"use client";

import React, { useEffect, useState, memo, useRef, useMemo } from "react";
import dynamic from "next/dynamic";
import { Server, Database, Brain, Sparkles, Code2, GitCompare, Activity } from "lucide-react";
import { PerHookBreakdown } from "./PerHookBreakdown";

const Globe = dynamic(() => import("./GlobeWrapper"), { ssr: false });

const formatNumber = (num: number) => new Intl.NumberFormat('en-US').format(num);
const formatCurrency = (num: number) => new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(num);

type TelemetryData = {
  stats: {
    totalRequests: number;
    totalTokensPassed: number;
    totalTokensSaved: number;
    totalCostSaved: number;
    distribution: {
      MISS: number;
      L1: number;
      L2: number;
      L3: number;
    };
    hookSavings?: {
      logCompressor: { bytes: number; tokens: number; count: number };
      outputReducer: { bytes: number; tokens: number; count: number };
      ccrCache: { hits: number; bytes: number };
      tagProtector: { zones: number };
      synapseRetrieve: { toolsInjected: number };
    };
  };
  markers: Array<{ location: [number, number]; size: number; color: [number, number, number] }>;
  arcs?: Array<{ startLat: number; startLng: number; endLat: number; endLng: number; color: string; dashAnimateTime: number }>;
};

const GLOBAL_CITIES = [
  { lat: 40.7128, lng: -74.0060 }, // New York
  { lat: 34.0522, lng: -118.2437 }, // Los Angeles
  { lat: 51.5074, lng: -0.1278 }, // London
  { lat: 48.8566, lng: 2.3522 }, // Paris
  { lat: 35.6762, lng: 139.6503 }, // Tokyo
  { lat: 1.3521, lng: 103.8198 }, // Singapore
  { lat: -33.8688, lng: 151.2093 }, // Sydney
  { lat: 37.7749, lng: -122.4194 }, // San Francisco
  { lat: 52.5200, lng: 13.4050 }, // Berlin
  { lat: 19.0760, lng: 72.8777 }, // Mumbai
  { lat: -23.5505, lng: -46.6333 }, // Sao Paulo
  { lat: 55.7558, lng: 37.6173 }, // Moscow
  { lat: 25.2048, lng: 55.2708 }, // Dubai
  { lat: -34.6037, lng: -58.3816 }, // Buenos Aires
  { lat: -1.2921, lng: 36.8219 }, // Nairobi
];

function getRandomCity(seed: string) {
  let hash = 0;
  for (let i = 0; i < seed.length; i++) {
    hash = seed.charCodeAt(i) + ((hash << 5) - hash);
  }
  const idx = Math.abs(hash) % GLOBAL_CITIES.length;
  return GLOBAL_CITIES[idx];
}

const proxyCoords = { lat: 50.1109, lng: 8.6821 }; // Frankfurt
const providerCoords: Record<string, { lat: number, lng: number }> = {
  openai: { lat: 41.8781, lng: -87.6298 }, // Chicago
  anthropic: { lat: 37.4316, lng: -78.6569 }, // N. Virginia
  google: { lat: 41.2619, lng: -95.8608 }, // Iowa
  minimax: { lat: 1.3521, lng: 103.8198 }, // Singapore
  deepseek: { lat: 39.9042, lng: 116.4074 }, // Beijing
};

// Memoized Globe component supporting markers and arcs
const GlobeCanvas = memo(({ markers, arcs = [] }: { markers: TelemetryData["markers"]; arcs?: any[] }) => {
  const globeRef = useRef<any>();
  const [dimensions, setDimensions] = useState({ width: 400, height: 400 });
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (globeRef.current) {
      globeRef.current.controls().autoRotate = true;
      globeRef.current.controls().autoRotateSpeed = 1.0;
      globeRef.current.controls().enableZoom = false;
    }

    const updateSize = () => {
      if (containerRef.current) {
        setDimensions({
          width: containerRef.current.clientWidth,
          height: containerRef.current.clientHeight
        });
      }
    };
    
    updateSize();
    window.addEventListener('resize', updateSize);
    return () => window.removeEventListener('resize', updateSize);
  }, []);

  const ringsData = markers.map(m => ({
    lat: m.location[0],
    lng: m.location[1],
    maxR: m.size * 80,
    propagationSpeed: 2,
    repeatPeriod: 1000 + Math.random() * 1000,
    color: `rgb(${Math.round(m.color[0]*255)}, ${Math.round(m.color[1]*255)}, ${Math.round(m.color[2]*255)})`
  }));

  return (
    <div className="relative w-[300px] h-[300px] xl:w-[400px] xl:h-[400px] flex-shrink-0" ref={containerRef}>
      <Globe
        innerRef={globeRef}
        width={dimensions.width}
        height={dimensions.height}
        backgroundColor="rgba(0,0,0,0)"
        globeImageUrl="//unpkg.com/three-globe/example/img/earth-dark.jpg"
        ringsData={ringsData}
        ringColor="color"
        ringMaxRadius="maxR"
        ringPropagationSpeed="propagationSpeed"
        ringRepeatPeriod="repeatPeriod"
        arcsData={arcs}
        arcColor="color"
        arcDashLength={0.4}
        arcDashGap={4}
        arcDashAnimateTime="dashAnimateTime"
        arcAltitude={0.25}
        arcStroke={1.5}
      />
      <div className="absolute inset-0 bg-[radial-gradient(circle_at_center,transparent_50%,rgba(0,0,0,0.8)_100%)] pointer-events-none z-10" />
    </div>
  );
});

GlobeCanvas.displayName = "GlobeCanvas";

type HookState = "idle" | "active" | "hit" | "miss" | "compressed";

export function GlobalCommandCenter() {
  const [data, setData] = useState<TelemetryData | null>(null);
  const [liveArcs, setLiveArcs] = useState<any[]>([]);
  const [hookStates, setHookStates] = useState<Record<string, HookState>>({
    L1: "idle",
    L2: "idle",
    L3: "idle",
    SmartCrusher: "idle",
    DiffCompressor: "idle",
    ASTCompactor: "idle",
  });
  const [activeReqId, setActiveReqId] = useState<string | null>(null);

  // Fetch initial telemetry
  useEffect(() => {
    const fetchTelemetry = async () => {
      try {
        const res = await fetch("/api/admin/telemetry");
        if (res.ok) {
          const json = await res.json();
          setData(json);
          if (json.arcs) {
            setLiveArcs(json.arcs);
          }
        }
      } catch (err) {
        console.error("Failed to fetch global telemetry", err);
      }
    };
    
    fetchTelemetry();
    const interval = setInterval(fetchTelemetry, 15000);
    return () => clearInterval(interval);
  }, []);

  // Listen to live SSE logs stream
  useEffect(() => {
    const eventSource = new EventSource("/api/admin/logs/stream");

    eventSource.onmessage = (event) => {
      try {
        const log = JSON.parse(event.data);
        if (log.at) return; // connected ping

        // 1. Update stats instantly
        setData(prev => {
          if (!prev) return null;
          const isL1 = log.cacheLevel === "L1";
          const isL2 = log.cacheLevel === "L2";
          const isL3 = log.cacheLevel === "L3";
          const isHit = isL1 || isL2 || isL3;

          const savedIn = log.tokensIn - log.tokensInOpt;
          const savedOut = log.tokensOut - log.tokensOutOpt;
          const totalSaved = Math.max(0, savedIn + savedOut);

          return {
            stats: {
              totalRequests: prev.stats.totalRequests + 1,
              totalTokensPassed: prev.stats.totalTokensPassed + log.tokensIn + log.tokensOut,
              totalTokensSaved: prev.stats.totalTokensSaved + totalSaved,
              totalCostSaved: prev.stats.totalCostSaved + (log.costSaved || 0),
              distribution: {
                MISS: prev.stats.distribution.MISS + (!isHit ? 1 : 0),
                L1: prev.stats.distribution.L1 + (isL1 ? 1 : 0),
                L2: prev.stats.distribution.L2 + (isL2 ? 1 : 0),
                L3: prev.stats.distribution.L3 + (isL3 ? 1 : 0),
              },
              hookSavings: prev.stats.hookSavings
            },
            markers: prev.markers
          };
        });

        // 2. Animate Hook Matrix
        runHookAnimation(log);

        // 3. Add dynamic shooting arc to the globe
        const city = getRandomCity(log.id);
        const isHit = log.cacheLevel === "L1" || log.cacheLevel === "L2" || log.cacheLevel === "L3";

        const newArc1 = {
          startLat: city.lat,
          startLng: city.lng,
          endLat: proxyCoords.lat,
          endLng: proxyCoords.lng,
          color: log.cacheLevel === "L3" ? "#a855f7" : (isHit ? "#10b981" : "#ef4444"),
          dashAnimateTime: isHit ? 1000 : 2500,
          createdAt: Date.now()
        };

        setLiveArcs(prev => [...prev, newArc1]);

        if (!isHit) {
          const prov = (log.provider || "").toLowerCase();
          const coords = providerCoords[prov] || { lat: 37.7749, lng: -122.4194 };
          const newArc2 = {
            startLat: proxyCoords.lat,
            startLng: proxyCoords.lng,
            endLat: coords.lat,
            endLng: coords.lng,
            color: "#f59e0b",
            dashAnimateTime: 2500,
            createdAt: Date.now()
          };
          setLiveArcs(prev => [...prev, newArc2]);
        }

      } catch (err) {
        console.error("SSE live log parse error", err);
      }
    };

    return () => {
      eventSource.close();
    };
  }, []);

  // Cleanup old arcs after 4s to avoid crowding the globe
  useEffect(() => {
    const timer = setInterval(() => {
      const cutoff = Date.now() - 4000;
      setLiveArcs(prev => prev.filter(arc => !arc.createdAt || arc.createdAt > cutoff));
    }, 2000);
    return () => clearInterval(timer);
  }, []);

  const runHookAnimation = (log: any) => {
    const cacheLevel = log.cacheLevel;
    let hooksInfo: any = {};
    if (log.perHookSavings) {
      try {
        hooksInfo = typeof log.perHookSavings === "string" ? JSON.parse(log.perHookSavings) : log.perHookSavings;
      } catch {}
    }

    setActiveReqId(log.id);

    // Initial resets
    setHookStates({
      L1: "idle",
      L2: "idle",
      L3: "idle",
      SmartCrusher: "idle",
      DiffCompressor: "idle",
      ASTCompactor: "idle",
    });

    // Sequential cascade
    // L1
    setTimeout(() => {
      setHookStates(prev => ({ ...prev, L1: "active" }));
      setTimeout(() => {
        if (cacheLevel === "L1") {
          setHookStates(prev => ({ ...prev, L1: "hit" }));
          return;
        }
        setHookStates(prev => ({ ...prev, L1: "miss" }));

        // L2
        setTimeout(() => {
          setHookStates(prev => ({ ...prev, L2: "active" }));
          setTimeout(() => {
            if (cacheLevel === "L2") {
              setHookStates(prev => ({ ...prev, L2: "hit" }));
              return;
            }
            setHookStates(prev => ({ ...prev, L2: "miss" }));

            // L3
            setTimeout(() => {
              setHookStates(prev => ({ ...prev, L3: "active" }));
              setTimeout(() => {
                if (cacheLevel === "L3") {
                  setHookStates(prev => ({ ...prev, L3: "hit" }));
                  return;
                }
                setHookStates(prev => ({ ...prev, L3: "miss" }));

                // Compressors
                const hasCrusher = hooksInfo.logCompressor && hooksInfo.logCompressor.compressions > 0;
                const hasDiff = hooksInfo.diffCompressor && hooksInfo.diffCompressor.bytesSaved > 0;
                const hasAST = hooksInfo.astCodeCompressor && hooksInfo.astCodeCompressor.bytesSaved > 0;

                if (hasCrusher || hasDiff || hasAST) {
                  setTimeout(() => {
                    if (hasCrusher) setHookStates(prev => ({ ...prev, SmartCrusher: "active" }));
                    if (hasDiff) setHookStates(prev => ({ ...prev, DiffCompressor: "active" }));
                    if (hasAST) setHookStates(prev => ({ ...prev, ASTCompactor: "active" }));

                    setTimeout(() => {
                      if (hasCrusher) setHookStates(prev => ({ ...prev, SmartCrusher: "compressed" }));
                      if (hasDiff) setHookStates(prev => ({ ...prev, DiffCompressor: "compressed" }));
                      if (hasAST) setHookStates(prev => ({ ...prev, ASTCompactor: "compressed" }));
                    }, 300);
                  }, 100);
                }
              }, 150);
            }, 100);
          }, 150);
        }, 100);
      }, 150);
    }, 50);
  };

  if (!data) {
    return <div className="h-[600px] flex items-center justify-center text-gray-500 font-mono animate-pulse">Establishing Satellite Uplink...</div>;
  }

  const { stats } = data;

  return (
    <div className="space-y-6 w-full">
      {/* HUD metrics dashboard */}
      <div className="relative w-full rounded-3xl overflow-hidden bg-black/60 border border-white/10 p-8 flex flex-col xl:flex-row items-center justify-between gap-8">
        {/* LEFT STATS HUD */}
        <div className="flex-1 space-y-8 z-10">
          <div>
            <h2 className="text-emerald-400 text-xs font-black tracking-[0.2em] uppercase mb-1 drop-shadow-[0_0_8px_rgba(52,211,153,0.5)]">
              Global Operations
            </h2>
            <div className="text-5xl font-black text-white tracking-tight">
              {formatNumber(stats.totalRequests)}
            </div>
            <div className="text-gray-400 text-sm mt-1">Total API Requests Processed</div>
          </div>

          <div className="space-y-4">
            <ProgressBar label="L1 (Exact)" count={stats.distribution.L1} total={stats.totalRequests} color="bg-emerald-400" />
            <ProgressBar label="L2 (Semantic)" count={stats.distribution.L2} total={stats.totalRequests} color="bg-emerald-400" />
            <ProgressBar label="L3 (Compression)" count={stats.distribution.L3} total={stats.totalRequests} color="bg-purple-500" />
          </div>
        </div>

        {/* PER-HOOK BREAKDOWN */}
        <PerHookBreakdown data={data.stats.hookSavings} />

        {/* GLOBE CANVAS */}
        <GlobeCanvas markers={data.markers} arcs={liveArcs} />

        {/* RIGHT STATS HUD */}
        <div className="flex-1 space-y-8 z-10 text-right">
          <div>
            <h2 className="text-amber-400 text-xs font-black tracking-[0.2em] uppercase mb-1 drop-shadow-[0_0_8px_rgba(251,191,36,0.5)]">
              Total Wealth Preserved
            </h2>
            <div className="text-6xl font-black text-amber-400 drop-shadow-[0_0_15px_rgba(251,191,36,0.3)] transition-all">
              {formatCurrency(stats.totalCostSaved)}
            </div>
            <div className="text-gray-400 text-sm mt-1">Dollars Saved Globally</div>
          </div>

          <div className="pt-6 border-t border-white/10">
            <div className="text-3xl font-black text-white">{formatNumber(stats.totalTokensSaved)}</div>
            <div className="text-gray-400 text-xs font-bold uppercase tracking-widest mt-1">Tokens Purged</div>
          </div>
          
          <div>
            <div className="text-xl font-bold text-gray-500">{formatNumber(stats.totalTokensPassed)}</div>
            <div className="text-gray-600 text-[10px] font-bold uppercase tracking-widest mt-1">Tokens Sent (Unoptimized)</div>
          </div>
        </div>
      </div>

      {/* NEW NEON HOOK MATRIX */}
      <div className="w-full bg-black/60 border border-white/10 rounded-3xl p-6 space-y-4">
        <div className="flex items-center justify-between border-b border-white/5 pb-3">
          <div className="flex items-center gap-2">
            <Activity className="w-4 h-4 text-cyan-400 animate-pulse" />
            <h3 className="text-xs font-black uppercase tracking-widest text-zinc-300">
              Real-Time Hook Execution Matrix
            </h3>
          </div>
          <span className="text-[10px] font-mono text-zinc-500">
            {activeReqId ? `Active payload: ${activeReqId.slice(0, 12)}...` : "Listening for events..."}
          </span>
        </div>

        <div className="grid grid-cols-2 md:grid-cols-6 gap-3">
          <HookMatrixCard id="L1" label="L1 Cache" icon={<Server className="w-4 h-4" />} desc="Exact in-memory match" state={hookStates.L1} />
          <HookMatrixCard id="L2" label="L2 Cache" icon={<Brain className="w-4 h-4" />} desc="Semantic vector VSS" state={hookStates.L2} />
          <HookMatrixCard id="L3" label="L3 CCR" icon={<Database className="w-4 h-4" />} desc="Chunk registry L3" state={hookStates.L3} />
          <HookMatrixCard id="SmartCrusher" label="SmartCrusher" icon={<Sparkles className="w-4 h-4" />} desc="CSV/JSON rows dropped" state={hookStates.SmartCrusher} />
          <HookMatrixCard id="DiffCompressor" label="Diff Compactor" icon={<GitCompare className="w-4 h-4" />} desc="Git diff lines filtered" state={hookStates.DiffCompressor} />
          <HookMatrixCard id="ASTCompactor" label="AST Compactor" icon={<Code2 className="w-4 h-4" />} desc="Tree-sitter signature elision" state={hookStates.ASTCompactor} />
        </div>
      </div>
    </div>
  );
}

function ProgressBar({ label, count, total, color }: { label: string; count: number; total: number; color: string }) {
  const percentage = (count / Math.max(1, total)) * 100;
  return (
    <div>
      <div className="flex justify-between text-xs font-bold text-gray-500 mb-1">
        <span>{label}</span>
        <span className="text-white">{formatNumber(count)}</span>
      </div>
      <div className="h-2 w-full bg-white/5 rounded-full overflow-hidden">
        <div className={`h-full ${color} transition-all duration-500`} style={{ width: `${percentage}%` }} />
      </div>
    </div>
  );
}

const HOOK_BG: Record<HookState, string> = {
  idle: "bg-zinc-950/40 border-white/5 text-zinc-500 shadow-none",
  active: "bg-cyan-500/10 border-cyan-400 text-cyan-200 shadow-md shadow-cyan-500/10 scale-102",
  hit: "bg-emerald-500/10 border-emerald-400 text-emerald-200 shadow-md shadow-emerald-500/20 scale-102",
  miss: "bg-rose-500/5 border-rose-500/30 text-rose-400 opacity-60 shadow-none",
  compressed: "bg-purple-500/10 border-purple-400 text-purple-200 shadow-md shadow-purple-500/20 scale-102"
};

const HOOK_GLOW: Record<HookState, string> = {
  idle: "text-zinc-600",
  active: "text-cyan-400 drop-shadow-[0_0_8px_rgba(34,211,238,0.5)]",
  hit: "text-emerald-400 drop-shadow-[0_0_8px_rgba(52,211,153,0.5)]",
  miss: "text-rose-500/40",
  compressed: "text-purple-400 drop-shadow-[0_0_8px_rgba(168,85,247,0.5)]"
};

function HookMatrixCard({ id, label, icon, desc, state }: { id: string; label: string; icon: React.ReactNode; desc: string; state: HookState }) {
  return (
    <div className={`p-3 rounded-2xl border transition-all duration-300 flex flex-col justify-between h-24 ${HOOK_BG[state]}`}>
      <div className="flex items-center justify-between w-full">
        <span className={`p-1.5 rounded-lg bg-black/40 border border-white/5 ${HOOK_GLOW[state]}`}>
          {icon}
        </span>
        <span className={`text-[8px] font-mono font-bold uppercase tracking-wider ${
          state === "hit" ? "text-emerald-400" :
          state === "miss" ? "text-rose-500/50" :
          state === "compressed" ? "text-purple-400" :
          state === "active" ? "text-cyan-400 animate-pulse" : "text-zinc-600"
        }`}>
          {state === "hit" ? "Hit" :
           state === "miss" ? "Miss" :
           state === "compressed" ? "Opt" :
           state === "active" ? "Exec" : "Idle"}
        </span>
      </div>
      <div className="space-y-0.5 mt-2">
        <h4 className={`text-xs font-bold font-mono tracking-tight ${state !== "idle" && state !== "miss" ? "text-white" : "text-zinc-400"}`}>{label}</h4>
        <p className="text-[9px] text-zinc-500 leading-tight truncate">{desc}</p>
      </div>
    </div>
  );
}
