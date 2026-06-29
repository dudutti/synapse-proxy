"use client";

import { useSession } from "next-auth/react";

import { useRouter } from "next/navigation";

import { useEffect, useState } from "react";

import Link from "next/link";

import { motion } from "framer-motion";

import { ArrowLeft, KeyRound, Plus, Trash2, Activity, CreditCard, CheckCircle2, Lock, Shield } from "lucide-react";

import { toast } from "sonner";

import { useSearchParams } from "next/navigation";

import FirewallModal, { FirewallApiKey } from "@/components/FirewallModal";

interface ApiKey {

  id: string;

  virtualKey: string;

  provider: string;

  monthlyBudget: number;

  currentUsage: number;

  benchmarkMode: boolean;

  semanticTolerance: number;

  cacheTtl: number;

  isolateCacheByUser: boolean;

  zeroLog?: boolean;

  // Agent Firewall fields. Synced between Postgres (source of
  // truth for the dashboard) and Redis (hot path read by the
  // proxy on every request). The proxy parses them in
  // internal/services/auth.go; see proxy/internal/handlers/proxy.go
  // for the enforcement points.
  enableL1?: boolean;

  enableL2?: boolean;

  enableL3?: boolean;

  killSwitch?: boolean;

  fingerprintLoopDetect?: boolean;

  sessionTokenLimit?: number | null;

  allowedTools?: string | null;

  blockUnknownTools?: boolean;

  redactPII?: boolean;

  toolTtls?: string;

}

