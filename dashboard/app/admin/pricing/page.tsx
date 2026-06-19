import { PricingCoveragePanel } from "@/components/PricingCoveragePanel";

export const metadata = {
  title: "Pricing Coverage — Synapse Proxy",
};

export default function PricingPage() {
  return (
    <div className="min-h-screen bg-[#050505] text-white p-6 md:p-10">
      <div className="max-w-5xl mx-auto space-y-6">
        <header className="flex items-center justify-between">
          <div>
            <h1 className="text-2xl font-black tracking-tight">Pricing Coverage</h1>
            <p className="text-zinc-500 text-xs mt-1">
              Every (provider, model) pair that has been used in production but is missing
              from <code className="text-amber-300">ProviderModel</code>. Currently each
              of these requests is being billed at the <strong className="text-amber-300">$1/MTok
              fallback</strong> instead of the real price — click <strong>Fix this</strong> to
              add the correct pricing and unlock real savings calculations.
            </p>
          </div>
          <a
            href="/admin"
            className="text-xs text-zinc-500 hover:text-white px-3 py-1.5 rounded-lg border border-white/10 bg-white/5"
          >
            ← Back to admin
          </a>
        </header>
        <PricingCoveragePanel />
      </div>
    </div>
  );
}
