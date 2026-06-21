import { AlertRulesPanel } from "@/components/AlertRulesPanel";

export const metadata = {
  title: "Alert Rules \u2014 Synapse Proxy",
};

export default function AlertsPage() {
  return (
    <div className="min-h-screen bg-[#050505] text-white p-6 md:p-10">
      <div className="max-w-5xl mx-auto space-y-6">
        <header className="flex items-center justify-between">
          <div>
            <h1 className="text-2xl font-black tracking-tight">Alert Rules</h1>
            <p className="text-zinc-500 text-xs mt-1">
              Configure alerts for key metrics like cache hit rate, panics, and pricing gaps.
              Get notified via email or Slack when thresholds are crossed.
            </p>
          </div>
          <a
            href="/"
            className="text-xs text-zinc-500 hover:text-white px-3 py-1.5 rounded-lg border border-white/10 bg-white/5"
          >
            \u2190 Back to dashboard
          </a>
        </header>
        <AlertRulesPanel />
      </div>
    </div>
  );
}
