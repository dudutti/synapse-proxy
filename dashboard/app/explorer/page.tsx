import { Suspense } from "react";
import { RequestExplorer } from "@/components/RequestExplorer";

export const metadata = {
  title: "Request Explorer — Synapse Proxy",
};

export default function ExplorerPage() {
  return (
    <div className="min-h-screen bg-[#050505] text-white p-6 md:p-10">
      <div className="max-w-7xl mx-auto space-y-6">
        <header className="flex items-center justify-between">
          <div>
            <h1 className="text-2xl font-black tracking-tight">Request Explorer</h1>
            <p className="text-zinc-500 text-xs mt-1">
              Advanced search across every RequestLog in the platform.
              Click any row to drill down into the full payload.
            </p>
          </div>
          <a
            href="/"
            className="text-xs text-zinc-500 hover:text-white px-3 py-1.5 rounded-lg border border-white/10 bg-white/5"
          >
            ← Back to dashboard
          </a>
        </header>
        <Suspense fallback={<div className="text-zinc-500 text-xs">Loading Explorer...</div>}>
          <RequestExplorer />
        </Suspense>
      </div>
    </div>
  );
}
