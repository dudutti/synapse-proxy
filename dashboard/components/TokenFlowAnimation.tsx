"use client";

import { motion } from "framer-motion";

export default function TokenFlowAnimation({ 
  tokensIn = 10000, 
  tokensOut = 3000, 
  active = true 
}: { 
  tokensIn?: number; 
  tokensOut?: number; 
  active?: boolean;
}) {
  const savings = Math.max(0, tokensIn - tokensOut);
  const pctSaved = tokensIn > 0 ? Math.round((savings / tokensIn) * 100) : 0;

  // Particle variations
  const particles = Array.from({ length: 15 }).map((_, i) => ({
    id: i,
    delay: Math.random() * 2,
    duration: 1.5 + Math.random(),
    yOffset: (Math.random() - 0.5) * 40
  }));

  return (
    <div className="w-full relative h-48 flex items-center justify-between px-8 bg-black/20 rounded-3xl border border-white/5 overflow-hidden">
      
      {/* Left side: Incoming Tokens */}
      <div className="z-10 flex flex-col items-center">
        <div className="text-[10px] text-gray-500 uppercase tracking-widest font-bold mb-2">Original Request</div>
        <div className="w-16 h-16 rounded-2xl bg-zinc-800/50 border border-white/10 flex items-center justify-center relative overflow-hidden">
          <div className="text-xl font-bold text-gray-300 relative z-10">IN</div>
          {active && <motion.div animate={{ opacity: [0.1, 0.3, 0.1] }} transition={{ repeat: Infinity, duration: 2 }} className="absolute inset-0 bg-zinc-500/20" />}
        </div>
        <div className="text-xs font-mono text-zinc-400 mt-2">{tokensIn.toLocaleString()} tok</div>
      </div>

      {/* Middle: The Synapse Proxy Engine */}
      <div className="relative flex-1 h-full mx-8 flex items-center justify-center">
        {/* Connection paths */}
        <div className="absolute inset-0 flex items-center justify-center w-full h-full opacity-30 pointer-events-none">
          <svg width="100%" height="100%" preserveAspectRatio="none" viewBox="0 0 100 100">
            <path d="M 0,50 C 30,50 30,50 50,50 C 70,50 70,50 100,50" fill="none" stroke="url(#gradient)" strokeWidth="0.5" strokeDasharray="2,2" />
            <defs>
              <linearGradient id="gradient" x1="0%" y1="0%" x2="100%" y2="0%">
                <stop offset="0%" stopColor="#c084fc" />
                <stop offset="50%" stopColor="#34d399" />
                <stop offset="100%" stopColor="#22d3ee" />
              </linearGradient>
            </defs>
          </svg>
        </div>

        {/* Animated Particles flowing through */}
        {active && particles.map(p => (
          <motion.div
            key={p.id}
            className="absolute rounded-full"
            style={{ 
              top: `calc(50% + ${p.yOffset}px)`, 
              left: '0%',
              width: '6px',
              height: '6px'
            }}
            animate={{
              left: ['0%', '50%', '100%'],
              opacity: [0, 1, 0],
              scale: [2.5, 1.2, 0.4],
              backgroundColor: ['#c084fc', '#34d399', '#22d3ee'],
              boxShadow: [
                '0 0 12px rgba(192,132,252,0.8)',
                '0 0 8px rgba(52,211,153,0.8)',
                '0 0 4px rgba(34,211,238,0.8)'
              ]
            }}
            transition={{
              duration: p.duration,
              repeat: Infinity,
              delay: p.delay,
              ease: "linear",
              times: [0, 0.5, 1]
            }}
          />
        ))}

        {/* The Proxy Core */}
        <div className="z-10 relative">
          <div className="absolute inset-0 bg-emerald-500/20 blur-xl rounded-full" />
          <div className="relative w-24 h-24 rounded-full bg-black border-2 border-emerald-500/50 flex flex-col items-center justify-center shadow-[0_0_30px_rgba(52,211,153,0.2)]">
            <div className="text-[10px] text-emerald-500 font-black uppercase tracking-wider mb-1">Synapse</div>
            <div className="text-xl font-black text-white">-{pctSaved}%</div>
            <div className="text-[8px] text-gray-500 uppercase tracking-widest mt-1">Compressed</div>
          </div>
        </div>
      </div>

      {/* Right side: Outgoing Tokens */}
      <div className="z-10 flex flex-col items-center">
        <div className="text-[10px] text-cyan-400/80 uppercase tracking-widest font-bold mb-2">Optimized Prompt</div>
        <div className="w-16 h-16 rounded-2xl bg-cyan-900/20 border border-cyan-500/30 flex items-center justify-center relative overflow-hidden">
          <div className="text-xl font-bold text-cyan-300 relative z-10">OPT.</div>
          {active && <motion.div animate={{ opacity: [0.1, 0.3, 0.1] }} transition={{ repeat: Infinity, duration: 2, delay: 0.5 }} className="absolute inset-0 bg-cyan-500/20" />}
        </div>
        <div className="text-xs font-mono text-cyan-400 mt-2">{tokensOut.toLocaleString()} tok</div>
      </div>
    </div>
  );
}
