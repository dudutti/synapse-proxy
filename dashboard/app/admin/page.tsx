import { prisma } from "@/lib/prisma";
import { revalidatePath } from "next/cache";
import { GlobalCommandCenter } from "@/components/GlobalCommandCenter";
import { ServerHealthCard } from "@/components/ServerHealthCard";
import { LiveLogConsole } from "@/components/LiveLogConsole";
import { AlertRulesPanel } from "@/components/AlertRulesPanel";

async function toggleRegistration(formData: FormData) {
  "use server";
  const isOpen = formData.get("action") === "open";
  
  await prisma.systemConfig.upsert({
    where: { id: "global" },
    update: { registrationOpen: isOpen },
    create: { id: "global", registrationOpen: isOpen }
  });
  
  revalidatePath("/admin");
}

export default async function AdminOverview() {
  const userCount = await prisma.user.count();
  const prospectCount = await prisma.prospect.count();
  
  let config = await prisma.systemConfig.findUnique({ where: { id: "global" } });
  if (!config) {
    config = await prisma.systemConfig.create({ data: { id: "global", registrationOpen: false } });
  }

  return (
    <div className="p-10 max-w-7xl mx-auto space-y-12">
      
      {/* HUD SPATIAL COMMAND CENTER */}
      <section>
        <GlobalCommandCenter />
      </section>

      {/* LOWER SECTION */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-6 max-w-4xl">
        <div className="p-6 bg-white/5 border border-white/10 rounded-2xl">
          <h3 className="text-sm text-gray-400 mb-2 font-bold uppercase tracking-wider">Total Users</h3>
          <p className="text-4xl font-black text-white">{userCount}</p>
        </div>
        
        <div className="p-6 bg-white/5 border border-white/10 rounded-2xl">
          <h3 className="text-sm text-gray-400 mb-2 font-bold uppercase tracking-wider">Waitlist Prospects</h3>
          <p className="text-4xl font-black text-amber-400">{prospectCount}</p>
        </div>
      </div>

      <div className="p-6 bg-black/40 border border-white/10 rounded-2xl mt-8 max-w-4xl">
        <h2 className="text-xl font-bold mb-4">Registration Status</h2>
        <div className="flex items-center justify-between">
          <div>
            <p className="text-gray-300">
              Currently, public registration is <strong className={config.registrationOpen ? "text-emerald-400" : "text-amber-400"}>{config.registrationOpen ? "OPEN" : "CLOSED"}</strong>.
            </p>
            <p className="text-xs text-gray-500 mt-1">If closed, users will only see the Waitlist form on the login page.</p>
          </div>

          <div className="flex items-center gap-3">
            <a
              href="/admin/explorer"
              className="px-4 py-2 bg-blue-500/10 text-blue-400 border border-blue-500/20 rounded-xl font-bold hover:bg-blue-500/20 transition-colors text-sm"
            >
              Request Explorer {"\u2192"}
            </a>
            <a
              href="/admin/pricing"
              className="px-4 py-2 bg-amber-500/10 text-amber-400 border border-amber-500/20 rounded-xl font-bold hover:bg-amber-500/20 transition-colors text-sm"
            >
              Pricing Coverage {"\u2192"}
            </a>
            <a
              href="/admin/expensive"
              className="px-4 py-2 bg-orange-500/10 text-orange-400 border border-orange-500/20 rounded-xl font-bold hover:bg-orange-500/20 transition-colors text-sm"
            >
              Expensive Prompts {"\u2192"}
            </a>
            <a
              href="/admin/sessions"
              className="px-4 py-2 bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 rounded-xl font-bold hover:bg-emerald-500/20 transition-colors text-sm"
            >
              Session History {"\u2192"}
            </a>
            <form action={toggleRegistration}>
              {config.registrationOpen ? (
                <button name="action" value="close" type="submit" className="px-6 py-3 bg-amber-500/10 text-amber-400 border border-amber-500/20 rounded-xl font-bold hover:bg-amber-500/20 transition-colors">
                  Close Registration
                </button>
              ) : (
                <button name="action" value="open" type="submit" className="px-6 py-3 bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 rounded-xl font-bold hover:bg-emerald-500/20 transition-colors">
                  Open Registration
                </button>
              )}
            </form>
          </div>
        </div>
      </div>

      {/* SUPERADMIN LIVE STATUS — auto-refreshing metrics HUD combining
          the proxy /metrics endpoint and DB aggregates. Refreshes every
          5 seconds, animates transitions, shows sparkline history of
          the last 30 samples, and surfaces cache-hit-rate, panic
          counts, upstream error rate, pricing coverage gaps, and DB
          growth in a single dense panel. */}
      <section>
        <ServerHealthCard />
      </section>

      {/* LIVE LOG CONSOLE — SSE-fed terminal that streams every
          RequestLog in real time. Filter by cache level / agent /
          minimum $ saved. Pause to inspect, auto-resumes. */}
      <section>
        <LiveLogConsole />
      </section>

      {/* ALERT RULES — configurable thresholds for panic_rate, error_rate,
          cache_hit_rate, upstream_latency_p95, pricing_gaps. Each rule
          can fire email + Slack hooks. Unacknowledged events show
          inline with an ACK button. */}
      <section>
        <AlertRulesPanel />
      </section>
    </div>
  );
}
