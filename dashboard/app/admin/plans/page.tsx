"use client";

import { useEffect, useState } from "react";
import { Plus, Trash2, CreditCard, Sparkles, CheckCircle } from "lucide-react";
import { toast } from "sonner";

interface StripePlan {
  id: string;
  name: string;
  tier: string;
  priceId: string;
  amount: number;
  tokens: number;
}

export default function AdminPlansPage() {
  const [plans, setPlans] = useState<StripePlan[]>([]);
  const [loading, setLoading] = useState(true);

  // Form states
  const [name, setName] = useState("");
  const [tier, setTier] = useState("PRO_1");
  const [priceId, setPriceId] = useState("");
  const [amount, setAmount] = useState("");
  const [tokens, setTokens] = useState("");

  const fetchPlans = async () => {
    setLoading(true);
    try {
      const res = await fetch("/api/admin/plans");
      if (res.ok) {
        const data = await res.json();
        setPlans(data);
      } else {
        toast.error("Failed to load plans");
      }
    } catch (err) {
      toast.error("Error loading plans");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchPlans();
  }, []);

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name || !priceId || !amount || !tokens) {
      toast.error("Please fill in all fields");
      return;
    }

    const tId = toast.loading("Saving plan...");
    try {
      const res = await fetch("/api/admin/plans", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          name,
          tier,
          priceId,
          amount: parseFloat(amount),
          tokens: parseInt(tokens),
        }),
      });

      if (res.ok) {
        toast.success("Plan saved successfully", { id: tId });
        setName("");
        setPriceId("");
        setAmount("");
        setTokens("");
        fetchPlans();
      } else {
        const data = await res.json().catch(() => ({}));
        toast.error(data.error || "Failed to save plan", { id: tId });
      }
    } catch (err) {
      toast.error("Error saving plan", { id: tId });
    }
  };

  const handleDelete = async (id: string) => {
    if (!confirm("Are you sure you want to delete this plan?")) return;

    const tId = toast.loading("Deleting plan...");
    try {
      const res = await fetch(`/api/admin/plans?id=${id}`, {
        method: "DELETE",
      });

      if (res.ok) {
        toast.success("Plan deleted successfully", { id: tId });
        fetchPlans();
      } else {
        toast.error("Failed to delete plan", { id: tId });
      }
    } catch (err) {
      toast.error("Error deleting plan", { id: tId });
    }
  };

  return (
    <div className="min-h-screen bg-[#050505] text-white p-6 md:p-10 font-sans relative overflow-hidden">
      {/* Background Glow */}
      <div className="absolute top-[20%] right-[10%] w-96 h-96 bg-emerald-500/5 rounded-full blur-[150px] pointer-events-none" />
      <div className="absolute bottom-[20%] left-[10%] w-96 h-96 bg-teal-500/5 rounded-full blur-[150px] pointer-events-none" />

      <div className="max-w-5xl mx-auto space-y-8 relative z-10">
        <header className="flex items-center justify-between bg-white/5 border border-white/10 p-6 rounded-2xl backdrop-blur-xl shadow-2xl">
          <div className="flex items-center gap-3">
            <div className="p-2.5 bg-emerald-500/20 rounded-xl border border-emerald-500/30 text-emerald-400">
              <CreditCard className="w-6 h-6" />
            </div>
            <div>
              <h1 className="text-2xl font-black tracking-tight">Stripe Price ID Mappings</h1>
              <p className="text-zinc-500 text-xs mt-0.5">Define which Stripe Price IDs correspond to which monthly usage tiers.</p>
            </div>
          </div>
          <a
            href="/admin"
            className="text-xs text-zinc-400 hover:text-white px-4 py-2 rounded-xl border border-white/10 bg-white/5 transition-all hover:bg-white/10"
          >
            ← Dashboard
          </a>
        </header>

        <div className="grid grid-cols-1 lg:grid-cols-3 gap-8">
          {/* Form */}
          <div className="lg:col-span-1 bg-white/5 p-6 rounded-2xl border border-white/10 backdrop-blur-xl shadow-2xl space-y-6">
            <h2 className="text-lg font-bold flex items-center gap-2 border-b border-white/5 pb-4">
              <Sparkles className="w-5 h-5 text-emerald-400" />
              <span>Add / Update Mapping</span>
            </h2>

            <form onSubmit={handleCreate} className="space-y-4">
              <div>
                <label className="block text-[10px] uppercase tracking-wider font-bold mb-1.5 text-zinc-400">Plan Name</label>
                <input
                  type="text"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="e.g. Pro Monthly"
                  className="w-full p-3 bg-black/40 border border-white/10 rounded-xl focus:border-emerald-500 focus:outline-none text-sm transition-all bg-[#0a0a0a]"
                  required
                />
              </div>

              <div>
                <label className="block text-[10px] uppercase tracking-wider font-bold mb-1.5 text-zinc-400">Target Tier</label>
                <select
                  value={tier}
                  onChange={(e) => setTier(e.target.value)}
                  className="w-full p-3 bg-black/40 border border-white/10 rounded-xl focus:border-emerald-500 focus:outline-none text-sm transition-all text-white bg-[#0a0a0a]"
                >
                  <option value="FREE">FREE (10M Limit)</option>
                  <option value="PRO_1">PRO_1 (20M Limit)</option>
                  <option value="PRO_2">PRO_2 (100M Limit)</option>
                </select>
              </div>

              <div>
                <label className="block text-[10px] uppercase tracking-wider font-bold mb-1.5 text-zinc-400">Stripe Price ID</label>
                <input
                  type="text"
                  value={priceId}
                  onChange={(e) => setPriceId(e.target.value)}
                  placeholder="e.g. price_1Q..."
                  className="w-full p-3 bg-black/40 border border-white/10 rounded-xl focus:border-emerald-500 focus:outline-none text-sm transition-all font-mono bg-[#0a0a0a]"
                  required
                />
              </div>

              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="block text-[10px] uppercase tracking-wider font-bold mb-1.5 text-zinc-400">Amount (€ / mo)</label>
                  <input
                    type="number"
                    step="0.01"
                    value={amount}
                    onChange={(e) => setAmount(e.target.value)}
                    placeholder="e.g. 5"
                    className="w-full p-3 bg-black/40 border border-white/10 rounded-xl focus:border-emerald-500 focus:outline-none text-sm transition-all bg-[#0a0a0a]"
                    required
                  />
                </div>
                <div>
                  <label className="block text-[10px] uppercase tracking-wider font-bold mb-1.5 text-zinc-400">Tokens / mo</label>
                  <input
                    type="number"
                    value={tokens}
                    onChange={(e) => setTokens(e.target.value)}
                    placeholder="e.g. 20000000"
                    className="w-full p-3 bg-[#0a0a0a] border border-white/10 rounded-xl focus:border-emerald-500 focus:outline-none text-sm transition-all"
                    required
                  />
                </div>
              </div>

              <button
                type="submit"
                className="w-full mt-4 flex items-center justify-center gap-2 bg-emerald-500 text-black font-bold text-sm py-3 px-6 rounded-xl hover:bg-emerald-400 transition-all hover:shadow-[0_0_20px_rgba(16,185,129,0.2)]"
              >
                <Plus className="w-4 h-4" /> Save Mapping
              </button>
            </form>
          </div>

          {/* List Table */}
          <div className="lg:col-span-2 bg-white/5 p-6 rounded-2xl border border-white/10 backdrop-blur-xl shadow-2xl space-y-6">
            <h2 className="text-lg font-bold flex items-center gap-2 border-b border-white/5 pb-4">
              <CheckCircle className="w-5 h-5 text-emerald-400" />
              <span>Active Mappings ({plans.length})</span>
            </h2>

            {loading ? (
              <div className="text-zinc-500 text-sm py-12 text-center animate-pulse">Loading plans...</div>
            ) : plans.length === 0 ? (
              <div className="text-zinc-500 text-center py-12 bg-black/20 rounded-2xl border border-white/5 border-dashed">
                No Stripe price mappings defined. Configure one on the left.
              </div>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-left text-sm">
                  <thead className="text-zinc-500 border-b border-white/5">
                    <tr>
                      <th className="pb-3 font-semibold uppercase text-[10px] tracking-wider">Plan / Price ID</th>
                      <th className="pb-3 font-semibold uppercase text-[10px] tracking-wider">Tier</th>
                      <th className="pb-3 font-semibold uppercase text-[10px] tracking-wider">Amount</th>
                      <th className="pb-3 font-semibold uppercase text-[10px] tracking-wider">Token Limit</th>
                      <th className="pb-3 font-semibold uppercase text-[10px] tracking-wider text-right">Action</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-white/5">
                    {plans.map((plan) => (
                      <tr key={plan.id} className="hover:bg-white/[0.02] transition-colors">
                        <td className="py-4">
                          <div className="font-bold text-white text-sm">{plan.name}</div>
                          <div className="font-mono text-zinc-500 text-xs mt-0.5 select-all">{plan.priceId}</div>
                        </td>
                        <td className="py-4">
                          <span className={`px-2 py-0.5 rounded text-[10px] font-bold ${
                            plan.tier === "FREE" ? "bg-white/10 text-zinc-300" :
                            plan.tier === "PRO_1" ? "bg-emerald-500/20 text-emerald-400" : "bg-teal-500/20 text-teal-400"
                          }`}>
                            {plan.tier}
                          </span>
                        </td>
                        <td className="py-4 font-semibold text-zinc-300">
                          €{plan.amount.toFixed(2)}<span className="text-xs font-normal text-zinc-600">/mo</span>
                        </td>
                        <td className="py-4 font-mono text-zinc-300">
                          {(plan.tokens / 1000000).toFixed(0)}M
                        </td>
                        <td className="py-4 text-right">
                          <button
                            onClick={() => handleDelete(plan.id)}
                            className="p-2 text-zinc-500 hover:text-red-400 hover:bg-red-500/10 rounded-lg transition-all"
                            title="Delete plan"
                          >
                            <Trash2 className="w-4 h-4" />
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
