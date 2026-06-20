"use client";

import { useState } from "react";
import Link from "next/link";
import { motion } from "framer-motion";
import { KeyRound, ArrowRight, ArrowLeft } from "lucide-react";
import { toast } from "sonner";
import ParticleBackground from "@/components/ParticleBackground";

export default function ForgotPasswordPage() {
  const [email, setEmail] = useState("");
  const [isLoading, setIsLoading] = useState(false);
  const [isSubmitted, setIsSubmitted] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setIsLoading(true);

    try {
      const res = await fetch("/api/auth/forgot-password", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email }),
      });

      if (res.ok) {
        setIsSubmitted(true);
      } else {
        toast.error("Failed to process request");
      }
    } catch {
      toast.error("Something went wrong");
    } finally {
      setIsLoading(false);
    }
  };

  return (
    <div className="min-h-screen flex items-center justify-center bg-[#050505] text-white font-sans relative overflow-hidden">
      <ParticleBackground />
      
      <motion.div 
        initial={{ opacity: 0, y: 20 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.5 }}
        className="relative z-10 w-full max-w-md p-8"
      >
        <div className="bg-black/20 border border-white/10 px-10 pt-10 pb-12 rounded-3xl backdrop-blur-lg shadow-2xl relative overflow-hidden">
          <div className="absolute top-0 right-0 w-64 h-64 bg-emerald-500/10 rounded-full blur-[80px] pointer-events-none" />
          
          <div className="flex flex-col items-center justify-center mb-8 relative z-10">
            <div className="w-24 h-24 rounded-full bg-[#0a0a0c] border border-white/10 shadow-[0_0_40px_rgba(52,211,153,0.5)] ring-2 ring-emerald-500/30 overflow-hidden flex items-center justify-center">
              <img src="/logo01.png" alt="Synapse Proxy Icon" className="w-[150%] h-[150%] object-cover max-w-none translate-y-3" />
            </div>
            <h1 className="mt-6 text-2xl font-black tracking-wide text-transparent bg-clip-text bg-gradient-to-r from-emerald-400 to-cyan-500">
              Synapse Proxy
            </h1>
          </div>
          
          <h2 className="text-3xl font-medium text-gray-100 text-center mb-2">Reset Password</h2>
          <p className="text-gray-400 text-center text-sm mb-8">Enter your email to receive a reset link.</p>

          {isSubmitted ? (
            <div className="text-center p-6 bg-emerald-500/10 border border-emerald-500/20 rounded-xl">
              <p className="text-emerald-400 font-bold mb-4">Check your inbox!</p>
              <p className="text-sm text-gray-400">If an account exists for {email}, a reset link has been sent.</p>
              <Link href="/login" className="mt-6 inline-block text-white hover:text-emerald-400 transition-colors font-bold text-sm">
                Return to Login
              </Link>
            </div>
          ) : (
            <form onSubmit={handleSubmit} className="space-y-5 relative z-10">
              <div>
                <label className="block text-xs uppercase tracking-wider font-bold mb-2 text-gray-500">Email Address</label>
                <input
                  type="email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  className="w-full p-4 bg-black/50 border border-white/10 rounded-xl focus:border-emerald-500 focus:outline-none text-white transition-colors"
                  placeholder="you@company.com"
                  required
                />
              </div>

              <button 
                type="submit" 
                disabled={isLoading}
                className="w-full bg-emerald-500 hover:bg-emerald-400 text-black font-black py-4 rounded-xl transition-all shadow-[0_0_20px_rgba(52,211,153,0.3)] hover:shadow-[0_0_30px_rgba(52,211,153,0.5)] disabled:opacity-50 disabled:hover:bg-emerald-500 flex items-center justify-center gap-2"
              >
                {isLoading ? "Sending..." : (
                  <>Send Reset Link <span className="text-lg">{"\u2192"}</span></>
                )}
              </button>
            </form>
          )}

          {!isSubmitted && (
            <div className="mt-8 flex justify-center">
              <Link href="/login" className="flex items-center gap-2 text-sm text-gray-400 hover:text-white transition-colors">
                <ArrowLeft className="w-4 h-4" /> Back to log in
              </Link>
            </div>
          )}
        </div>
      </motion.div>
    </div>
  );
}
