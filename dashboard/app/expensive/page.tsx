import { ExpensivePromptsPanel } from "@/components/ExpensivePromptsPanel";

export const metadata = {
  title: "Most Expensive Prompts — Synapse Proxy",
};

export default function ExpensivePromptsPage() {
  return (
    <div className="min-h-screen bg-[#050505] text-white p-6 md:p-10">
      <div className="max-w-7xl mx-auto space-y-6">
        <header className="flex items-center justify-between">
          <div>
            <h1 className="text-2xl font-black tracking-tight">Most Expensive Prompts</h1>
            <p className="text-zinc-500 text-xs mt-1">
              Top prompts by total cost (at the current $1/MTok fallback pricing). The
              "L2 potential" column shows how much you could save if these prompts
              were added to the semantic cache — assumes an 80% L2 hit rate on repeats.
            </p>
          </div>
          <a
            href="/"
            className="text-xs text-zinc-500 hover:text-white px-3 py-1.5 rounded-lg border border-white/10 bg-white/5"
          >
            ← Back to dashboard
          </a>
        </header>
        <ExpensivePromptsPanel />
      </div>
    </div>
  );
}
