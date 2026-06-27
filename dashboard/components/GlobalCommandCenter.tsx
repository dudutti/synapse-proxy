"use client";

import React, { useEffect, useState, memo, useRef } from "react";
import dynamic from "next/dynamic";
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
};

// Memoized Globe component using react-globe.gl (Three.js based, extremely robust)
const GlobeCanvas = memo(({ markers }: { markers: TelemetryData["markers"] }) => {
  const globeRef = useRef<any>();
  const [dimensions, setDimensions] = useState({ width: 400, height: 400 });
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    // Auto-rotate
    if (globeRef.current) {
      globeRef.current.controls().autoRotate = true;
      globeRef.current.controls().autoRotateSpeed = 1.0;
      globeRef.current.controls().enableZoom = false;
    }

    // Handle responsive sizing
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

  // Map markers to glowing rings
  const ringsData = markers.map(m => ({
    lat: m.location[0],
    lng: m.location[1],
    maxR: m.size * 80, // Scale up for visibility
    propagationSpeed: 2,
    repeatPeriod: 1000 + Math.random() * 1000, // Randomize pulses
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
      />
      {/* Overlay gradient so the globe fades into the dark background at the edges */}
      <div className="absolute inset-0 bg-[radial-gradient(circle_at_center,transparent_50%,rgba(0,0,0,0.8)_100%)] pointer-events-none z-10" />
    </div>
  );
});

GlobeCanvas.displayName = "GlobeCanvas";

export function GlobalCommandCenter() {
  const [data, setData] = useState<TelemetryData | null>(null);

  useEffect(() => {
    // Fetch data immediately, then poll every 5 seconds
    const fetchTelemetry = async () => {
      try {
        const res = await fetch("/api/admin/telemetry");
        if (res.ok) {
          const json = await res.json();
          setData(json);
        }
      } catch (err) {
        console.error("Failed to fetch global telemetry", err);
      }
    };
    
    fetchTelemetry();
    const interval = setInterval(fetchTelemetry, 15000);
    return () => clearInterval(interval);
  }, []);

  if (!data) {
    return <div className="h-[600px] flex items-center justify-center text-gray-500 font-mono animate-pulse">Establishing Satellite Uplink...</div>;
  }

  const { stats } = data;

  return (
    <div className="relative w-full rounded-3xl overflow-hidden bg-black/60 border border-white/10 p-8 flex flex-col xl:flex-row items-center justify-between gap-8">
      
      {/* LEFT STATS HUD */}
      <div className="flex-1 space-y-8 z-10">
        <div>
          <h2 className="text-emerald-400 text-xs font-black tracking-[0.2em] uppercase mb-1 drop-shadow-[0_0_8px_rgba(52,211,153,0.5)]">
            Global Operations
          </h2>
          <div className="text-5xl font-black text-white">{formatNumber(stats.totalRequests)}</div>
          <div className="text-gray-400 text-sm mt-1">Total API Requests Processed</div>
        </div>

        <div className="space-y-4">
          <div>
            <div className="flex justify-between text-xs font-bold text-gray-500 mb-1">
              <span>L1 (Exact)</span>
              <span className="text-white">{formatNumber(stats.distribution.L1)}</span>
            </div>
            <div className="h-2 w-full bg-white/5 rounded-full overflow-hidden">
              <div className="h-full bg-emerald-400" style={{ width: `${(stats.distribution.L1 / Math.max(1, stats.totalRequests)) * 100}%` }} />
            </div>
          </div>
          <div>
            <div className="flex justify-between text-xs font-bold text-gray-500 mb-1">
              <span>L2 (Semantic)</span>
              <span className="text-white">{formatNumber(stats.distribution.L2)}</span>
            </div>
            <div className="h-2 w-full bg-white/5 rounded-full overflow-hidden">
              <div className="h-full bg-emerald-400" style={{ width: `${(stats.distribution.L2 / Math.max(1, stats.totalRequests)) * 100}%` }} />
            </div>
          </div>
          <div>
            <div className="flex justify-between text-xs font-bold text-gray-500 mb-1">
              <span>L3 (Compression)</span>
              <span className="text-white">{formatNumber(stats.distribution.L3)}</span>
            </div>
            <div className="h-2 w-full bg-white/5 rounded-full overflow-hidden">
              <div className="h-full bg-purple-500" style={{ width: `${(stats.distribution.L3 / Math.max(1, stats.totalRequests)) * 100}%` }} />
            </div>
          </div>
        </div>
      </div>

      {/* PER-HOOK BREAKDOWN */}
      <PerHookBreakdown data={data.stats.hookSavings} />

      {/* GLOBE CANVAS (Stable, only mounts once) */}
      <GlobeCanvas markers={data.markers} />

      {/* RIGHT STATS HUD */}
      <div className="flex-1 space-y-8 z-10 text-right">
        <div>
          <h2 className="text-amber-400 text-xs font-black tracking-[0.2em] uppercase mb-1 drop-shadow-[0_0_8px_rgba(251,191,36,0.5)]">
            Total Wealth Preserved
          </h2>
          <div className="text-6xl font-black text-amber-400 drop-shadow-[0_0_15px_rgba(251,191,36,0.3)]">
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
  );
}
