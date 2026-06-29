import type { Metadata } from "next";
import Link from "next/link";
import { Layers, Play, Check, Terminal } from "lucide-react";
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
  const t = await getTranslation("mcp", lang);
  return {
    title: t.metaTitle,
    description: t.metaDesc,
  };
}

export default async function McpPage() {
  const cookieStore = cookies();
  const lang = (cookieStore.get("lang")?.value as Language) || "fr";
  const t = await getTranslation("mcp", lang);

  return (
    <div className="min-h-screen bg-[#050505] text-white font-sans relative overflow-hidden p-8 lg:p-16">
      <StructuredData />
      <ParticleBackground />
      
      {/* MASSIVE WATERMARK LOGO */}
      <div className="absolute inset-0 pointer-events-none opacity-[0.05] z-0 flex items-center justify-center overflow-hidden">
        <img src="/logo01.png" alt="Watermark" className="w-full h-full object-cover max-w-[800px] max-h-[800px] opacity-40 drop-shadow-[0_0_100px_rgba(52,211,153,0.3)] scale-110" />
      </div>

      <div className="absolute top-[-10%] left-[-10%] w-[50%] h-[50%] bg-indigo-500/10 rounded-full blur-[120px] pointer-events-none" />
      <div className="absolute bottom-[-10%] right-[-10%] w-[50%] h-[50%] bg-violet-500/10 rounded-full blur-[120px] pointer-events-none" />

      <div className="max-w-6xl mx-auto relative z-10">
        <PublicHeader lang={lang} floating={false} />

        {/* Hero */}
        <div className="text-center mb-16">
          <div className="w-16 h-16 mx-auto rounded-2xl bg-indigo-500/10 border border-indigo-500/20 flex items-center justify-center text-indigo-400 mb-6 shadow-[0_0_30px_rgba(99,102,241,0.2)]">
            <Layers className="w-8 h-8" />
          </div>
          <h1 className="text-4xl lg:text-6xl font-black tracking-tight text-transparent bg-clip-text bg-gradient-to-r from-indigo-400 via-violet-300 to-indigo-500 mb-6">
            {t.heroTitle}
          </h1>
          <p className="text-gray-400 text-lg lg:text-xl max-w-3xl mx-auto leading-relaxed">
            {t.heroDesc}
          </p>
        </div>

        {/* Technical breakdown */}
        <div className="grid grid-cols-1 md:grid-cols-2 gap-8 mb-16">
          {t.sections.map((sec, idx) => (
            <div key={idx} className="bg-[#0f0f11]/60 border border-white/5 rounded-2xl p-8 backdrop-blur-xl">

              {sec.mediaUrl && (
                <div className={`mb-6 overflow-hidden flex items-center justify-center ${
                  sec.mediaSize === 'full' ? '-mx-8 -mt-8 mb-6 rounded-none aspect-video' : 
                  sec.mediaSize === 'large' ? 'rounded-xl aspect-video w-full' : 
                  sec.mediaSize === 'small' ? 'rounded-xl w-16 h-16 mb-4' : 
                  'rounded-xl aspect-video w-full max-w-[200px] mx-auto'
                }`}>
                  {sec.mediaUrl.endsWith('.mp4') ? (
                    <video src={sec.mediaUrl} autoPlay loop muted playsInline className="w-full h-full object-cover" />
                  ) : (
                    <img src={sec.mediaUrl} alt={sec.title} className="w-full h-full object-cover" />
                  )}
                </div>
              )}

              <h3 className="text-xl font-bold mb-4 flex items-center gap-2">
                <Terminal className="w-5 h-5 text-indigo-400" /> {sec.title}
              </h3>
              <p className="text-gray-400 text-sm leading-relaxed mb-6">
                {sec.desc}
              </p>
              <ul className="space-y-2 text-xs text-gray-300">
                {sec.items.map((item, i) => (
                  <li key={i} className="flex items-center gap-2">
                    <Check className="w-3.5 h-3.5 text-indigo-400" /> {item}
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>

        {/* Video Demo */}
        <div className="bg-[#0f0f11]/40 border border-white/5 rounded-3xl p-8 shadow-2xl relative overflow-hidden mb-16">
          <div className="absolute top-0 left-0 w-64 h-64 bg-indigo-500/5 rounded-full blur-[100px] pointer-events-none" />
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-8 items-center">
            <div>
              <h3 className="text-2xl font-bold mb-4 flex items-center gap-2">
                <Play className="w-6 h-6 text-indigo-400" /> {t.videoTitle}
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
              <DemoVideo src={t.videoUrl || "/mcp_agent_interaction.webp"} alt={t.videoAlt} />
            </div>
          </div>
        </div>
      </div>
      <Footer />
    </div>
  );
}
