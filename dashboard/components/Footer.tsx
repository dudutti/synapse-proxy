import Link from "next/link";

export default function Footer() {
  const currentYear = new Date().getFullYear();

  return (
    <footer className="w-full mt-24 border-t border-white/10 bg-[#050505]/60 backdrop-blur-md pt-16 pb-8 text-gray-400">
      <div className="max-w-6xl mx-auto px-4 grid grid-cols-1 md:grid-cols-4 gap-8 mb-12">
        {/* Brand Column */}
        <div className="space-y-4">
          <div className="flex items-center gap-3">
            <div className="w-8 h-8 rounded-full bg-[#0f0f11] border border-white/10 ring-1 ring-emerald-500/20 overflow-hidden flex items-center justify-center">
              <img src="/logo01.png" alt="Synapse Proxy Icon" className="w-[150%] h-[150%] object-cover max-w-none translate-y-1" />
            </div>
            <span className="font-bold tracking-tight text-white text-lg">Synapse Proxy</span>
          </div>
          <p className="text-xs text-gray-500 leading-relaxed">
            Passerelle réseau d'IA souveraine pour la gestion de cache sémantique, le pruning de contexte et le pare-feu d'agents autonomes.
          </p>
        </div>

        {/* Column 2: Fonctionnalités */}
        <div>
          <h4 className="text-white font-bold text-sm mb-4 uppercase tracking-wider">Fonctionnalités</h4>
          <ul className="space-y-2 text-xs">
            <li>
              <Link href="/features/caching" className="hover:text-emerald-400 transition-colors">
                Cache Multi-Niveaux (L1/L2/L3)
              </Link>
            </li>
            <li>
              <Link href="/features/firewall" className="hover:text-emerald-400 transition-colors">
                Agentic Firewall (Anti-Boucle)
              </Link>
            </li>
            <li>
              <Link href="/features/compression" className="hover:text-emerald-400 transition-colors">
                Compression de Contexte
              </Link>
            </li>
            <li>
              <Link href="/features/mcp" className="hover:text-emerald-400 transition-colors">
                Serveur MCP Intégré
              </Link>
            </li>
          </ul>
        </div>

        {/* Column 3: Solutions */}
        <div>
          <h4 className="text-white font-bold text-sm mb-4 uppercase tracking-wider">Solutions</h4>
          <ul className="space-y-2 text-xs">
            <li>
              <Link href="/use-cases/cost-reduction" className="hover:text-emerald-400 transition-colors">
                Réduction Coûts Startups
              </Link>
            </li>
            <li>
              <Link href="/use-cases/agent-safety" className="hover:text-emerald-400 transition-colors">
                Sécurité des Agents IA
              </Link>
            </li>
            <li>
              <Link href="/use-cases/enterprise-gateway" className="hover:text-emerald-400 transition-colors">
                Passerelle Souveraine Entreprise
              </Link>
            </li>
          </ul>
        </div>

        {/* Column 4: Comparatifs */}
        <div>
          <h4 className="text-white font-bold text-sm mb-4 uppercase tracking-wider">Comparatifs</h4>
          <ul className="space-y-2 text-xs">
            <li>
              <Link href="/compare/litellm" className="hover:text-emerald-400 transition-colors">
                vs LiteLLM
              </Link>
            </li>
            <li>
              <Link href="/compare/portkey" className="hover:text-emerald-400 transition-colors">
                vs Portkey.ai
              </Link>
            </li>
            <li>
              <Link href="/compare/llmlingua" className="hover:text-emerald-400 transition-colors">
                vs LLMLingua
              </Link>
            </li>
          </ul>
        </div>
      </div>

      {/* Bottom bar */}
      <div className="max-w-6xl mx-auto px-4 pt-8 border-t border-white/5 flex flex-col md:flex-row items-center justify-between gap-4 text-xs text-gray-500">
        <div>
          &copy; {currentYear} <a href="https://synapse-proxy.com" className="hover:text-white transition-colors">synapse-proxy.com</a>. Tous droits réservés.
        </div>
        <div className="flex gap-6">
          <Link href="/cgv" className="hover:text-white transition-colors">CGV / CGU</Link>
          <Link href="/privacy" className="hover:text-white transition-colors">Politique de Confidentialité</Link>
          <Link href="/legal" className="hover:text-white transition-colors">Mentions Légales</Link>
        </div>
      </div>
    </footer>
  );
}
