"use client";

import { useState, useEffect } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { motion } from "framer-motion";
import { Sparkles, ArrowRight, UserPlus } from "lucide-react";
import { toast } from "sonner";
import ParticleBackground from "@/components/ParticleBackground";

export default function SignupPage() {
  const [firstName, setFirstName] = useState("");
  const [lastName, setLastName] = useState("");
  const [company, setCompany] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [passwordConfirm, setPasswordConfirm] = useState("");
  const [isLoading, setIsLoading] = useState(false);
  const [isConfigLoading, setIsConfigLoading] = useState(true);
  const [registrationOpen, setRegistrationOpen] = useState(true);
  const router = useRouter();

  useEffect(() => {
    fetch("/api/config")
      .then(res => res.json())
      .then(data => {
        setRegistrationOpen(data.registrationOpen);
        setIsConfigLoading(false);
      })
      .catch(() => setIsConfigLoading(false));
  }, []);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setIsLoading(true);

    if (password !== passwordConfirm) {
      toast.error("Passwords do not match");
      setIsLoading(false);
      return;
    }

    try {
      const res = await fetch("/api/auth/register", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ firstName, lastName, company, email, password }),
      });

      if (res.ok) {
        toast.success("Account created! Please check your email to verify.");
        router.push("/login");
      } else {
        const errorText = await res.text();
        toast.error(errorText || "Registration failed");
        setIsLoading(false);
      }
    } catch {
      toast.error("Something went wrong");
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
          
          <h2 className="text-3xl font-medium text-gray-100 text-center mb-2">Create Account</h2>
          <p className="text-gray-400 text-center text-sm mb-8">Join Synapse Proxy and optimize your AI costs</p>

          {isConfigLoading ? (
            <div className="text-center text-gray-400">Loading...</div>
          ) : registrationOpen ? (
            <form onSubmit={handleSubmit} className="space-y-4 relative z-10 pb-4">
              <div className="flex gap-4">
                <div className="flex-1">
                  <label className="block text-xs uppercase tracking-wider font-bold mb-2 text-gray-500">First Name</label>
                  <input
                    type="text"
                    value={firstName}
                    onChange={(e) => setFirstName(e.target.value)}
                    className="w-full p-4 bg-black/50 border border-white/10 rounded-xl focus:border-emerald-500 focus:outline-none text-white transition-colors"
                    placeholder="Jane"
                    required
                  />
                </div>
                <div className="flex-1">
                  <label className="block text-xs uppercase tracking-wider font-bold mb-2 text-gray-500">Last Name</label>
                  <input
                    type="text"
                    value={lastName}
                    onChange={(e) => setLastName(e.target.value)}
                    className="w-full p-4 bg-black/50 border border-white/10 rounded-xl focus:border-emerald-500 focus:outline-none text-white transition-colors"
                    placeholder="Doe"
                    required
                  />
                </div>
              </div>

              <div>
                <label className="block text-xs uppercase tracking-wider font-bold mb-2 text-gray-500">Company</label>
                <input
                  type="text"
                  value={company}
                  onChange={(e) => setCompany(e.target.value)}
                  className="w-full p-4 bg-black/50 border border-white/10 rounded-xl focus:border-emerald-500 focus:outline-none text-white transition-colors"
                  placeholder="Acme Corp"
                  required
                />
              </div>

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

              <div className="flex gap-4">
                <div className="flex-1">
                  <label className="block text-xs uppercase tracking-wider font-bold mb-2 text-gray-500">Password</label>
                  <input
                    type="password"
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    className="w-full p-4 bg-black/50 border border-white/10 rounded-xl focus:border-emerald-500 focus:outline-none text-white transition-colors"
                    placeholder="••••••••"
                    required
                  />
                </div>
                <div className="flex-1">
                  <label className="block text-xs uppercase tracking-wider font-bold mb-2 text-gray-500">Confirm Password</label>
                  <input
                    type="password"
                    value={passwordConfirm}
                    onChange={(e) => setPasswordConfirm(e.target.value)}
                    className="w-full p-4 bg-black/50 border border-white/10 rounded-xl focus:border-emerald-500 focus:outline-none text-white transition-colors"
                    placeholder="••••••••"
                    required
                  />
                </div>
              </div>

              <button 
                type="submit" 
                disabled={isLoading}
                className="w-full bg-emerald-500 hover:bg-emerald-400 text-black font-black py-4 rounded-xl transition-all shadow-[0_0_20px_rgba(52,211,153,0.3)] hover:shadow-[0_0_30px_rgba(52,211,153,0.5)] disabled:opacity-50 disabled:hover:bg-emerald-500 flex items-center justify-center gap-2 mt-4"
              >
                {isLoading ? "Creating..." : (
                  <>Create Account <span className="text-lg">{"\u2192"}</span></>
                )}
              </button>
            </form>
          ) : (
            <div className="text-center p-8 bg-amber-500/10 border border-amber-500/20 rounded-xl">
              <h3 className="text-amber-400 font-bold mb-2">Service Not Available</h3>
              <p className="text-sm text-gray-300">Public registration is currently closed. Please join the waitlist on the login page.</p>
            </div>
          )}

          <p className="mt-8 text-center text-sm text-gray-400 relative z-10">
            Already have an account? <Link href="/login" className="text-white font-bold hover:text-emerald-400 transition-colors">Log in</Link>
          </p>
        </div>
      </motion.div>
    </div>
  );
}
