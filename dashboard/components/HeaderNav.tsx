"use client";

import { useState, useEffect } from "react";
import Link from "next/link";
import { ChevronDown, Sparkles, Shield, Database, Cpu, Activity, KeyRound, Layers, Menu, X } from "lucide-react";

export default function HeaderNav() {
  const [activeDropdown, setActiveDropdown] = useState<string | null>(null);
  const [isMobileMenuOpen, setIsMobileMenuOpen] = useState(false);
  const [lang, setLang] = useState<"fr" | "en">(() => {
    if (typeof window !== "undefined") {
      const match = document.cookie.match(/(?:^|; )lang=([^;]*)/);
      return (match ? match[1] : "fr") as "fr" | "en";
    }
    return "fr";
  });

  const toggleLanguage = (newLang: "fr" | "en") => {
    document.cookie = `lang=${newLang}; path=/; max-age=31536000`;
    setLang(newLang);
    window.location.reload();
  };

  const menu = {
    features: {
      label: lang === "fr" ? "Fonctionnalités" : "Features",
      items: [
        { label: lang === "fr" ? "Cache Multi-Niveaux" : "Multi-Level Caching", href: "/features/caching", desc: lang === "fr" ? "Cache L1/L2/L3 & recherche sémantique locale" : "L1/L2/L3 cache & local semantic search", icon: Database },
        { label: lang === "fr" ? "Pare-feu Agentique" : "Agentic Firewall", href: "/features/firewall", desc: lang === "fr" ? "Détection de boucle & injection d'auto-correction" : "Loop detection & self-correction injection", icon: Shield },
        { label: lang === "fr" ? "Compression de Contexte" : "Context Compression", href: "/features/compression", desc: lang === "fr" ? "Optimisation & pruning de tokens intelligents" : "Token optimization & smart pruning", icon: Cpu },
        { label: lang === "fr" ? "Serveur MCP" : "MCP Server", href: "/features/mcp", desc: lang === "fr" ? "Intégration d'outils Cursor & Claude Desktop" : "Cursor & Claude Desktop tools integration", icon: Layers },
      ]
    },
    useCases: {
      label: "Solutions",
      items: [
        { label: lang === "fr" ? "Réduction des Coûts" : "Cost Reduction", href: "/use-cases/cost-reduction", desc: lang === "fr" ? "Économies intelligentes pour startups innovantes" : "Smart savings for innovative startups", icon: Sparkles },
        { label: lang === "fr" ? "Sécurité des Agents" : "Agent Safety", href: "/use-cases/agent-safety", desc: lang === "fr" ? "Auto-correction de boucles & logs d'audit" : "Loop self-recovery & audit logs", icon: Shield },
        { label: lang === "fr" ? "Passerelle Entreprise" : "Enterprise Gateway", href: "/use-cases/enterprise-gateway", desc: lang === "fr" ? "Multi-tenant souverain, quotas & budgets" : "Sovereign multi-tenant, quotas & budgets", icon: KeyRound },
      ]
    },
    compare: {
      label: lang === "fr" ? "Comparatifs" : "Comparisons",
      items: [
        { label: "vs LiteLLM", href: "/compare/litellm", desc: lang === "fr" ? "Comparatif de cache sémantique et pare-feu" : "Semantic caching and firewall comparison", icon: Activity },
        { label: "vs Portkey", href: "/compare/portkey", desc: lang === "fr" ? "Déploiement souverain local vs SaaS Cloud" : "Local sovereign deployment vs SaaS Cloud", icon: Layers },
        { label: "vs LLMLingua", href: "/compare/llmlingua", desc: lang === "fr" ? "Compression gateway vs compression applicative" : "Gateway compression vs app-level compression", icon: Cpu },
      ]
    }
  };

  // Prevent scrolling when mobile menu is open
  useEffect(() => {
    if (isMobileMenuOpen) {
      document.body.style.overflow = "hidden";
    } else {
      document.body.style.overflow = "unset";
    }
    return () => {
      document.body.style.overflow = "unset";
    };
  }, [isMobileMenuOpen]);

  return (
    <>
      {/* Desktop Navigation */}
      <nav className="hidden lg:flex gap-6 items-center text-sm">
        {Object.entries(menu).map(([key, group]) => (
          <div
            key={key}
            className="relative group py-2"
            onMouseEnter={() => setActiveDropdown(key)}
            onMouseLeave={() => setActiveDropdown(null)}
          >
            <button className="flex items-center gap-1.5 text-gray-400 hover:text-white transition-all font-bold focus:outline-none">
              {group.label}
              <ChevronDown className={`w-3.5 h-3.5 transition-transform duration-300 ${activeDropdown === key ? 'rotate-180 text-emerald-400' : ''}`} />
            </button>
            
            {/* Dropdown Menu */}
            <div className="absolute top-full left-1/2 -translate-x-1/2 mt-2 w-80 bg-[#0c0c0e]/95 border border-white/10 rounded-2xl p-4 shadow-2xl backdrop-blur-xl opacity-0 translate-y-2 pointer-events-none group-hover:opacity-100 group-hover:translate-y-0 group-hover:pointer-events-auto transition-all duration-300 z-50 before:content-[''] before:absolute before:-top-2 before:left-0 before:right-0 before:h-2">
              <div className="space-y-1">
                {group.items.map((item) => {
                  const Icon = item.icon;
                  return (
                    <Link
                      key={item.href}
                      href={item.href}
                      className="flex gap-3 p-2.5 rounded-xl hover:bg-white/5 transition-all text-left group/item"
                    >
                      <div className="w-8 h-8 rounded-lg bg-white/5 border border-white/10 flex items-center justify-center text-gray-400 group-hover/item:border-emerald-500/30 group-hover/item:text-emerald-400 shrink-0">
                        <Icon className="w-4 h-4" />
                      </div>
                      <div>
                        <div className="text-xs font-bold text-gray-200 group-hover/item:text-emerald-400 transition-colors">
                          {item.label}
                        </div>
                        <div className="text-[10px] text-gray-400/70 mt-0.5 leading-snug">
                          {item.desc}
                        </div>
                      </div>
                    </Link>
                  );
                })}
              </div>
            </div>
          </div>
        ))}

        {/* Language Selector Desktop */}
        <div className="relative group py-2 border-l border-white/10 pl-6 ml-2">
          <button className="flex items-center gap-1.5 text-gray-400 hover:text-white transition-all font-bold focus:outline-none uppercase">
            {lang === "fr" ? "🇫🇷 FR" : "🇬🇧 EN"}
            <ChevronDown className="w-3 h-3 text-gray-500" />
          </button>
          <div className="absolute top-full right-0 mt-2 w-28 bg-[#0c0c0e]/95 border border-white/10 rounded-xl p-1 shadow-2xl backdrop-blur-xl opacity-0 translate-y-2 pointer-events-none group-hover:opacity-100 group-hover:translate-y-0 group-hover:pointer-events-auto transition-all duration-200 z-50 before:content-[''] before:absolute before:-top-2 before:left-0 before:right-0 before:h-2">
            <button 
              onClick={() => toggleLanguage("fr")}
              className={`w-full flex items-center gap-2 px-3 py-2 rounded-lg text-left text-xs font-bold transition-all hover:bg-white/5 ${lang === "fr" ? "text-emerald-400" : "text-gray-400"}`}
            >
              🇫🇷 Français
            </button>
            <button 
              onClick={() => toggleLanguage("en")}
              className={`w-full flex items-center gap-2 px-3 py-2 rounded-lg text-left text-xs font-bold transition-all hover:bg-white/5 ${lang === "en" ? "text-emerald-400" : "text-gray-400"}`}
            >
              🇬🇧 English
            </button>
          </div>
        </div>
      </nav>

      {/* Mobile Menu Toggle Button */}
      <button 
        className="lg:hidden p-2 text-gray-400 hover:text-white focus:outline-none z-50 relative"
        onClick={() => setIsMobileMenuOpen(!isMobileMenuOpen)}
      >
        {isMobileMenuOpen ? <X className="w-6 h-6" /> : <Menu className="w-6 h-6" />}
      </button>

      {/* Mobile Menu Overlay */}
      {isMobileMenuOpen && (
        <div className="fixed inset-0 bg-[#050505]/95 backdrop-blur-xl z-40 overflow-y-auto lg:hidden pt-20 pb-10 px-6 border-t border-white/5 mt-16">
          <div className="flex flex-col gap-8 max-w-sm mx-auto">
            {Object.entries(menu).map(([key, group]) => (
              <div key={key} className="space-y-4">
                <h3 className="text-sm font-bold text-gray-400 uppercase tracking-wider">{group.label}</h3>
                <div className="grid gap-3">
                  {group.items.map((item) => {
                    const Icon = item.icon;
                    return (
                      <Link
                        key={item.href}
                        href={item.href}
                        onClick={() => setIsMobileMenuOpen(false)}
                        className="flex gap-3 p-3 rounded-xl bg-white/5 border border-white/10 active:bg-white/10 transition-all text-left"
                      >
                        <div className="w-10 h-10 rounded-lg bg-black/50 border border-white/10 flex items-center justify-center text-emerald-400 shrink-0">
                          <Icon className="w-5 h-5" />
                        </div>
                        <div className="flex-1">
                          <div className="text-sm font-bold text-gray-200">
                            {item.label}
                          </div>
                          <div className="text-xs text-gray-500 mt-0.5 leading-snug">
                            {item.desc}
                          </div>
                        </div>
                      </Link>
                    );
                  })}
                </div>
              </div>
            ))}

            {/* Language Selector Mobile */}
            <div className="space-y-4 pt-6 border-t border-white/10">
              <h3 className="text-sm font-bold text-gray-400 uppercase tracking-wider">Language / Langue</h3>
              <div className="grid grid-cols-2 gap-3">
                <button 
                  onClick={() => toggleLanguage("fr")}
                  className={`flex items-center justify-center gap-2 p-3 rounded-xl border transition-all text-sm font-bold ${lang === "fr" ? "bg-emerald-500/10 border-emerald-500/30 text-emerald-400" : "bg-white/5 border-white/10 text-gray-400"}`}
                >
                  🇫🇷 Français
                </button>
                <button 
                  onClick={() => toggleLanguage("en")}
                  className={`flex items-center justify-center gap-2 p-3 rounded-xl border transition-all text-sm font-bold ${lang === "en" ? "bg-emerald-500/10 border-emerald-500/30 text-emerald-400" : "bg-white/5 border-white/10 text-gray-400"}`}
                >
                  🇬🇧 English
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
