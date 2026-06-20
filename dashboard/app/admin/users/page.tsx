"use client";

import { useEffect, useState } from "react";
import { toast } from "sonner";
import { UserCheck, AlertTriangle } from "lucide-react";

interface ApiKey {
  id: string;
  virtualKey: string;
}

interface User {
  id: string;
  email: string;
  role: string;
  tier: string;
  currentMonthTokens: number;
  stripeSubscriptionId: string | null;
  createdAt: string;
  apiKeys: ApiKey[];
}

export default function AdminUsersPage() {
  const [users, setUsers] = useState<User[]>([]);
  const [loading, setLoading] = useState(true);

  const fetchUsers = async () => {
    setLoading(true);
    try {
      const res = await fetch("/api/admin/users");
      if (res.ok) {
        const data = await res.json();
        setUsers(data);
      } else {
        toast.error("Failed to load users");
      }
    } catch (err) {
      toast.error("Error loading users");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchUsers();
  }, []);

  const handleTierChange = async (userId: string, newTier: string) => {
    const tId = toast.loading("Updating user tier...");
    try {
      const res = await fetch(`/api/admin/users/${userId}/tier`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ tier: newTier }),
      });

      if (res.ok) {
        toast.success(`User tier updated to ${newTier}`, { id: tId });
        fetchUsers();
      } else {
        toast.error("Failed to update tier", { id: tId });
      }
    } catch (err) {
      toast.error("Error updating user tier", { id: tId });
    }
  };

  return (
    <div className="min-h-screen bg-[#050505] text-white p-6 md:p-10 font-sans relative overflow-hidden">
      {/* Background Glow */}
      <div className="absolute top-[20%] right-[10%] w-96 h-96 bg-blue-500/5 rounded-full blur-[150px] pointer-events-none" />
      <div className="absolute bottom-[20%] left-[10%] w-96 h-96 bg-indigo-500/5 rounded-full blur-[150px] pointer-events-none" />

      <div className="max-w-6xl mx-auto space-y-8 relative z-10">
        <header className="flex items-center justify-between bg-white/5 border border-white/10 p-6 rounded-2xl backdrop-blur-xl shadow-2xl">
          <div className="flex items-center gap-3">
            <div className="p-2.5 bg-blue-500/20 rounded-xl border border-blue-500/30 text-blue-400">
              <UserCheck className="w-6 h-6" />
            </div>
            <div>
              <h1 className="text-2xl font-black tracking-tight">Users Management</h1>
              <p className="text-zinc-500 text-xs mt-0.5">Manage user roles, manually override subscription tiers, and monitor token consumption.</p>
            </div>
          </div>
          <a
            href="/admin"
            className="text-xs text-zinc-400 hover:text-white px-4 py-2 rounded-xl border border-white/10 bg-white/5 transition-all hover:bg-white/10"
          >
            ← Dashboard
          </a>
        </header>

        <div className="bg-white/5 rounded-2xl border border-white/10 backdrop-blur-xl shadow-2xl overflow-hidden">
          {loading ? (
            <div className="text-zinc-500 text-sm py-24 text-center animate-pulse">Loading user profiles...</div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-left text-sm">
                <thead className="bg-white/5 border-b border-white/10 text-zinc-400">
                  <tr>
                    <th className="p-4 font-bold uppercase text-[10px] tracking-wider">Email</th>
                    <th className="p-4 font-bold uppercase text-[10px] tracking-wider">Role</th>
                    <th className="p-4 font-bold uppercase text-[10px] tracking-wider">Subscription Tier</th>
                    <th className="p-4 font-bold uppercase text-[10px] tracking-wider">Monthly Tokens</th>
                    <th className="p-4 font-bold uppercase text-[10px] tracking-wider">Joined</th>
                    <th className="p-4 font-bold uppercase text-[10px] tracking-wider">API Keys</th>
                    <th className="p-4 font-bold uppercase text-[10px] tracking-wider">Stripe Sub ID</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-white/5">
                  {users.map((u) => (
                    <tr key={u.id} className="hover:bg-white/5 transition-colors">
                      <td className="p-4 font-medium text-white select-all">{u.email}</td>
                      <td className="p-4">
                        <span className={`px-2 py-0.5 rounded text-[10px] font-black tracking-wide ${
                          u.role === "SUPERADMIN" ? "bg-amber-500/20 text-amber-400 border border-amber-500/30" : "bg-zinc-800 text-zinc-300 border border-zinc-700/50"
                        }`}>
                          {u.role}
                        </span>
                      </td>
                      <td className="p-4">
                        <select
                          value={u.tier || "FREE"}
                          onChange={(e) => handleTierChange(u.id, e.target.value)}
                          className="bg-black/50 border border-white/10 text-white rounded-lg px-2.5 py-1 text-xs focus:border-emerald-500 focus:outline-none transition-all cursor-pointer font-bold bg-[#0a0a0a]"
                        >
                          <option value="FREE">FREE</option>
                          <option value="PRO_1">PRO_1</option>
                          <option value="PRO_2">PRO_2</option>
                        </select>
                      </td>
                      <td className="p-4 font-mono text-zinc-300">
                        {(u.currentMonthTokens || 0).toLocaleString()}
                        <span className="text-[10px] text-zinc-500 block">
                          Limit: {
                            u.tier === "FREE" ? "10M" :
                            u.tier === "PRO_1" ? "20M" :
                            u.tier === "PRO_2" ? "100M" : "Unlimited"
                          }
                        </span>
                      </td>
                      <td className="p-4 text-zinc-400 text-xs">
                        {new Date(u.createdAt).toLocaleDateString()}
                      </td>
                      <td className="p-4 font-mono text-zinc-300">{u.apiKeys.length}</td>
                      <td className="p-4 font-mono text-xs">
                        {u.stripeSubscriptionId ? (
                          <span className="text-emerald-400 select-all">{u.stripeSubscriptionId}</span>
                        ) : (
                          <span className="text-zinc-600 italic text-[11px]">None</span>
                        )}
                      </td>
                    </tr>
                  ))}
                  {users.length === 0 && (
                    <tr>
                      <td colSpan={7} className="p-12 text-center text-zinc-500">
                        <AlertTriangle className="w-8 h-8 mx-auto mb-3 opacity-20" />
                        No users registered yet.
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
