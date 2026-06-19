import { prisma } from "@/lib/prisma";

export default async function AdminUsers() {
  const users = await prisma.user.findMany({
    orderBy: { createdAt: "desc" },
    include: { apiKeys: true }
  });

  return (
    <div className="p-10 max-w-6xl mx-auto space-y-8">
      <h1 className="text-3xl font-black">Users Management</h1>
      
      <div className="bg-black/40 border border-white/10 rounded-2xl overflow-hidden">
        <table className="w-full text-left text-sm">
          <thead className="bg-white/5 border-b border-white/10 text-gray-400">
            <tr>
              <th className="p-4 font-bold">Email</th>
              <th className="p-4 font-bold">Role</th>
              <th className="p-4 font-bold">Joined</th>
              <th className="p-4 font-bold">API Keys</th>
              <th className="p-4 font-bold">Stripe Sub</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-white/5">
            {users.map(u => (
              <tr key={u.id} className="hover:bg-white/5 transition-colors">
                <td className="p-4 font-medium">{u.email}</td>
                <td className="p-4">
                  <span className={`px-2 py-1 rounded text-xs font-bold ${u.role === "SUPERADMIN" ? "bg-amber-500/20 text-amber-400" : "bg-white/10 text-gray-300"}`}>
                    {u.role}
                  </span>
                </td>
                <td className="p-4 text-gray-400">{new Date(u.createdAt).toLocaleDateString()}</td>
                <td className="p-4">{u.apiKeys.length}</td>
                <td className="p-4">
                  {u.stripeSubscriptionId ? (
                    <span className="text-emerald-400 text-xs font-mono">{u.stripeSubscriptionId}</span>
                  ) : (
                    <span className="text-gray-600 text-xs italic">None</span>
                  )}
                </td>
              </tr>
            ))}
            {users.length === 0 && (
              <tr>
                <td colSpan={5} className="p-8 text-center text-gray-500">No users found.</td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
