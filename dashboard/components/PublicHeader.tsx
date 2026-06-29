"use client";

import Link from "next/link";
import HeaderNav from "./HeaderNav";

interface PublicHeaderProps {
  lang: "fr" | "en";
  dashboardBtnText?: string;
  showVersion?: boolean;
  floating?: boolean;
}

export default function PublicHeader({ lang, dashboardBtnText, showVersion = false, floating = true }: PublicHeaderProps) {
  const defaultBtnText = lang === "fr" ? "Tableau de Bord" : "Dashboard";
  const btnText = dashboardBtnText || defaultBtnText;

  const headerContent = (
    <header className={`flex justify-between items-center border border-white/10 rounded-2xl backdrop-blur-xl relative z-50 ${
      floating 
        ? "bg-[#050505]/80 p-4 shadow-2xl" 
        : "w-full bg-[#050505]/40 py-4 px-8 mb-12"
    }`}>
      <Link href="/" className="flex items-center gap-3 group">
        <div className={`rounded-xl bg-[#0f0f11] border border-white/10 ring-1 ring-emerald-500/20 overflow-hidden flex items-center justify-center transition-all ${
          floating 
            ? "w-10 h-10 shadow-[0_0_20px_rgba(52,211,153,0.2)] group-hover:shadow-[0_0_30px_rgba(52,211,153,0.4)]" 
            : "w-8 h-8"
        }`}>
          <img 
            src="/logo01.png" 
            alt="Synapse Proxy Icon" 
            className={`object-cover max-w-none ${
              floating ? "w-[150%] h-[150%] translate-y-1.5" : "w-[150%] h-[150%] translate-y-1"
            }`} 
          />
        </div>
        <div>
          <h1 className={`font-bold tracking-tight text-white group-hover:text-emerald-400 transition-colors leading-none ${
            floating ? "text-xl" : "text-base"
          }`}>
            Synapse Proxy
          </h1>
          <p className="text-gray-500 text-[10px] mt-1 hidden sm:block">Intelligent LLM Gateway</p>
        </div>
      </Link>
      
      <HeaderNav />
      
      <div className="flex items-center gap-3">
        {showVersion ? (
          <span className="text-xs text-gray-500 font-bold px-3 py-1.5 bg-[#0f0f11] border border-white/5 rounded-xl">
            v1.1.0
          </span>
        ) : (
          <Link 
            href="/" 
            className="px-4 py-2 rounded-xl bg-emerald-500 hover:bg-emerald-400 transition-all text-xs font-bold text-black shadow-[0_0_15px_rgba(16,185,129,0.2)]"
          >
            {btnText}
          </Link>
        )}
      </div>
    </header>
  );

  if (floating) {
    return (
      <div className="fixed top-0 left-0 right-0 z-50 p-4 pointer-events-none">
        <div className="max-w-7xl mx-auto pointer-events-auto">
          {headerContent}
        </div>
      </div>
    );
  }

  return headerContent;
}
