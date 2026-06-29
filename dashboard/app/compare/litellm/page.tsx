import type { Metadata } from "next";
import Link from "next/link";
import { Activity, Play, Check } from "lucide-react";
import DemoVideo from "@/components/DemoVideo";
import PublicHeader from "@/components/PublicHeader";
import Footer from "@/components/Footer";
import { cookies } from "next/headers";
import ParticleBackground from "@/components/ParticleBackground";
import StructuredData from "@/components/StructuredData";
import { getTranslation, Language } from "@/lib/translations";

export async function generateMetadata(): Promise<Metadata> {
  const cookieStore = cookies();
  const lang = (cookieStore.get("lang")?.value as Language) || "fr";
  const t = await getTranslation("litellm", lang);
  return {
    title: t.metaTitle,
    description: t.metaDesc,
  };
}

export default async function CompareLiteLlmPage() {
  const cookieStore = cookies();
  const lang = (cookieStore.get("lang")?.value as Language) || "fr";
  const t = await getTranslation("litellm", lang);

  return (
    <div className="min-h-screen bg-[#050505] text-white font-sans relative overflow-hidden p-8 lg:p-16">
      <StructuredData />
      <ParticleBackground />
      
      {/* MASSIVE WATERMARK LOGO */}
      <div className="absolute inset-0 pointer-events-none opacity-[0.05] z-0 flex items-center justify-center overflow-hidden">
        <img src="/logo01.png" alt="Watermark" className="w-full h-full object-cover max-w-[800px] max-h-[800px] opacity-40 drop-shadow-[0_0_100px_rgba(52,211,153,0.3)] scale-110" />
      </div>

      <div className="absolute top-[-10%] left-[-10%] w-[50%] h-[50%] bg-purple-500/10 rounded-full blur-[120px] pointer-events-none" />
      <div className="absolute bottom-[-10%] right-[-10%] w-[50%] h-[50%] bg-emerald-500/10 rounded-full blur-[120px] pointer-events-none" />

      <div className="max-w-6xl mx-auto relative z-10">
        <PublicHeader lang={lang} floating={false} />

        {/* Hero */}
        <div className="text-center mb-16">
          <div className="w-16 h-16 mx-auto rounded-2xl bg-purple-500/10 border border-purple-500/20 flex items-center justify-center text-purple-400 mb-6 shadow-[0_0_30px_rgba(168,85,247,0.2)]">
            <Activity className="w-8 h-8" />
          </div>
          <h1 className="text-4xl lg:text-6xl font-black tracking-tight text-transparent bg-clip-text bg-gradient-to-r from-purple-400 via-emerald-300 to-purple-500 mb-6">
            {t.heroTitle}
          </h1>
          <p className="text-gray-400 text-lg lg:text-xl max-w-3xl mx-auto leading-relaxed">
            {t.heroDesc}
          </p>
        </div>

        {/* Feature Comparison Table */}
        {t.table && (
          <div className="bg-[#0f0f11]/60 border border-white/5 rounded-3xl p-8 backdrop-blur-xl mb-16 overflow-x-auto shadow-2xl">
            <table className="w-full text-left border-collapse min-w-[500px]">
              <thead>
                <tr className="border-b border-white/10 text-xs uppercase tracking-wider text-gray-400">
                  <th className="pb-4">{t.table.headers[0]}</th>
                  <th className="pb-4 text-emerald-400 font-bold">{t.table.headers[1]}</th>
                  <th className="pb-4 text-gray-500">{t.table.headers[2]}</th>
                </tr>
              </thead>
              <tbody className="text-sm divide-y divide-white/5">
                {t.table.rows.map((row, idx) => (
                  <tr key={idx}>
                    <td className="py-4 font-medium text-gray-200">{row.feature}</td>
                    <td className="py-4 text-emerald-400 font-bold flex items-center gap-1.5">
                      <Check className="w-4 h-4" /> {row.synapse}
                    </td>
                    <td className="py-4 text-gray-500">{row.other}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}

        {/* Video Demo */}
        <div className="bg-[#0f0f11]/40 border border-white/5 rounded-3xl p-8 shadow-2xl relative overflow-hidden mb-16">
          <div className="absolute top-0 left-0 w-64 h-64 bg-purple-500/5 rounded-full blur-[100px] pointer-events-none" />
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-8 items-center">
            <div>
              <h3 className="text-2xl font-bold mb-4 flex items-center gap-2">
                <Play className="w-6 h-6 text-purple-400" /> {t.videoTitle}
              </h3>
              <p className="text-gray-400 text-sm leading-relaxed mb-6">
                {t.videoDesc}
              </p>
              <div className="bg-black/40 border border-white/5 rounded-xl p-4 text-xs font-mono text-gray-300">
                {t.videoConsoleItems?.map((item, i) => (
                  <div key={i}>{item}</div>
                ))}
              </div>
            </div>
            <div className="relative rounded-2xl overflow-hidden border border-white/10 bg-black/60 aspect-video flex items-center justify-center">
              <DemoVideo src={t.videoUrl || "/playground_cache_hits.webp"} alt={t.videoAlt} />
            </div>
          </div>
        </div>
      </div>
      <Footer />
    </div>
  );
}
