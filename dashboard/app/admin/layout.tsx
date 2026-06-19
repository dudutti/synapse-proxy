import { getServerSession } from "next-auth";
import { authOptions } from "@/lib/authOptions";
import { redirect } from "next/navigation";
import Link from "next/link";
import { Users, Database, Settings, ShieldAlert, FileText, Mail } from "lucide-react";

export default async function AdminLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const session = await getServerSession(authOptions);
  
  if (!session || (session.user as any).role !== "SUPERADMIN") {
    redirect("/");
  }

  return (
    <div className="min-h-screen bg-[#050505] text-white flex">
      {/* Sidebar */}
      <div className="w-64 border-r border-white/10 p-6 flex flex-col gap-6">
        <div className="flex items-center gap-2 text-amber-500 font-bold text-xl mb-4">
          <ShieldAlert className="w-6 h-6" />
          <span>Superadmin</span>
        </div>
        
        <nav className="flex flex-col gap-2">
          <Link href="/admin" className="flex items-center gap-3 p-3 rounded-xl hover:bg-white/5 transition-colors text-sm text-gray-300 hover:text-white">
            <Settings className="w-5 h-5" /> Dashboard
          </Link>
          <Link href="/admin/users" className="flex items-center gap-3 p-3 rounded-xl hover:bg-white/5 transition-colors text-sm text-gray-300 hover:text-white">
            <Users className="w-5 h-5" /> Users
          </Link>
          <Link href="/admin/prospects" className="flex items-center gap-3 p-3 rounded-xl hover:bg-white/5 transition-colors text-sm text-gray-300 hover:text-white">
            <FileText className="w-5 h-5" /> Waitlist
          </Link>
          <Link href="/admin/pricing" className="flex items-center gap-3 p-3 rounded-xl hover:bg-white/5 transition-colors text-sm text-gray-300 hover:text-white">
            <Database className="w-5 h-5" /> Model Pricing
          </Link>
          <Link href="/admin/emails" className="flex items-center gap-3 p-3 rounded-xl hover:bg-white/5 transition-colors text-sm text-gray-300 hover:text-white">
            <Mail className="w-5 h-5" /> Email Templates
          </Link>
        </nav>

        <div className="mt-auto pt-6 border-t border-white/10">
          <Link href="/" className="text-sm text-gray-500 hover:text-white transition-colors">
            &larr; Back to App
          </Link>
        </div>
      </div>

      {/* Main Content */}
      <div className="flex-1 overflow-auto bg-[#0a0a0a]">
        {children}
      </div>
    </div>
  );
}
