import { cookies } from "next/headers";
import Link from "next/link";
import { Check, ArrowRight } from "lucide-react";
import PublicHeader from "@/components/PublicHeader";
import ParticleBackground from "@/components/ParticleBackground";
import { prisma } from "@/lib/prisma";
import { translations, TranslationItem, Language } from "@/lib/translations";

export const metadata = {
  title: "Tarifs — Synapse Proxy",
  description: "Choisissez le plan adapté à votre utilisation de Synapse Proxy.",
};

export default async function PricingPage() {
  const cookieStore = cookies();
  const lang = (cookieStore.get("lang")?.value || "fr") as Language;
  
  let t: TranslationItem;
  try {
    const record = await prisma.landingPageContent.findUnique({
      where: { pageKey_lang: { pageKey: "pricing", lang } }
    });
    if (record) {
      t = JSON.parse(record.content);
    } else {
      t = translations["pricing"][lang];
    }
  } catch (err) {
    t = translations["pricing"][lang];
  }

  return (
    <div className="min-h-screen bg-[#050505] text-white font-sans relative overflow-hidden pt-20">
      <ParticleBackground />
      
      <PublicHeader lang={lang} />

      <div className="relative z-10 max-w-6xl mx-auto px-6 pt-20 pb-24">
        <div className="text-center mb-16">
          <h1 className="text-4xl md:text-5xl font-black text-transparent bg-clip-text bg-gradient-to-r from-emerald-400 via-teal-300 to-cyan-500 mb-4">
            {t.heroTitle}
          </h1>
          <p className="text-gray-400 text-lg max-w-2xl mx-auto">
            {t.heroDesc}
          </p>
        </div>

        <div className="grid md:grid-cols-3 gap-8">
          {t.sections.map((tier, i) => (
            <div 
              key={i}
              className={`relative bg-[#0a0a0c] rounded-3xl border p-8 flex flex-col transition-all duration-300 ${
                tier.highlight 
                  ? "border-emerald-500/50 shadow-[0_0_30px_rgba(52,211,153,0.15)] scale-105 z-10" 
                  : "border-white/10 hover:border-white/20 hover:bg-white/[0.02]"
              }`}
            >
              {tier.highlight && (
                <div className="absolute -top-3 left-1/2 -translate-x-1/2 px-3 py-1 bg-emerald-500 text-black text-xs font-black uppercase tracking-wider rounded-full shadow-[0_0_15px_rgba(52,211,153,0.5)]">
                  Most Popular
                </div>
              )}
              
              <div className="mb-6">
                <h3 className="text-xl font-bold text-white mb-2">{tier.title}</h3>
                <div className="flex items-baseline gap-1 mb-2">
                  {tier.price !== "Sur mesure" && tier.price !== "Custom" && <span className="text-xl text-gray-500 font-bold">$</span>}
                  <span className="text-4xl font-black text-white">{tier.price}</span>
                  {tier.price !== "Sur mesure" && tier.price !== "Custom" && <span className="text-gray-500">/mo</span>}
                </div>
                <p className="text-sm text-gray-400 min-h-[40px]">{tier.desc}</p>
              </div>

              <div className="flex-1 space-y-4 mb-8">
                {tier.items.map((feat, j) => (
                  <div key={j} className="flex items-start gap-3">
                    <div className={`mt-0.5 w-4 h-4 rounded-full flex items-center justify-center shrink-0 ${tier.highlight ? "bg-emerald-500/20 text-emerald-400" : "bg-white/5 text-gray-400"}`}>
                      <Check className="w-2.5 h-2.5" />
                    </div>
                    <span className="text-sm text-gray-300">{feat}</span>
                  </div>
                ))}
              </div>

              <Link 
                href={tier.href || "/signup"}
                className={`w-full py-3 rounded-xl font-bold text-sm flex items-center justify-center gap-2 transition-all ${
                  tier.highlight 
                    ? "bg-emerald-500 text-black hover:bg-emerald-400 shadow-[0_0_20px_rgba(52,211,153,0.3)] hover:shadow-[0_0_30px_rgba(52,211,153,0.5)]" 
                    : "bg-white/5 text-white hover:bg-white/10 border border-white/10"
                }`}
              >
                {tier.cta || "Commencer"}
                <ArrowRight className="w-4 h-4" />
              </Link>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
