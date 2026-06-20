import type { Metadata } from "next";
import Link from "next/link";
import HeaderNav from "@/components/HeaderNav";
import Footer from "@/components/Footer";
import { cookies } from "next/headers";
import ParticleBackground from "@/components/ParticleBackground";
import StructuredData from "@/components/StructuredData";
import { getTranslation, Language } from "@/lib/translations";

export async function generateMetadata(): Promise<Metadata> {
  const cookieStore = cookies();
  const lang = (cookieStore.get("lang")?.value as Language) || "fr";
  const t = await getTranslation("legal", lang);
  return {
    title: t.metaTitle,
    description: t.metaDesc,
  };
}

export default async function LegalPage() {
  const cookieStore = cookies();
  const lang = (cookieStore.get("lang")?.value as Language) || "fr";
  const t = await getTranslation("legal", lang);

  return (
    <div className="min-h-screen bg-[#050505] text-white font-sans relative overflow-hidden p-8 lg:p-16">
      <StructuredData />
      <ParticleBackground />

      {/* MASSIVE WATERMARK LOGO */}
      <div className="absolute inset-0 pointer-events-none opacity-[0.05] z-0 flex items-center justify-center overflow-hidden">
        <img src="/logo01.png" alt="Watermark" className="w-full h-full object-cover max-w-[800px] max-h-[800px] opacity-40 drop-shadow-[0_0_100px_rgba(52,211,153,0.3)] scale-110" />
      </div>

      <div className="absolute top-[-10%] left-[-10%] w-[50%] h-[50%] bg-emerald-500/10 rounded-full blur-[120px] pointer-events-none" />
      <div className="absolute bottom-[-10%] right-[-10%] w-[50%] h-[50%] bg-cyan-500/10 rounded-full blur-[120px] pointer-events-none" />

      <div className="max-w-4xl mx-auto relative z-10">
        <header className="w-full border border-white/10 bg-[#050505]/40 backdrop-blur-md flex items-center justify-between py-4 px-8 z-50 mb-12 rounded-2xl">
          <Link href="/" className="flex items-center gap-3 hover:opacity-80 transition-opacity">
            <div className="w-8 h-8 rounded-full bg-[#0f0f11] border border-white/10 ring-1 ring-emerald-500/20 overflow-hidden flex items-center justify-center">
              <img src="/logo01.png" alt="Synapse Proxy Icon" className="w-[150%] h-[150%] object-cover max-w-none translate-y-1" />
            </div>
            <span className="font-bold tracking-tight text-white">Synapse Proxy</span>
          </Link>
          <HeaderNav />
          <div className="flex items-center gap-3">
            <Link href="/" className="px-4 py-2 rounded-xl bg-emerald-500 hover:bg-emerald-400 transition-all text-xs font-bold text-black shadow-[0_0_15px_rgba(16,185,129,0.2)]">
              {t.dashboardBtn}
            </Link>
          </div>
        </header>

        <div className="mb-16">
          <div className="inline-block px-3 py-1 rounded-full bg-white/5 border border-white/10 text-xs font-bold text-gray-400 uppercase tracking-wider mb-6">
            {t.heroBadge}
          </div>
          <h1 className="text-4xl lg:text-5xl font-black tracking-tight text-transparent bg-clip-text bg-gradient-to-r from-emerald-400 via-teal-300 to-cyan-500 mb-6">
            {t.heroTitle}
          </h1>
          <p className="text-gray-400 text-lg leading-relaxed">
            {t.heroDesc}
          </p>
        </div>

        <div className="space-y-12 bg-[#0f0f11]/60 border border-white/5 rounded-3xl p-8 lg:p-12 backdrop-blur-xl mb-16">
          {t.sections.map((sec, idx) => (
            <div key={idx} className="border-b border-white/5 last:border-b-0 pb-8 last:pb-0">
              <h2 className="text-xl font-bold mb-4 text-emerald-400">{sec.title}</h2>
              <p className="text-gray-300 text-sm leading-relaxed mb-4 whitespace-pre-wrap">{sec.desc}</p>
              {sec.items && sec.items.length > 0 && (
                <ul className="list-disc pl-6 space-y-2 text-gray-400 text-sm">
                  {sec.items.map((item, i) => (
                    <li key={i}>{item}</li>
                  ))}
                </ul>
              )}
            </div>
          ))}
        </div>
      </div>
      <Footer />
    </div>
  );
}
