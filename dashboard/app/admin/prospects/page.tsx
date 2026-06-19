import { prisma } from "@/lib/prisma";

export default async function AdminProspects() {
  const prospects = await prisma.prospect.findMany({
    orderBy: { createdAt: "desc" }
  });

  return (
    <div className="p-10 max-w-4xl mx-auto space-y-8">
      <div className="flex justify-between items-center">
        <h1 className="text-3xl font-black">Waitlist Prospects</h1>
        <div className="text-sm text-gray-400 bg-white/10 px-4 py-2 rounded-xl">
          Total: <strong className="text-white">{prospects.length}</strong>
        </div>
      </div>
      
      <div className="bg-black/40 border border-white/10 rounded-2xl overflow-hidden">
        <table className="w-full text-left text-sm">
          <thead className="bg-white/5 border-b border-white/10 text-gray-400">
            <tr>
              <th className="p-4 font-bold">Email</th>
              <th className="p-4 font-bold">Signup Date</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-white/5">
            {prospects.map(p => (
              <tr key={p.id} className="hover:bg-white/5 transition-colors">
                <td className="p-4 font-medium text-emerald-400">{p.email}</td>
                <td className="p-4 text-gray-400">{new Date(p.createdAt).toLocaleString()}</td>
              </tr>
            ))}
            {prospects.length === 0 && (
              <tr>
                <td colSpan={2} className="p-8 text-center text-gray-500">Waitlist is empty.</td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