export default function SettingsPage() {

  const { data: session, status } = useSession();

  const router = useRouter();

  const [keys, setKeys] = useState<ApiKey[]>([]);

  const [loading, setLoading] = useState(true);

  const [userProfile, setUserProfile] = useState<{ id: string; email: string; tier: string; currentMonthTokens: number } | null>(null);
  const [plans, setPlans] = useState<{ id: string; name: string; tier: string; priceId: string; amount: number; tokens: number }[]>([]);

  const fetchProfileAndPlans = async () => {
    try {
      const [profileRes, plansRes] = await Promise.all([
        fetch("/api/user"),
        fetch("/api/plans")
      ]);
      if (profileRes.ok) {
        setUserProfile(await profileRes.json());
      }
      if (plansRes.ok) {
        setPlans(await plansRes.json());
      }
    } catch (err) {
      console.error("Failed to load user profile or plans", err);
    }
  };

  const [newProvider, setNewProvider] = useState("openai");

  const [newRealKey, setNewRealKey] = useState("");

  const [newFallbackProvider, setNewFallbackProvider] = useState("");

  const [newFallbackKey, setNewFallbackKey] = useState("");

  const [newFallbackModel, setNewFallbackModel] = useState("");

  const [newDefaultModel, setNewDefaultModel] = useState("");

  const [newIsolateCache, setNewIsolateCache] = useState(false);

  const searchParams = useSearchParams();

  const [availableModels, setAvailableModels] = useState<{id: string, name: string}[]>([]);

  const [loadingModels, setLoadingModels] = useState(false);

  const [availableFallbackModels, setAvailableFallbackModels] = useState<{id: string, name: string}[]>([]);

  const [loadingFallbackModels, setLoadingFallbackModels] = useState(false);

  useEffect(() => {

    const fetchFallbackModelsList = async () => {

      if (!newFallbackKey || newFallbackKey.length < 10 || !newFallbackProvider || newFallbackProvider === "custom") {

        setAvailableFallbackModels([]);

        return;

      }

      setLoadingFallbackModels(true);

      try {

        const res = await fetch("/api/models", {

          method: "POST",

          headers: { "Content-Type": "application/json" },

          body: JSON.stringify({ provider: newFallbackProvider, api_key: newFallbackKey }),

        });

        if (res.ok) {

          const data = await res.json();

          setAvailableFallbackModels(data.models || []);

        } else {

          setAvailableFallbackModels([]);

        }

      } catch {

        setAvailableFallbackModels([]);

      }

      setLoadingFallbackModels(false);

    };

    const delayDebounceFn = setTimeout(() => {

      fetchFallbackModelsList();

    }, 1000);

    return () => clearTimeout(delayDebounceFn);

  }, [newFallbackProvider, newFallbackKey]);

  useEffect(() => {

    const fetchModelsList = async () => {

      if (!newRealKey || newRealKey.length < 10 || !newProvider || newProvider === "custom") {

        setAvailableModels([]);

        return;

      }

      setLoadingModels(true);

      try {
        if (newProvider === "ollama" || newProvider === "lmstudio") {
          const res = await fetch(`/api/models?provider=${newProvider}&url=${encodeURIComponent(newRealKey)}`);
          if (res.ok) {
            const data = await res.json();
            if (Array.isArray(data)) {
              setAvailableModels(data.map((name: string) => ({ id: name, name: name })));
            } else {
              setAvailableModels([]);
            }
          } else {
            setAvailableModels([]);
          }
          setLoadingModels(false);
          return;
        }

        const res = await fetch("/api/models", {

          method: "POST",

          headers: { "Content-Type": "application/json" },

          body: JSON.stringify({ provider: newProvider, api_key: newRealKey }),

        });

        if (res.ok) {

          const data = await res.json();

          setAvailableModels(data.models || []);

        } else {

          setAvailableModels([]);

        }

      } catch {

        setAvailableModels([]);

      }

      setLoadingModels(false);

    };

    const delayDebounceFn = setTimeout(() => {

      fetchModelsList();

    }, 1000);

    return () => clearTimeout(delayDebounceFn);

  }, [newProvider, newRealKey]);

  useEffect(() => {

    if (status === "unauthenticated") {

      router.push("/login");

    } else if (status === "authenticated") {

      fetchKeys();
      fetchProfileAndPlans();

      if (searchParams.get("success")) {

        toast.success("Subscription updated successfully!");

        router.replace("/settings");

      }

      if (searchParams.get("canceled")) {

        toast.error("Checkout was canceled.");

        router.replace("/settings");

      }

    }

  }, [status, router, searchParams]);

  const handleCheckout = async (priceId: string) => {

    try {

      const res = await fetch("/api/stripe/checkout", {

        method: "POST",

        headers: { "Content-Type": "application/json" },

        body: JSON.stringify({ priceId })

      });

      if (!res.ok) throw new Error();

      const data = await res.json();

      window.location.href = data.url;

    } catch {

      toast.error("Failed to initiate checkout");

    }

  };

  const [showSnippetModal, setShowSnippetModal] = useState<string | null>(null);

  // Agent Firewall modal: holds the key being edited. Null = closed.
  // We snapshot the key inside the modal so we can edit without
  // mutating the list state until the user clicks Save.
  // The state type is the lighter FirewallApiKey (the modal
  // only needs the firewall fields + id + display labels).
  const [showFirewallModal, setShowFirewallModal] = useState<FirewallApiKey | null>(null);

  const fetchKeys = async () => {

    setLoading(true);

    const res = await fetch("/api/keys");

    if (res.ok) {

      const data = await res.json();

      setKeys(data);

    }

    setLoading(false);

  };

  const generateKey = async (e: React.FormEvent) => {

    e.preventDefault();

    const tId = toast.loading("Encrypting key...");

    try {

      const res = await fetch("/api/keys", {

        method: "POST",

        headers: { "Content-Type": "application/json" },

        body: JSON.stringify({

          provider: newProvider,

          realKey: newRealKey,

          fallbackProvider: newFallbackProvider,

          fallbackKey: newFallbackKey,

          fallbackModel: newFallbackModel,

          defaultModel: newDefaultModel,

          isolateCacheByUser: newIsolateCache

        }),

      });

      if (!res.ok) {

        const errBody = await res.json().catch(() => ({}));

        throw new Error(errBody?.error || `Server returned ${res.status}`);

      }

      const newKeyData = await res.json();

      setNewRealKey("");

      setNewFallbackKey("");

      setNewFallbackProvider("");

      setNewFallbackModel("");

      setNewDefaultModel("");

      setNewIsolateCache(false);

      await fetchKeys();

      router.refresh();

      toast.success("Key generated securely", { id: tId });

      setShowSnippetModal(newKeyData.virtualKey);

    } catch (err: any) {

      toast.error(`Could not generate key: ${err?.message || "unknown error"}`, { id: tId });

    }

  };

  const deleteKey = async (id: string) => {

    if (!confirm("Are you sure you want to delete this key?")) return;

    setKeys((prev) => prev.filter((k) => k.id !== id));

    const tId = toast.loading("Deleting key...");

    try {

      const res = await fetch(`/api/keys/${id}`, { method: "DELETE" });

      if (!res.ok) {

        const errBody = await res.json().catch(() => ({}));

        throw new Error(errBody?.error || `Server returned ${res.status}`);

      }

      await fetchKeys();

      router.refresh();

      toast.success("Key deleted", { id: tId });

    } catch (err: any) {

      await fetchKeys();

      toast.error(`Could not delete: ${err?.message || "unknown error"}`, { id: tId });

    }

  };

  const toggleBenchmark = async (key: ApiKey) => {

    const tId = toast.loading("Updating mode...");

    try {

      const res = await fetch(`/api/keys/${key.id}`, {

        method: "PUT",

        headers: { "Content-Type": "application/json" },

        body: JSON.stringify({ benchmarkMode: !key.benchmarkMode }),

      });

      if (!res.ok) {

        const errBody = await res.json().catch(() => ({}));

        throw new Error(errBody?.error || `Server returned ${res.status}`);

      }

      await fetchKeys();

      router.refresh();

      toast.success(key.benchmarkMode ? "Benchmark mode disabled" : "Benchmark mode enabled", { id: tId });

    } catch (err: any) {

      toast.error(`Could not update mode: ${err?.message || "unknown error"}`, { id: tId });

    }

  };

  const toggleIsolateCache = async (key: ApiKey) => {

    const tId = toast.loading("Updating Cache Isolation...");

    try {

      const res = await fetch(`/api/keys/${key.id}`, {

        method: "PUT",

        headers: { "Content-Type": "application/json" },

        body: JSON.stringify({ isolateCacheByUser: !key.isolateCacheByUser }),

      });

      if (!res.ok) {

        const errBody = await res.json().catch(() => ({}));

        throw new Error(errBody?.error || `Server returned ${res.status}`);

      }

      await fetchKeys();

      router.refresh();

      toast.success(`Cache Isolation ${!key.isolateCacheByUser ? "Enabled" : "Disabled"}`, { id: tId });

    } catch (err: any) {

      toast.error(`Could not update setting: ${err?.message || "unknown error"}`, { id: tId });

    }

  };

  const toggleZeroLog = async (key: ApiKey) => {

    const next = !key.zeroLog;

    const confirm = window.confirm(

      next

        ? "Enable Zero-Log Mode?\n\nThe proxy will no longer persist the content of your prompts or responses. Token counts and metadata will still be saved.\n\nL1, L2, loop cache and Model Radar sample collection will be disabled for this key."

        : "Disable Zero-Log Mode?\n\nThe proxy will resume normal caching and telemetry for this key."

    );

    if (!confirm) return;

    const tId = toast.loading("Updating Zero-Log Mode...");

    try {

      const res = await fetch(`/api/keys/${key.id}`, {

        method: "PUT",

        headers: { "Content-Type": "application/json" },

        body: JSON.stringify({ zeroLog: next }),

      });

      if (!res.ok) {

        const errBody = await res.json().catch(() => ({}));

        throw new Error(errBody?.error || `Server returned ${res.status}`);

      }

      await fetchKeys();

      router.refresh();

      toast.success(`Zero-Log Mode ${next ? "Enabled" : "Disabled"}`, { id: tId });

    } catch (err: any) {

      toast.error(`Could not update Zero-Log: ${err?.message || "unknown error"}`, { id: tId });

    }

  };

  // saveFirewall sends the Agent Firewall configuration to PUT
  // /api/keys/[id] which persists to Postgres AND syncs the
  // corresponding fields into Redis (so the proxy reads them
  // on the next request). The body shape matches what
  // dashboard/app/api/keys/[id]/route.ts expects in its
  // `dataToUpdate` and `redisData` builders.
  const saveFirewall = async (form: {
    enableL1: boolean;
    enableL2: boolean;
    enableL3: boolean;
    killSwitch: boolean;
    fingerprintLoopDetect: boolean;
    sessionTokenLimit: number | null;
    allowedTools: string;
    blockUnknownTools: boolean;
    redactPII: boolean;
    toolTtls: string;
  }) => {
    if (!showFirewallModal) return;
    const tId = toast.loading("Saving firewall rules...");
    try {
      const res = await fetch(`/api/keys/${showFirewallModal.id}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(form),
      });
      if (!res.ok) {
        const errBody = await res.json().catch(() => ({}));
        throw new Error(errBody?.error || `Server returned ${res.status}`);
      }
      await fetchKeys();
      router.refresh();
      toast.success("Firewall rules updated", { id: tId });
      setShowFirewallModal(null);
    } catch (err: any) {
      toast.error(`Could not save firewall: ${err?.message || "unknown error"}`, { id: tId });
    }
  };

  const updateTolerance = async (key: ApiKey, value: number) => {

    const tId = toast.loading("Saving...");

    try {

      const res = await fetch(`/api/keys/${key.id}`, {

        method: "PUT",

        headers: { "Content-Type": "application/json" },

        body: JSON.stringify({ semanticTolerance: value }),

      });

      if (!res.ok) {

        const errBody = await res.json().catch(() => ({}));

        throw new Error(errBody?.error || `Server returned ${res.status}`);

      }

      await fetchKeys();

      router.refresh();

      toast.success("Semantic Tolerance updated", { id: tId });

    } catch (err: any) {

      toast.error(`Could not update tolerance: ${err?.message || "unknown error"}`, { id: tId });

    }

  };

  const updateCacheTtl = async (key: ApiKey, value: number) => {

    const tId = toast.loading("Saving TTL...");

    try {

      const res = await fetch(`/api/keys/${key.id}`, {

        method: "PUT",

        headers: { "Content-Type": "application/json" },

        body: JSON.stringify({ cacheTtl: value }),

      });

      if (!res.ok) {

        const errBody = await res.json().catch(() => ({}));

        throw new Error(errBody?.error || `Server returned ${res.status}`);

      }

      await fetchKeys();

      router.refresh();

      toast.success("Cache TTL updated", { id: tId });

    } catch (err: any) {

      toast.error(`Error: ${err?.message || "unknown"}`, { id: tId });

    }

  };

  const updateBudget = async (key: ApiKey, value: number) => {

    const tId = toast.loading("Saving Budget...");

    try {

      const res = await fetch(`/api/keys/${key.id}`, {

        method: "PUT",

        headers: { "Content-Type": "application/json" },

        body: JSON.stringify({ monthlyBudget: value }),

      });

      if (!res.ok) {

        const errBody = await res.json().catch(() => ({}));

        throw new Error(errBody?.error || `Server returned ${res.status}`);

      }

      await fetchKeys();

      router.refresh();

      toast.success("Monthly Budget updated", { id: tId });

    } catch (err: any) {

      toast.error(`Error: ${err?.message || "unknown"}`, { id: tId });

    }

  };

  const purgeCache = async (key: ApiKey) => {

    if (!confirm("Are you sure you want to clear the cache for this key? This will delete all saved responses and impact your cache hit ratio.")) return;

    

    const promise = fetch(`/api/cache/purge?vk=${key.virtualKey}`, {

      method: "DELETE",

    }).then(async (res) => {

      if (!res.ok) throw new Error("Failed to purge cache");

      const data = await res.json();

      return `Cache cleared (${data.deleted} entries deleted)`;

    });

    toast.promise(promise, {

      loading: "Clearing cache...",

      success: (data) => data,

      error: "Could not clear cache"

    });

  };

  const containerVars = {

    hidden: { opacity: 0 },

    show: {

      opacity: 1,

      transition: { staggerChildren: 0.1 }

    }

  };

  const itemVars = {

    hidden: { opacity: 0, y: 20 },

    show: { opacity: 1, y: 0, transition: { duration: 0.5, ease: "easeOut" } }

  };

  const activeTier = userProfile?.tier || "FREE";

  const defaultPlans = [
    {
      id: "free",
      name: "Hobby (Free)",
      tier: "FREE",
      priceId: "price_free",
      amount: 0,
      tokens: 10000000,
      features: [
        "10M optimization tokens / mo",
        "1 Virtual Synapse Proxy Key",
        "Standard latency & caching",
        "Community support"
      ]
    },
    {
      id: "pro",
      name: "Pro (Tier 1)",
      tier: "PRO_1",
      priceId: "price_mock_pro",
      amount: 5,
      tokens: 20000000,
      features: [
        "20M optimization tokens / mo",
        "Unlimited Virtual Keys",
        "Priority latency & L3 compression",
        "Email support (24h response)"
      ]
    },
    {
      id: "scale",
      name: "Scale (Tier 2)",
      tier: "PRO_2",
      priceId: "price_mock_enterprise",
      amount: 15,
      tokens: 100000000,
      features: [
        "100M optimization tokens / mo",
        "Unlimited Virtual Keys",
        "Dedicated caching nodes",
        "Priority Support (4h response)"
      ]
    }
  ];

  const displayPlans = plans.length > 0 ? plans.map(p => {
    let features: string[] = [];
    if (p.tier === "FREE") {
      features = [
        "10M optimization tokens / mo",
        "1 Virtual Synapse Proxy Key",
        "Standard latency & caching",
        "Community support"
      ];
    } else if (p.tier === "PRO_1") {
      features = [
        "20M optimization tokens / mo",
        "Unlimited Virtual Keys",
        "Priority latency & L3 compression",
        "Email support (24h response)"
      ];
    } else if (p.tier === "PRO_2") {
      features = [
        "100M optimization tokens / mo",
        "Unlimited Virtual Keys",
        "Dedicated caching nodes",
        "Priority Support (4h response)"
      ];
    } else {
      features = [
        `${(p.tokens / 1000000).toFixed(0)}M optimization tokens / mo`,
        "Unlimited Virtual Keys",
        "Advanced Agent Firewall features",
        "Priority Support"
      ];
    }
    return {
      ...p,
      features,
      recommended: p.tier === "PRO_1"
    };
  }) : defaultPlans.map(p => ({ ...p, recommended: p.tier === "PRO_1" }));

  if (status === "loading" || loading) return <div className="min-h-screen bg-[#050505] text-white flex items-center justify-center">Loading...</div>;

  return (

    <div className="min-h-screen bg-[#050505] text-white p-8 font-sans relative overflow-hidden">

      {/* Background Orbs */}

      <div className="absolute top-[10%] right-[10%] w-96 h-96 bg-blue-500/10 rounded-full blur-[120px] pointer-events-none" />

      <div className="absolute bottom-[10%] left-[10%] w-96 h-96 bg-emerald-500/10 rounded-full blur-[120px] pointer-events-none" />

      <motion.div 

        variants={containerVars}

        initial="hidden"

        animate="show"

        className="max-w-5xl mx-auto relative z-10"

      >

        <motion.header variants={itemVars} className="mb-10 flex justify-between items-center bg-white/5 border border-white/10 p-6 rounded-2xl backdrop-blur-xl shadow-2xl">

          <div className="flex items-center gap-4">

            <Link href="/" className="w-10 h-10 rounded-xl bg-white/5 hover:bg-white/10 transition border border-white/10 flex items-center justify-center text-gray-400 hover:text-white">

              <ArrowLeft className="w-5 h-5" />

            </Link>

            <div>

              <h1 className="text-2xl font-bold tracking-tight text-white">API Keys & Security</h1>

              <p className="text-gray-400 text-sm">Manage your provider keys and settings</p>

            </div>

          </div>

        </motion.header>

        <motion.main variants={itemVars} className="grid gap-8">

          {/* Create Key Section */}

          <section className="bg-white/5 p-8 rounded-3xl border border-white/10 backdrop-blur-xl shadow-2xl relative overflow-hidden">

            <div className="absolute top-0 right-0 w-64 h-64 bg-indigo-500/10 rounded-full blur-[80px] pointer-events-none" />

            <div className="flex items-center gap-3 mb-6 relative z-10">

              <div className="p-2 bg-indigo-500/20 rounded-lg border border-indigo-500/30 text-indigo-400">

                <KeyRound className="w-5 h-5" />

              </div>

              <div>

                <h2 className="text-lg font-bold text-white">Generate Virtual Synapse Proxy Key</h2>

                <p className="text-gray-400 text-sm">

                  We encrypt your real key at rest. You get a safe <code>sk-opti-...</code> key.

                </p>

              </div>

            </div>

            <form onSubmit={generateKey} className="flex flex-col gap-6 relative z-10">

              

              <div className="grid grid-cols-1 md:grid-cols-2 gap-4">

                <div className="w-full">

                  <label className="block text-[10px] uppercase tracking-wider font-bold mb-2 text-gray-400">Primary Provider</label>

                  <select

                    value={newProvider}

                    onChange={(e) => {
                      const val = e.target.value;
                      setNewProvider(val);
                      if (val === "ollama") {
                        setNewRealKey("http://localhost:11434");
                      } else if (val === "lmstudio") {
                        setNewRealKey("http://localhost:1234");
                      } else {
                        setNewRealKey("");
                      }
                    }}

                    className="w-full p-4 bg-[#0a0a0a] border border-white/10 rounded-xl focus:border-emerald-500 focus:ring-1 focus:ring-emerald-500/50 focus:outline-none text-white appearance-none transition-all"

                  >

                    <option value="openai">OpenAI</option>

                    <option value="anthropic">Anthropic</option>

                    <option value="google">Google (Gemini)</option>

                    <option value="deepseek">DeepSeek</option>

                    <option value="mistral">Mistral AI</option>

                    <option value="openrouter">OpenRouter</option>

                    <option value="minimax">Minimax</option>

                    <option value="groq">Groq</option>

                    <option value="ollama">Ollama (Local)</option>

                    <option value="lmstudio">LM Studio (Local)</option>

                    <option value="custom">Custom Endpoint</option>

                  </select>

                </div>

                <div className="w-full">

                  <label className="block text-[10px] uppercase tracking-wider font-bold mb-2 text-gray-400">Secret Key</label>

                  <div className="relative">

                    <input

                      type="password"

                      value={newRealKey}

                      onChange={(e) => setNewRealKey(e.target.value)}

                      className="w-full p-4 pr-32 bg-[#0a0a0a] border border-white/10 rounded-xl focus:border-emerald-500 focus:ring-1 focus:ring-emerald-500/50 focus:outline-none text-white font-mono text-sm transition-all"

                      placeholder="sk-..."

                      required

                    />

                    <div className="absolute right-4 top-1/2 -translate-y-1/2 flex items-center gap-1 text-[10px] text-emerald-500/80 font-bold bg-emerald-500/10 px-2 py-1 rounded-md">

                      <CheckCircle2 className="w-3 h-3" /> ENCRYPTED

                    </div>

                  </div>

                </div>

              </div>

              <div className="w-full">

                <div className="flex items-center gap-2 mb-2">

                  <label className="block text-[10px] uppercase tracking-wider font-bold text-gray-400">

                    Default Model Override (Optional)

                  </label>

                  {loadingModels && <span className="text-emerald-500 animate-pulse text-[10px] font-bold">Loading...</span>}

                </div>

                {availableModels.length > 0 ? (

                  <select

                    value={newDefaultModel}

                    onChange={(e) => setNewDefaultModel(e.target.value)}

                    className="w-full p-4 bg-[#0a0a0a] border border-white/10 rounded-xl focus:border-emerald-500 focus:ring-1 focus:ring-emerald-500/50 focus:outline-none text-white appearance-none transition-all"

                  >

                    <option value="">-- No override (Use original request model) --</option>

                    {availableModels.map(m => (

                      <option key={m.id} value={m.id}>{m.name || m.id}</option>

                    ))}

                  </select>

                ) : (

                  <input

                    type="text"

                    value={newDefaultModel}

                    onChange={(e) => setNewDefaultModel(e.target.value)}

                    className="w-full p-4 bg-[#0a0a0a] border border-white/10 rounded-xl focus:border-emerald-500 focus:ring-1 focus:ring-emerald-500/50 focus:outline-none text-white transition-all"

                    placeholder="e.g. abab6.5s-chat, gpt-4o, claude-3-opus-20240229..."

                  />

                )}

                <p className="text-xs text-gray-500 mt-2">If set, Synapse Proxy will forcefully overwrite any model name requested by your local clients and map it to this specific model.</p>

              </div>

              <div className="h-px w-full bg-gradient-to-r from-transparent via-white/10 to-transparent my-4" />

              <div className="grid grid-cols-1 md:grid-cols-3 gap-4">

                <div className="w-full">

                  <label className="block text-[10px] uppercase tracking-wider font-bold mb-2 text-emerald-500/80">Fallback Provider</label>

                  <select

                    value={newFallbackProvider}

                    onChange={(e) => setNewFallbackProvider(e.target.value)}

                    className="w-full p-4 bg-[#0a0a0a] border border-white/10 rounded-xl focus:border-emerald-500 focus:ring-1 focus:ring-emerald-500/50 focus:outline-none text-white appearance-none transition-all"

                  >

                    <option value="">None</option>

                    <option value="openai">OpenAI</option>

                    <option value="anthropic">Anthropic</option>

                    <option value="google">Google (Gemini)</option>

                    <option value="deepseek">DeepSeek</option>

                    <option value="mistral">Mistral AI</option>

                    <option value="openrouter">OpenRouter</option>

                    <option value="minimax">Minimax</option>

                    <option value="groq">Groq</option>

                    <option value="cohere">Cohere</option>

                    <option value="together">Together AI</option>

                    <option value="perplexity">Perplexity</option>

                  </select>

                </div>

                <div className="w-full">

                  <label className="block text-[10px] uppercase tracking-wider font-bold mb-2 text-emerald-500/80">Fallback API Key</label>

                  <input

                    type="password"

                    value={newFallbackKey}

                    onChange={(e) => setNewFallbackKey(e.target.value)}

                    className="w-full p-4 bg-[#0a0a0a] border border-white/10 rounded-xl focus:border-emerald-500 focus:ring-1 focus:ring-emerald-500/50 focus:outline-none text-white transition-all font-mono text-sm"

                    placeholder="Leave empty if no fallback"

                  />

                </div>

                <div className="w-full">

                  <div className="flex items-center gap-2 mb-2">

                    <label className="block text-[10px] uppercase tracking-wider font-bold text-emerald-500/80">

                      Fallback Model Override

                    </label>

                    {loadingFallbackModels && <span className="text-emerald-500 animate-pulse text-[10px] font-bold">Loading...</span>}

                  </div>

                  {availableFallbackModels.length > 0 ? (

                    <select

                      value={newFallbackModel}

                      onChange={(e) => setNewFallbackModel(e.target.value)}

                      className="w-full p-4 bg-[#0a0a0a] border border-white/10 rounded-xl focus:border-emerald-500 focus:ring-1 focus:ring-emerald-500/50 focus:outline-none text-white appearance-none transition-all"

                    >

                      <option value="">-- No override --</option>

                      {availableFallbackModels.map(m => (

                        <option key={m.id} value={m.id}>{m.name || m.id}</option>

                      ))}

                    </select>

                  ) : (

                    <input

                      type="text"

                      value={newFallbackModel}

                      onChange={(e) => setNewFallbackModel(e.target.value)}

                      className="w-full p-4 bg-[#0a0a0a] border border-white/10 rounded-xl focus:border-emerald-500 focus:ring-1 focus:ring-emerald-500/50 focus:outline-none text-white transition-all"

                      placeholder="e.g. gpt-3.5-turbo"

                    />

                  )}

                </div>

              </div>

              <div 

                className="flex items-start gap-4 mt-2 p-5 bg-[#0a0a0a]/50 border border-white/5 rounded-xl hover:border-white/10 transition-colors cursor-pointer group"

                onClick={() => setNewIsolateCache(!newIsolateCache)}

              >

                <div className="pt-0.5">

                  <div className={`w-5 h-5 rounded border flex items-center justify-center transition-colors ${newIsolateCache ? 'bg-emerald-500 border-emerald-500' : 'border-white/20 bg-black/50 group-hover:border-white/40'}`}>

                    {newIsolateCache && <CheckCircle2 className="w-3 h-3 text-black stroke-[3]" />}

                  </div>

                </div>

                <div>

                  <label className="text-sm font-bold text-white block cursor-pointer">

                    Enable Multi-Tenant Cache Isolation (Namespace)

                  </label>

                  <p className="text-xs text-gray-400 mt-1">

                    If enabled, Synapse Proxy will fragment the L1 and L2 cache by reading the <code className="text-emerald-400/80 bg-emerald-400/10 px-1.5 py-0.5 rounded text-[10px] mx-1">user</code> parameter from your OpenAI payload. Crucial for SaaS chatbots to prevent Data Bleeding.

                  </p>

                </div>

              </div>

              <button type="submit" className="w-full mt-4 flex items-center justify-center gap-2 bg-emerald-500 text-black font-bold text-base py-4 px-8 rounded-xl hover:bg-emerald-400 hover:scale-[1.01] transition-all active:scale-95 shadow-[0_0_20px_rgba(16,185,129,0.2)] hover:shadow-[0_0_30px_rgba(16,185,129,0.4)]">

                <Plus className="w-5 h-5" /> Generate Virtual Key

              </button>

            </form>

          </section>

          {/* Existing Keys Section */}

          <section className="bg-white/5 p-8 rounded-3xl border border-white/10 backdrop-blur-xl shadow-2xl relative overflow-hidden">

            <h2 className="text-lg font-bold mb-6 text-white relative z-10">Your Active Keys</h2>

            

            {keys.length === 0 ? (

              <div className="text-gray-500 text-center py-12 bg-black/20 rounded-2xl border border-white/5 border-dashed">

                <KeyRound className="w-12 h-12 mx-auto mb-4 opacity-20" />

                No keys generated yet.

              </div>

            ) : (

              <div className="overflow-x-auto relative z-10">

                <table className="w-full text-left text-sm">

                  <thead className="text-gray-500 border-b border-white/5">

                    <tr>

                      <th className="pb-4 font-medium uppercase text-xs tracking-wider">Provider</th>

                      <th className="pb-4 font-medium uppercase text-xs tracking-wider">Virtual Key</th>

                      <th className="pb-4 font-medium uppercase text-xs tracking-wider">Limits & Cache</th>

                      <th className="pb-4 font-medium uppercase text-xs tracking-wider">Sensitivity</th>

                      <th className="pb-4 font-medium uppercase text-xs tracking-wider">Benchmark</th>

                      <th className="pb-4 font-medium uppercase text-xs tracking-wider">Tenant Cache</th>

                      <th className="pb-4 font-medium uppercase text-xs tracking-wider">Zero-Log</th>

                      <th className="pb-4 font-medium uppercase text-xs tracking-wider text-right">Actions</th>

                    </tr>

                  </thead>

                  <tbody className="divide-y divide-white/5">

                    {keys.map((k) => (

                      <motion.tr 

                        initial={{ opacity: 0 }} animate={{ opacity: 1 }} 

                        key={k.id} 

                        className="hover:bg-white/[0.02] transition-colors"

                      >

                        <td className="py-5 font-mono text-xs uppercase text-blue-300 font-bold">{k.provider}</td>

                        <td className="py-5 font-mono text-xs text-emerald-400 bg-emerald-400/5 px-2 rounded">

                          {k.virtualKey}

                        </td>

                        <td className="py-5 text-gray-300 font-medium">

                          <div className="flex flex-col gap-3 w-32">

                            <div className="flex flex-col">

                              <span className="text-[10px] text-gray-500 font-bold uppercase mb-1">TTL (Seconds)</span>

                              <input 

                                type="number" 

                                defaultValue={k.cacheTtl || 86400}

                                onBlur={(e) => updateCacheTtl(k, parseInt(e.target.value))}

                                className="w-full bg-black/40 border border-white/10 rounded px-2 py-1 text-xs focus:border-emerald-500 outline-none"

                              />

                            </div>

                            <div className="flex flex-col">

                              <span className="text-[10px] text-gray-500 font-bold uppercase mb-1">Budget ($)</span>

                              <input 

                                type="number" 

                                defaultValue={k.monthlyBudget || 100}

                                onBlur={(e) => updateBudget(k, parseFloat(e.target.value))}

                                className="w-full bg-black/40 border border-white/10 rounded px-2 py-1 text-xs focus:border-emerald-500 outline-none"

                              />

                            </div>

                          </div>

                        </td>

                        <td className="py-5 text-gray-300 font-medium">

                          <div className="flex flex-col gap-2 w-32">

                            <div className="flex justify-between text-[10px] text-gray-500 font-bold uppercase">

                              <span>Strict</span>

                              <span className="text-emerald-400">{k.semanticTolerance || 0.15}</span>

                              <span>Loose</span>

                            </div>

                            <input 

                              type="range" 

                              min="0.05" max="0.30" step="0.01" 

                              defaultValue={k.semanticTolerance || 0.15}

                              onMouseUp={(e) => updateTolerance(k, parseFloat(e.currentTarget.value))}

                              className="w-full h-1 bg-white/10 rounded-lg appearance-none cursor-pointer accent-emerald-500"

                            />

                          </div>

                        </td>

                        <td className="py-5">

                          <button

                            onClick={() => toggleBenchmark(k)}

                            className={`flex items-center gap-1.5 px-3 py-1.5 text-xs font-bold rounded-lg border transition-all ${

                              k.benchmarkMode

                                ? "bg-purple-500/20 text-purple-300 border-purple-500/30 hover:bg-purple-500/30 shadow-[0_0_15px_rgba(168,85,247,0.2)]"

                                : "bg-white/5 text-gray-400 border-white/10 hover:bg-white/10"

                            }`}

                          >

                            <Activity className="w-3 h-3" />

                            {k.benchmarkMode ? "ACTIVE (Eval)" : "Disabled"}

                          </button>

                        </td>

                        <td className="py-5">

                          <button

                            onClick={() => toggleIsolateCache(k)}

                            className={`flex items-center gap-1.5 px-3 py-1.5 text-xs font-bold rounded-lg border transition-all ${

                              k.isolateCacheByUser

                                ? "bg-emerald-500/20 text-emerald-300 border-emerald-500/30 hover:bg-emerald-500/30 shadow-[0_0_15px_rgba(16,185,129,0.2)]"

                                : "bg-white/5 text-gray-400 border-white/10 hover:bg-white/10"

                            }`}

                          >

                            {k.isolateCacheByUser ? "Isolated" : "Shared"}

                          </button>

                        </td>

                        <td className="py-5">

                          <button

                            onClick={() => toggleZeroLog(k)}

                            title="Zero-Log Mode: never persist prompt/response content. Caches disabled."

                            className={`flex items-center gap-1.5 px-3 py-1.5 text-xs font-bold rounded-lg border transition-all ${

                              k.zeroLog

                                ? "bg-amber-500/20 text-amber-300 border-amber-500/30 hover:bg-amber-500/30 shadow-[0_0_15px_rgba(245,158,11,0.2)]"

                                : "bg-white/5 text-gray-400 border-white/10 hover:bg-white/10"

                            }`}

                          >

                            <Lock className="w-3 h-3" />

                            {k.zeroLog ? "ON" : "OFF"}

                          </button>

                        </td>

                        <td className="py-5 text-right flex flex-col gap-2 items-end">

                          <button

                            onClick={() => setShowFirewallModal(k as FirewallApiKey)}

                            title="Agent Firewall: kill switch, tool filter, PII redaction"

                            className="text-cyan-400 hover:text-cyan-300 flex items-center justify-center w-24 gap-1.5 text-xs font-bold px-3 py-1.5 border border-cyan-500/20 rounded-lg bg-cyan-500/10 hover:bg-cyan-500/20 transition-all"

                          >

                            <Shield className="w-3.5 h-3.5" /> Firewall

                          </button>

                          <button

                            onClick={() => purgeCache(k)}

                            className="text-amber-400 hover:text-amber-300 flex items-center justify-center w-24 gap-1.5 text-xs font-bold px-3 py-1.5 border border-amber-500/20 rounded-lg bg-amber-500/10 hover:bg-amber-500/20 transition-all"

                          >

                            Purge

                          </button>

                          <button 

                            onClick={() => deleteKey(k.id)}

                            className="text-red-400 hover:text-red-300 flex items-center justify-center w-24 gap-1.5 text-xs font-bold px-3 py-1.5 border border-red-500/20 rounded-lg bg-red-500/10 hover:bg-red-500/20 transition-all"

                          >

                            <Trash2 className="w-3.5 h-3.5" /> Delete

                          </button>

                        </td>

                      </motion.tr>

                    ))}

                  </tbody>

                </table>

              </div>

            )}

          </section>

          {/* Billing Section */}

          <section className="bg-white/5 p-8 rounded-3xl border border-white/10 backdrop-blur-xl shadow-2xl relative overflow-hidden">

            <div className="absolute top-0 right-0 w-64 h-64 bg-emerald-500/10 rounded-full blur-[80px] pointer-events-none" />

            <div className="flex items-center gap-3 mb-8 relative z-10">

              <div className="p-2 bg-emerald-500/20 rounded-lg border border-emerald-500/30 text-emerald-400">

                <CreditCard className="w-5 h-5" />

              </div>

              <div>

                <h2 className="text-lg font-bold text-white">Subscription & Billing</h2>

                <p className="text-gray-400 text-sm">Manage your plan and token usage limits</p>

              </div>

            </div>

            {userProfile && (
              <div className="mb-8 p-6 bg-black/40 border border-white/5 rounded-2xl relative z-10">
                <div className="flex justify-between items-center mb-2 text-xs">
                  <span className="font-bold text-zinc-400 uppercase tracking-wider">Monthly Optimization Token Usage</span>
                  <span className="font-mono text-emerald-400 font-bold">
                    {userProfile.currentMonthTokens.toLocaleString()} / {
                      activeTier === "FREE" ? "10M (10,000,000)" :
                      activeTier === "PRO_1" ? "20M (20,000,000)" :
                      activeTier === "PRO_2" ? "100M (100,000,000)" : "Unlimited"
                    }
                  </span>
                </div>
                <div className="w-full bg-white/10 h-2 rounded-full overflow-hidden">
                  <div 
                    className="bg-gradient-to-r from-emerald-500 to-teal-500 h-full transition-all duration-500"
                    style={{ 
                      width: `${Math.min(
                        100, 
                        (userProfile.currentMonthTokens / (
                          activeTier === "FREE" ? 10000000 :
                          activeTier === "PRO_1" ? 20000000 :
                          activeTier === "PRO_2" ? 100000000 : Infinity
                        )) * 100
                      )}%` 
                    }}
                  />
                </div>
                <p className="text-[10px] text-zinc-500 mt-2">
                  Only prompt tokens (input only) are counted against your monthly tier limits.
                </p>
              </div>
            )}

            <div className="grid grid-cols-1 md:grid-cols-3 gap-6 relative z-10">

              {displayPlans.map((plan) => {
                const isCurrent = plan.tier === activeTier;
                return (
                  <div 
                    key={plan.priceId}
                    className={`border p-6 rounded-2xl flex flex-col relative transition-all ${
                      plan.recommended 
                        ? "bg-gradient-to-br from-emerald-950/30 to-teal-950/30 border-emerald-500/30 shadow-[0_0_30px_rgba(16,185,129,0.05)]" 
                        : "bg-black/50 border-white/10"
                    }`}
                  >
                    {plan.recommended && (
                      <div className="absolute -top-3 left-1/2 -translate-x-1/2 bg-gradient-to-r from-emerald-400 to-teal-400 text-black text-[9px] font-black uppercase px-2.5 py-0.5 rounded-full tracking-wider">
                        Recommended
                      </div>
                    )}

                    <h3 className="text-white font-bold text-lg mb-1">{plan.name}</h3>
                    <p className="text-[10px] text-zinc-500 font-bold uppercase tracking-wider mb-4">Tier: {plan.tier}</p>

                    <div className="text-3xl font-black text-white mb-6">
                      €{plan.amount}
                      <span className="text-xs font-normal text-zinc-500">/mo</span>
                    </div>

                    <ul className="space-y-3 mb-8 flex-1 text-xs text-zinc-400">
                      {plan.features.map((feat, idx) => (
                        <li key={idx} className="flex items-center gap-2">
                          <CheckCircle2 className="w-3.5 h-3.5 text-emerald-400 flex-shrink-0" />
                          <span>{feat}</span>
                        </li>
                      ))}
                    </ul>

                    {isCurrent ? (
                      <button disabled className="w-full py-2 bg-white/15 text-zinc-400 rounded-lg font-bold border border-white/5 cursor-not-allowed text-xs">
                        Current Plan
                      </button>
                    ) : (
                      <button 
                        onClick={() => handleCheckout(plan.priceId)} 
                        className={`w-full py-2 rounded-lg font-bold text-xs transition-all hover:scale-[1.02] active:scale-95 ${
                          plan.recommended
                            ? "bg-gradient-to-r from-emerald-500 to-teal-500 text-black hover:from-emerald-400 hover:to-teal-400 shadow-md shadow-emerald-500/10"
                            : "bg-white/10 hover:bg-white/20 text-white border border-white/10"
                        }`}
                      >
                        {plan.amount === 0 ? "Downgrade Plan" : `Upgrade to ${plan.name.split(" ")[0]}`}
                      </button>
                    )}
                  </div>
                );
              })}

            </div>

          </section>

        </motion.main>

      </motion.div>

      {/* Snippet Modal */}

      {showSnippetModal && (

        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/80 backdrop-blur-sm">

          <motion.div

            initial={{ opacity: 0, scale: 0.95 }}

            animate={{ opacity: 1, scale: 1 }}

            className="bg-[#0f0f0f] border border-white/10 p-6 rounded-2xl w-full max-w-2xl shadow-2xl relative"

          >

            <div className="absolute top-4 right-4 cursor-pointer text-gray-500 hover:text-white" onClick={() => setShowSnippetModal(null)}>

              ✕

            </div>

            <h2 className="text-xl font-bold text-white mb-2 flex items-center gap-2">

              <CheckCircle2 className="text-emerald-400 w-6 h-6" />

              Key Generated Successfully!

            </h2>

            <p className="text-sm text-gray-400 mb-6">Here is how to use your new virtual key in your applications. The base URL points to the Synapse Proxy proxy.</p>

            <div className="space-y-4">

              <div>

                <h3 className="text-sm font-bold text-gray-300 mb-2 uppercase tracking-wide">cURL</h3>

                <pre className="bg-black border border-white/10 p-4 rounded-xl text-xs font-mono text-emerald-400 overflow-x-auto">

{`curl -X POST ${process.env.NEXT_PUBLIC_PROXY_URL || 'https://synapse-proxy.com'}/v1/chat/completions \\

  -H "Content-Type: application/json" \\

  -H "Authorization: Bearer ${showSnippetModal}" \\

  -d '{

    "model": "gpt-4o",

    "messages": [{"role": "user", "content": "Hello, world!"}]

  }'`}

                </pre>

              </div>

              <div>

                <h3 className="text-sm font-bold text-gray-300 mb-2 uppercase tracking-wide">Python (OpenAI SDK)</h3>

                <pre className="bg-black border border-white/10 p-4 rounded-xl text-xs font-mono text-blue-300 overflow-x-auto">

{`from openai import OpenAI

client = OpenAI(

  base_url="${process.env.NEXT_PUBLIC_PROXY_URL || 'http://localhost:8080'}/v1",

  api_key="${showSnippetModal}"

)

response = client.chat.completions.create(

  model="gpt-4o",

  messages=[{"role": "user", "content": "Hello, world!"}]

)

print(response.choices[0].message.content)`}

                </pre>

              </div>

              <div>

                <h3 className="text-sm font-bold text-gray-300 mb-2 uppercase tracking-wide">Claude Code</h3>

                <pre className="bg-black border border-white/10 p-4 rounded-xl text-xs font-mono text-amber-400 overflow-x-auto">

{`# Configure Claude Code to use Synapse Proxy (port 8080 local)
export CLAUDE_BASE_URL="${process.env.NEXT_PUBLIC_PROXY_URL || 'http://localhost:8080'}/v1"
export ANTHROPIC_API_KEY="${showSnippetModal}"

# Run Claude Code normally!
claude`}

                </pre>

              </div>

              <div>

                <h3 className="text-sm font-bold text-gray-300 mb-2 uppercase tracking-wide">Cursor / VS Code</h3>

                <pre className="bg-black border border-white/10 p-4 rounded-xl text-xs font-mono text-cyan-300 overflow-x-auto">

{`Settings -> Models -> OpenAI or Anthropic -> Override URL:
${process.env.NEXT_PUBLIC_PROXY_URL || 'http://localhost:8080'}/v1

API Key:
${showSnippetModal}`}

                </pre>

              </div>

            </div>

            <div className="mt-6 flex justify-end">

              <button 

                onClick={() => setShowSnippetModal(null)}

                className="px-6 py-2 bg-white/10 hover:bg-white/20 text-white font-bold rounded-lg transition-colors"

              >

                Done

              </button>

            </div>

          </motion.div>

        </div>

      )}

      {/* Firewall Modal — Agent Firewall configuration for one key.

        Lets the user toggle the kill switch, the L1/L2/L3 cache
        stages, the per-session token limit, the allowed-tools
        whitelist, and PII redaction. Each toggle maps 1:1 to a
        field in dashboard/prisma/schema.prisma (ApiKey model)
        and to a Redis field that the proxy reads on every
        request (proxy/internal/services/auth.go). */}

      {showFirewallModal && (

        <FirewallModal

          key={showFirewallModal.id}

          apiKey={showFirewallModal}

          onClose={() => setShowFirewallModal(null)}

          onSave={saveFirewall}

        />

      )}

    </div>

  );

}
