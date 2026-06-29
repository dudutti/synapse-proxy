"use client";

import { useState, useEffect, Suspense } from "react";
import { signIn } from "next-auth/react";
import { useRouter, useSearchParams } from "next/navigation";
import Link from "next/link";
import { motion } from "framer-motion";
import { Sparkles, ArrowRight, CheckCircle2 } from "lucide-react";
import { toast } from "sonner";
import ParticleBackground from "@/components/ParticleBackground";
import TelemetryGlobe from "@/components/TelemetryGlobe";
import { LineChart, Line, PieChart, Pie, Cell, XAxis, Tooltip, ResponsiveContainer } from "recharts";
import PublicHeader from "@/components/PublicHeader";

function LoginContent() {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [isLoading, setIsLoading] = useState(false);
  const [isConfigLoading, setIsConfigLoading] = useState(true);
  const [registrationOpen, setRegistrationOpen] = useState(true);
  
  const [waitlistEmail, setWaitlistEmail] = useState("");
  const [isWaitlistLoading, setIsWaitlistLoading] = useState(false);
  const [joinedWaitlist, setJoinedWaitlist] = useState(false);
  const [lang, setLang] = useState<"fr" | "en">("fr");

  const router = useRouter();
  const searchParams = useSearchParams();

  const isVerified = searchParams.get("verified") === "true";

  const [globalStats, setGlobalStats] = useState<any>(null);

  useEffect(() => {
    fetch("/api/config")
      .then(res => res.json())
      .then(data => {
        setRegistrationOpen(data.registrationOpen);
        setIsConfigLoading(false);
      })
      .catch(() => setIsConfigLoading(false));

    const fetchStats = () => {
      fetch("/api/public/global-stats")
        .then(res => {
          if (!res.ok) throw new Error("API Error");
          return res.json();
        })
        .then(data => {
          if (data && typeof data.totalCostSaved === 'number') {
            setGlobalStats(data);
          }
        })
        .catch(() => {});
    };
    
    fetchStats();
    const interval = setInterval(fetchStats, 10000); // refresh every 10s
    
    // Read language cookie
    if (typeof document !== "undefined") {
      const match = document.cookie.match(/(?:^|; )lang=([^;]*)/);
      if (match) setLang(match[1] as "fr" | "en");
    }

    return () => clearInterval(interval);
  }, []);

  const t = {
    fr: {
      welcome: "Bon retour",
      loginSub: "Connectez-vous pour gérer votre espace Synapse Proxy",
      email: "Adresse Email",
      pass: "Mot de passe",
      forgot: "Oublié ?",
      signin: "Se connecter",
      auth: "Authentification...",
      noAccount: "Pas encore de compte ?",
      createOne: "Créer un compte",
      earlyAccess: "Envie d'un accès anticipé ?",
      joinWaitlist: "Rejoignez la liste d'attente pour être notifié de l'ouverture.",
      join: "Rejoindre",
      firewallTitle: "Le Pare-feu Agentique",
      firewallDesc: "Les agents autonomes sont incroyables, jusqu'à ce qu'ils tombent dans une boucle infinie un vendredi soir et brûlent 5 000 $ de crédits OpenAI. Synapse Proxy agit comme un Coupe-circuit (Kill Switch). Il intercepte les agents en boucle et leur envoie un prompt d'auto-correction avant qu'ils ne vous ruinent.",
      globalOps: "Opérations Globales",
      totalApi: "Total des Requêtes API Traitées",
      savedCredits: "Crédits API Sauvés des Boucles",
      dollarsSaved: "Dollars Économisés Globalement",
      tokensPurged: "Tokens Purgés",
      tokensSent: "Tokens Envoyés (Non-optimisés)",
      traffic: "Trafic Global (24h)",
      models: "Top Modèles",
    },
    en: {
      welcome: "Welcome Back",
      loginSub: "Log in to manage your Synapse Proxy workspace",
      email: "Email Address",
      pass: "Password",
      forgot: "Forgot?",
      signin: "Sign In",
      auth: "Authenticating...",
      noAccount: "Don't have an account?",
      createOne: "Create one",
      earlyAccess: "Want early access?",
      joinWaitlist: "Join the waitlist to be notified when we open.",
      join: "Join",
      firewallTitle: "The Agentic Firewall",
      firewallDesc: "Autonomous AI agents are amazing, right up until they fall into an infinite loop on Friday night and burn $5,000 in OpenAI credits. Synapse Proxy acts as a Kill Switch. It intercepts looping agents and sends them a self-correction prompt before they bankrupt you.",
      globalOps: "Global Operations",
      totalApi: "Total API Requests Processed",
      savedCredits: "API Credits Saved from Agent Loops",
      dollarsSaved: "Dollars Saved Globally",
      tokensPurged: "Tokens Purged",
      tokensSent: "Tokens Sent (Unoptimized)",
      traffic: "Global Traffic (Last 24h)",
      models: "Top Models",
    }
  }[lang];

  const handleJoinWaitlist = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!waitlistEmail) return;
    setIsWaitlistLoading(true);
    const res = await fetch("/api/waitlist", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email: waitlistEmail })
    });
    setIsWaitlistLoading(false);
    if (res.ok) {
      toast.success("You're on the list!");
      setJoinedWaitlist(true);
    } else {
      toast.error("Failed to join waitlist");
    }
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setIsLoading(true);
    const res = await signIn("credentials", {
      email,
      password,
      redirect: false,
    });

    if (res?.ok) {
      toast.success("Welcome back!");
      router.push("/");
    } else {
      toast.error("Invalid credentials");
      setIsLoading(false);
    }
  };

  return (
    <div className="min-h-screen lg:grid lg:grid-cols-2 bg-[#050505] text-white font-sans relative overflow-hidden pt-20">
      <ParticleBackground />
      
      <PublicHeader lang={lang} showVersion={true} />
      
      {/* MASSIVE WATERMARK LOGO */}
      <div className="absolute inset-0 lg:w-1/2 pointer-events-none opacity-[0.15] z-0 flex items-center justify-center overflow-hidden">
        <img src="/logo01.png" alt="Watermark" className="w-full h-full object-cover drop-shadow-[0_0_100px_rgba(52,211,153,0.8)] scale-125" />
      </div>
      
      {/* LEFT SIDE: Auth */}
      <div className="flex items-center justify-center p-8 relative z-10 min-h-screen lg:min-h-0">
        <motion.div 
          initial={{ opacity: 0, x: -20 }}
          animate={{ opacity: 1, x: 0 }}
          transition={{ duration: 0.5 }}
          className="w-full max-w-md"
        >
        <div className="bg-black/20 border border-white/10 px-10 pt-10 pb-12 rounded-3xl backdrop-blur-lg shadow-2xl relative overflow-hidden">
          <div className="absolute top-0 right-0 w-64 h-64 bg-emerald-500/10 rounded-full blur-[80px] pointer-events-none" />
          
          <div className="flex flex-col items-center justify-center mb-8 relative z-10">
            <div className="w-24 h-24 rounded-full bg-[#0a0a0c] border border-white/10 shadow-[0_0_40px_rgba(52,211,153,0.5)] ring-2 ring-emerald-500/30 overflow-hidden flex items-center justify-center">
              {/* Translate-y moves the image physically down to center the icon */}
              <img src="/logo01.png" alt="Synapse Proxy Icon" className="w-[150%] h-[150%] object-cover max-w-none translate-y-3" />
            </div>
            <h1 className="mt-6 text-2xl font-black tracking-wide text-transparent bg-clip-text bg-gradient-to-r from-emerald-400 to-cyan-500">
              Synapse Proxy
            </h1>
          </div>
          
          <h2 className="text-3xl font-medium text-gray-100 text-center mb-2">{t.welcome}</h2>
          <p className="text-center text-gray-400 text-sm mb-8">{t.loginSub}</p>

          {isVerified && (
            <div className="mb-6 p-4 bg-emerald-500/10 border border-emerald-500/20 rounded-xl flex items-center gap-3 text-emerald-400 text-sm">
              <CheckCircle2 className="w-5 h-5 shrink-0" />
              <span>Email verified successfully! You can now log in.</span>
            </div>
          )}
          
          <form onSubmit={handleSubmit} className="space-y-5 relative z-10 pb-4">
            <div>
              <label className="block text-xs uppercase tracking-wider font-bold mb-2 text-gray-500">{t.email}</label>
              <input
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                className="w-full p-4 bg-white/5 border border-white/10 rounded-xl focus:border-emerald-500 focus:outline-none text-white transition-colors"
                placeholder="you@company.com"
                required
              />
            </div>

            <div>
              <div className="flex justify-between items-center mb-2">
                <label className="block text-xs uppercase tracking-wider font-bold text-gray-500">{t.pass}</label>
                <Link href="/forgot-password" className="text-xs text-emerald-400 hover:text-emerald-300 transition-colors">{t.forgot}</Link>
              </div>
              <input
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                className="w-full p-4 bg-white/5 border border-white/10 rounded-xl focus:border-emerald-500 focus:outline-none text-white transition-colors"
                placeholder="••••••••"
                required
              />
            </div>

            <button 
              type="submit" 
              disabled={isLoading}
              className="w-full bg-emerald-500 hover:bg-emerald-400 text-black font-black py-4 rounded-xl transition-all shadow-[0_0_20px_rgba(52,211,153,0.3)] hover:shadow-[0_0_30px_rgba(52,211,153,0.5)] disabled:opacity-50 disabled:hover:bg-emerald-500 flex items-center justify-center gap-2"
            >
              {isLoading ? t.auth : (
                <>{t.signin} <span className="text-lg">{"\u2192"}</span></>
              )}
            </button>
          </form>

          <p className="mt-8 text-center text-sm text-gray-400">
            {isConfigLoading ? (
               "Checking server status..."
            ) : registrationOpen ? (
               <>{t.noAccount} <Link href="/signup" className="text-white font-bold hover:text-emerald-400 transition-colors">{t.createOne}</Link></>
            ) : (
               <span className="text-amber-400 font-bold">Public registration is currently closed.</span>
            )}
          </p>

          {!isConfigLoading && !registrationOpen && (
            <div className="mt-6 pt-6 border-t border-white/10">
              <h3 className="text-center text-sm font-bold text-white mb-2">{t.earlyAccess}</h3>
              <p className="text-center text-xs text-gray-400 mb-4">{t.joinWaitlist}</p>
              {joinedWaitlist ? (
                <div className="p-3 bg-emerald-500/10 border border-emerald-500/20 rounded-xl flex flex-col items-center justify-center gap-2 text-emerald-400 text-sm">
                  <CheckCircle2 className="w-5 h-5 shrink-0" />
                  <span className="font-bold">Thanks! We'll be in touch.</span>
                </div>
              ) : (
                <form onSubmit={handleJoinWaitlist} className="flex gap-2 relative z-10">
                  <input
                    type="email"
                    value={waitlistEmail}
                    onChange={(e) => setWaitlistEmail(e.target.value)}
                    className="flex-1 p-3 bg-black/50 border border-white/10 rounded-xl focus:border-emerald-500 focus:outline-none text-white text-sm transition-colors"
                    placeholder="Email address"
                    required
                  />
                  <button 
                    type="submit" 
                    disabled={isWaitlistLoading}
                    className="bg-white/5 border border-white/10 hover:bg-white/10 text-white font-bold px-4 rounded-xl transition-all text-sm disabled:opacity-50"
                  >
                    {isWaitlistLoading ? "..." : t.join}
                  </button>
                </form>
              )}
            </div>
          )}
        </div>
      </motion.div>
      </div>

      {/* RIGHT SIDE: Global Telemetry */}
      <div className="hidden lg:flex flex-col items-center justify-center p-8 relative z-10 lg:border-l border-white/5 bg-gradient-to-br from-[#0a0a0a] to-[#050505]">
        <motion.div 
          initial={{ opacity: 0, x: 20 }}
          animate={{ opacity: 1, x: 0 }}
          transition={{ duration: 0.5, delay: 0.2 }}
          className="w-full max-w-5xl"
        >
          <div className="text-center mb-8">
            <h1 className="text-3xl font-black text-transparent bg-clip-text bg-gradient-to-r from-emerald-400 via-teal-300 to-cyan-500 mb-2">
              {t.firewallTitle}
            </h1>
            <p className="text-gray-400 text-sm max-w-2xl mx-auto leading-relaxed">
              {t.firewallDesc}
            </p>
          </div>

          <div className="bg-[#0f0f11] border border-white/5 rounded-3xl p-8 flex flex-col xl:flex-row items-center justify-between gap-8 relative overflow-hidden shadow-2xl">
            {/* Left Column: Requests & Cache */}
            <div className="w-full xl:flex-1 space-y-6 relative z-10 xl:pl-4 text-center xl:text-left">
              <div>
                <h3 className="text-emerald-400 text-xs font-black uppercase tracking-widest mb-1">{t.globalOps}</h3>
                <div className="text-5xl font-black text-white">{globalStats ? globalStats.totalRequests.toLocaleString() : "..."}</div>
                <div className="text-xs text-gray-500 mt-1">{t.totalApi}</div>
              </div>

              {globalStats && (
                <div className="space-y-4 pt-4">
                  <div>
                    <div className="flex justify-between text-[10px] font-bold text-gray-400 mb-1">
                      <span>L1 (Exact)</span>
                      <span className="text-white">{globalStats.cacheDistribution?.L1 || 0}</span>
                    </div>
                    <div className="h-1.5 bg-white/5 rounded-full overflow-hidden">
                      <div 
                        className="h-full bg-emerald-400" 
                        style={{ width: `${Math.max(2, (globalStats.cacheDistribution?.L1 / globalStats.totalRequests) * 100)}%` }} 
                      />
                    </div>
                  </div>
                  <div>
                    <div className="flex justify-between text-[10px] font-bold text-gray-400 mb-1">
                      <span>L2 (Semantic)</span>
                      <span className="text-white">{globalStats.cacheDistribution?.L2 || 0}</span>
                    </div>
                    <div className="h-1.5 bg-white/5 rounded-full overflow-hidden">
                      <div 
                        className="h-full bg-teal-400" 
                        style={{ width: `${Math.max(2, (globalStats.cacheDistribution?.L2 / globalStats.totalRequests) * 100)}%` }} 
                      />
                    </div>
                  </div>
                  <div>
                    <div className="flex justify-between text-[10px] font-bold text-gray-400 mb-1">
                      <span>L3 (Compression)</span>
                      <span className="text-white">{globalStats.cacheDistribution?.L3 || 0}</span>
                    </div>
                    <div className="h-1.5 bg-white/5 rounded-full overflow-hidden">
                      <div 
                        className="h-full bg-cyan-400" 
                        style={{ width: `${Math.max(2, (globalStats.cacheDistribution?.L3 / globalStats.totalRequests) * 100)}%` }} 
                      />
                    </div>
                  </div>
                </div>
              )}
            </div>

            {/* Center Column: Globe */}
            <div className="w-[300px] h-[300px] xl:w-[400px] xl:h-[400px] relative flex-shrink-0 flex items-center justify-center">
              <div className="absolute inset-0 bg-cyan-500/10 blur-[80px] rounded-full pointer-events-none" />
              <TelemetryGlobe />
            </div>

            {/* Right Column: Tokens & Cash */}
            <div className="w-full xl:flex-1 space-y-8 relative z-10 text-center xl:text-right xl:pr-4">
              <div>
                <h3 className="text-amber-400 text-xs font-black uppercase tracking-widest mb-1">{t.savedCredits}</h3>
                <div className="text-5xl font-black text-amber-500 drop-shadow-[0_0_15px_rgba(245,158,11,0.3)]">
                  ${globalStats ? globalStats.totalCostSaved.toFixed(2) : "..."}
                </div>
                <div className="text-xs text-gray-500 mt-1">{t.dollarsSaved}</div>
              </div>

              <div className="border-t border-white/5 pt-6 space-y-6">
                <div>
                  <div className="text-2xl font-black text-white">{globalStats ? globalStats.tokensPurged.toLocaleString() : "..."}</div>
                  <div className="text-[10px] font-bold text-gray-500 uppercase tracking-widest mt-1">{t.tokensPurged}</div>
                </div>
                <div>
                  <div className="text-xl font-bold text-gray-400">{globalStats ? globalStats.tokensSent.toLocaleString() : "..."}</div>
                  <div className="text-[10px] font-bold text-gray-600 uppercase tracking-widest mt-1">{t.tokensSent}</div>
                </div>
              </div>
            </div>
          </div>

          {/* CHARTS SECTION */}
          {globalStats && (
            <div className="mt-8 grid grid-cols-1 md:grid-cols-3 gap-6">
              {/* Activity Line Chart */}
              <div className="md:col-span-2 bg-[#0f0f11] border border-white/5 rounded-3xl p-6 shadow-xl">
                <h3 className="text-gray-400 text-xs font-bold uppercase tracking-widest mb-4">{t.traffic}</h3>
                <div className="h-48">
                  <ResponsiveContainer width="100%" height="100%">
                    <LineChart data={globalStats.hourlyActivity || []}>
                      <XAxis dataKey="hour" stroke="#333" fontSize={10} tickMargin={10} />
                      <Tooltip 
                        contentStyle={{ backgroundColor: '#000', borderColor: '#333', borderRadius: '12px', fontSize: '12px' }}
                        itemStyle={{ color: '#34d399' }}
                        cursor={{ stroke: '#333', strokeWidth: 1 }}
                      />
                      <Line type="monotone" dataKey="requests" name="Requests" stroke="#34d399" strokeWidth={3} dot={false} />
                    </LineChart>
                  </ResponsiveContainer>
                </div>
              </div>

              {/* Models Pie Chart */}
              <div className="bg-[#0f0f11] border border-white/5 rounded-3xl p-6 shadow-xl flex flex-col">
                <h3 className="text-gray-400 text-xs font-bold uppercase tracking-widest mb-2">{t.models}</h3>
                <div className="flex-1 relative">
                  <ResponsiveContainer width="100%" height="100%">
                    <PieChart>
                      <Pie
                        data={globalStats.modelsDistribution || []}
                        dataKey="count"
                        nameKey="model"
                        cx="50%"
                        cy="50%"
                        innerRadius={45}
                        outerRadius={70}
                        stroke="none"
                      >
                        {(globalStats.modelsDistribution || []).map((entry: any, index: number) => (
                          <Cell key={`cell-${index}`} fill={['#34d399', '#2dd4bf', '#a855f7', '#3b82f6', '#f59e0b', '#ec4899'][index % 6]} />
                        ))}
                      </Pie>
                      <Tooltip 
                        contentStyle={{ backgroundColor: '#000', borderColor: '#333', borderRadius: '12px', fontSize: '12px' }}
                        itemStyle={{ color: '#fff' }}
                      />
                    </PieChart>
                  </ResponsiveContainer>
                </div>
              </div>
            </div>
          )}

        </motion.div>
      </div>

    </div>
  );
}

export default function LoginPage() {
  return (
    <Suspense fallback={<div className="min-h-screen bg-[#050505] text-white flex items-center justify-center">Loading...</div>}>
      <LoginContent />
    </Suspense>
  );
}
