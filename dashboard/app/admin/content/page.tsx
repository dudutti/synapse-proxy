"use client";

import { useState, useEffect } from "react";
import { toast } from "sonner";
import { Save, Plus, Trash2, Globe, FileText, ChevronRight, HelpCircle } from "lucide-react";

interface Section {
  title: string;
  desc: string;
  items: string[];
  color: string;
}

interface TableRow {
  feature: string;
  synapse: string;
  other: string;
}

interface Table {
  headers: string[];
  rows: TableRow[];
}

interface ContentState {
  metaTitle: string;
  metaDesc: string;
  heroBadge: string;
  heroTitle: string;
  heroDesc: string;
  backBtn: string;
  dashboardBtn: string;
  sections: Section[];
  videoTitle: string;
  videoDesc: string;
  videoAlt: string;
  videoConsoleTitle?: string;
  videoConsoleItems?: string[];
  table?: Table;
}

const PAGES_LIST = [
  { key: "caching", name: "Cache Multi-Niveaux (Landing)" },
  { key: "firewall", name: "Agentic Firewall (Landing)" },
  { key: "compression", name: "Compression de Contexte (Landing)" },
  { key: "mcp", name: "Serveur MCP (Landing)" },
  { key: "costReduction", name: "Réduction des Coûts (Landing)" },
  { key: "agentSafety", name: "Sécurité des Agents (Landing)" },
  { key: "enterpriseGateway", name: "Passerelle Entreprise (Landing)" },
  { key: "litellm", name: "vs LiteLLM (Landing)" },
  { key: "portkey", name: "vs Portkey.ai (Landing)" },
  { key: "llmlingua", name: "vs LLMLingua (Landing)" },
  { key: "cgv", name: "CGV / CGU (Page Légale)" },
  { key: "privacy", name: "Politique de Confidentialité (Page Légale)" },
  { key: "legal", name: "Mentions Légales (Page Légale)" },
];

export default function AdminContentPage() {
  const [selectedPage, setSelectedPage] = useState("caching");
  const [selectedLang, setSelectedLang] = useState<"fr" | "en">("fr");
  const [isLoading, setIsLoading] = useState(false);
  const [isSaving, setIsSaving] = useState(false);
  
  const [state, setState] = useState<ContentState>({
    metaTitle: "",
    metaDesc: "",
    heroBadge: "",
    heroTitle: "",
    heroDesc: "",
    backBtn: "",
    dashboardBtn: "",
    sections: [],
    videoTitle: "",
    videoDesc: "",
    videoAlt: "",
    videoConsoleTitle: "",
    videoConsoleItems: [],
  });

  const fetchContent = async (page: string, lang: "fr" | "en") => {
    setIsLoading(true);
    try {
      const res = await fetch(`/api/admin/content?pageKey=${page}&lang=${lang}`);
      if (!res.ok) throw new Error("Failed to fetch");
      const data = await res.json();
      setState({
        metaTitle: data.metaTitle || "",
        metaDesc: data.metaDesc || "",
        heroBadge: data.heroBadge || "",
        heroTitle: data.heroTitle || "",
        heroDesc: data.heroDesc || "",
        backBtn: data.backBtn || "",
        dashboardBtn: data.dashboardBtn || "",
        sections: data.sections || [],
        videoTitle: data.videoTitle || "",
        videoDesc: data.videoDesc || "",
        videoAlt: data.videoAlt || "",
        videoConsoleTitle: data.videoConsoleTitle || "",
        videoConsoleItems: data.videoConsoleItems || [],
        table: data.table || undefined,
      });
    } catch (err) {
      console.error(err);
      toast.error("Erreur lors du chargement du contenu");
    } finally {
      setIsLoading(false);
    }
  };

  useEffect(() => {
    fetchContent(selectedPage, selectedLang);
  }, [selectedPage, selectedLang]);

  const handleSave = async () => {
    setIsSaving(true);
    try {
      const res = await fetch("/api/admin/content", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          pageKey: selectedPage,
          lang: selectedLang,
          content: state,
        }),
      });
      if (!res.ok) throw new Error("Failed to save");
      toast.success("Contenu enregistré avec succès !");
    } catch (err) {
      console.error(err);
      toast.error("Erreur lors de l'enregistrement");
    } finally {
      setIsSaving(false);
    }
  };

  const updateField = (field: keyof ContentState, value: any) => {
    setState((prev) => ({ ...prev, [field]: value }));
  };

  const handleSectionChange = (index: number, key: keyof Section, value: any) => {
    setState((prev) => {
      const newSections = [...prev.sections];
      newSections[index] = { ...newSections[index], [key]: value };
      return { ...prev, sections: newSections };
    });
  };

  const addSection = () => {
    setState((prev) => ({
      ...prev,
      sections: [
        ...prev.sections,
        { title: "Nouveau titre", desc: "Nouvelle description", items: [], color: "emerald" },
      ],
    }));
  };

  const deleteSection = (index: number) => {
    setState((prev) => ({
      ...prev,
      sections: prev.sections.filter((_, i) => i !== index),
    }));
  };

  const addBulletPoint = (sectionIndex: number) => {
    setState((prev) => {
      const newSections = [...prev.sections];
      newSections[sectionIndex].items = [...newSections[sectionIndex].items, "Nouvel élément"];
      return { ...prev, sections: newSections };
    });
  };

  const deleteBulletPoint = (sectionIndex: number, bulletIndex: number) => {
    setState((prev) => {
      const newSections = [...prev.sections];
      newSections[sectionIndex].items = newSections[sectionIndex].items.filter((_, i) => i !== bulletIndex);
      return { ...prev, sections: newSections };
    });
  };

  const updateBulletPoint = (sectionIndex: number, bulletIndex: number, value: string) => {
    setState((prev) => {
      const newSections = [...prev.sections];
      newSections[sectionIndex].items[bulletIndex] = value;
      return { ...prev, sections: newSections };
    });
  };

  // Video Console Items helpers
  const addConsoleItem = () => {
    setState((prev) => ({
      ...prev,
      videoConsoleItems: [...(prev.videoConsoleItems || []), "Nouveau log console"],
    }));
  };

  const deleteConsoleItem = (index: number) => {
    setState((prev) => ({
      ...prev,
      videoConsoleItems: (prev.videoConsoleItems || []).filter((_, i) => i !== index),
    }));
  };

  const updateConsoleItem = (index: number, value: string) => {
    setState((prev) => {
      const newItems = [...(prev.videoConsoleItems || [])];
      newItems[index] = value;
      return { ...prev, videoConsoleItems: newItems };
    });
  };

  // Table helpers
  const initTable = () => {
    setState((prev) => ({
      ...prev,
      table: {
        headers: ["Fonctionnalité", "Synapse Proxy", "Concurrent"],
        rows: [{ feature: "Exemple", synapse: "Oui", other: "Non" }],
      },
    }));
  };

  const removeTable = () => {
    setState((prev) => {
      const { table, ...rest } = prev;
      return rest;
    });
  };

  const handleTableHeaderChange = (index: number, value: string) => {
    setState((prev) => {
      if (!prev.table) return prev;
      const newHeaders = [...prev.table.headers];
      newHeaders[index] = value;
      return { ...prev, table: { ...prev.table, headers: newHeaders } };
    });
  };

  const handleTableRowChange = (index: number, key: keyof TableRow, value: string) => {
    setState((prev) => {
      if (!prev.table) return prev;
      const newRows = [...prev.table.rows];
      newRows[index] = { ...newRows[index], [key]: value };
      return { ...prev, table: { ...prev.table, rows: newRows } };
    });
  };

  const addTableRow = () => {
    setState((prev) => {
      if (!prev.table) return prev;
      return {
        ...prev,
        table: {
          ...prev.table,
          rows: [...prev.table.rows, { feature: "", synapse: "", other: "" }],
        },
      };
    });
  };

  const deleteTableRow = (index: number) => {
    setState((prev) => {
      if (!prev.table) return prev;
      return {
        ...prev,
        table: {
          ...prev.table,
          rows: prev.table.rows.filter((_, i) => i !== index),
        },
      };
    });
  };

  return (
    <div className="p-8 max-w-5xl mx-auto space-y-8">
      {/* Header */}
      <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 border-b border-white/10 pb-6">
        <div>
          <h1 className="text-3xl font-black tracking-tight text-white flex items-center gap-3">
            <FileText className="w-8 h-8 text-emerald-400" />
            Administration du Contenu
          </h1>
          <p className="text-gray-400 text-sm mt-1">
            Modifiez en temps réel les textes, médias et sections des pages d'atterrissage et pages légales.
          </p>
        </div>

        <button
          onClick={handleSave}
          disabled={isSaving || isLoading}
          className="flex items-center justify-center gap-2 px-5 py-2.5 bg-emerald-500 hover:bg-emerald-400 disabled:bg-emerald-800 disabled:text-gray-400 text-black font-bold rounded-xl transition-all shadow-[0_0_20px_rgba(16,185,129,0.2)] shrink-0"
        >
          <Save className="w-4 h-4" />
          {isSaving ? "Enregistrement..." : "Enregistrer les modifications"}
        </button>
      </div>

      {/* Selectors */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4 bg-[#0f0f11] border border-white/5 p-4 rounded-2xl">
        <div>
          <label className="block text-xs font-bold text-gray-400 uppercase tracking-wider mb-2">Page à administrer</label>
          <select
            value={selectedPage}
            onChange={(e) => setSelectedPage(e.target.value)}
            className="w-full bg-[#050505] border border-white/10 rounded-xl px-4 py-2.5 text-sm font-bold text-white focus:outline-none focus:border-emerald-500 transition-colors"
          >
            {PAGES_LIST.map((p) => (
              <option key={p.key} value={p.key}>
                {p.name}
              </option>
            ))}
          </select>
        </div>

        <div>
          <label className="block text-xs font-bold text-gray-400 uppercase tracking-wider mb-2">Langue de saisie</label>
          <div className="grid grid-cols-2 gap-2 h-[42px]">
            <button
              onClick={() => setSelectedLang("fr")}
              className={`flex items-center justify-center gap-2 rounded-xl border text-sm font-bold transition-all ${
                selectedLang === "fr"
                  ? "bg-emerald-500/10 border-emerald-500 text-emerald-400"
                  : "bg-[#050505] border-white/10 text-gray-400 hover:text-white"
              }`}
            >
              🇫🇷 Français (FR)
            </button>
            <button
              onClick={() => setSelectedLang("en")}
              className={`flex items-center justify-center gap-2 rounded-xl border text-sm font-bold transition-all ${
                selectedLang === "en"
                  ? "bg-emerald-500/10 border-emerald-500 text-emerald-400"
                  : "bg-[#050505] border-white/10 text-gray-400 hover:text-white"
              }`}
            >
              🇬🇧 English (EN)
            </button>
          </div>
        </div>
      </div>

      {isLoading ? (
        <div className="flex flex-col items-center justify-center py-20 gap-4">
          <div className="w-10 h-10 border-4 border-emerald-500/20 border-t-emerald-500 rounded-full animate-spin" />
          <p className="text-gray-400 text-sm">Chargement du contenu...</p>
        </div>
      ) : (
        <div className="space-y-8">
          {/* SEO & Header Info */}
          <div className="bg-[#0f0f11]/60 border border-white/5 p-6 rounded-3xl space-y-6">
            <h3 className="text-lg font-bold border-b border-white/5 pb-3 text-gray-200">SEO & En-tête</h3>
            
            <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
              <div>
                <label className="block text-xs font-bold text-gray-400 uppercase mb-2">Meta Title (SEO)</label>
                <input
                  type="text"
                  value={state.metaTitle}
                  onChange={(e) => updateField("metaTitle", e.target.value)}
                  className="w-full bg-black/40 border border-white/10 rounded-xl px-4 py-2.5 text-xs text-white focus:outline-none focus:border-emerald-500 transition-colors"
                />
              </div>
              <div>
                <label className="block text-xs font-bold text-gray-400 uppercase mb-2">Hero Badge</label>
                <input
                  type="text"
                  value={state.heroBadge}
                  onChange={(e) => updateField("heroBadge", e.target.value)}
                  className="w-full bg-black/40 border border-white/10 rounded-xl px-4 py-2.5 text-xs text-white focus:outline-none focus:border-emerald-500 transition-colors"
                />
              </div>
            </div>

            <div>
              <label className="block text-xs font-bold text-gray-400 uppercase mb-2">Meta Description (SEO)</label>
              <textarea
                value={state.metaDesc}
                onChange={(e) => updateField("metaDesc", e.target.value)}
                rows={2}
                className="w-full bg-black/40 border border-white/10 rounded-xl p-4 text-xs text-white focus:outline-none focus:border-emerald-500 transition-colors"
              />
            </div>

            <div>
              <label className="block text-xs font-bold text-gray-400 uppercase mb-2">Hero Title</label>
              <input
                type="text"
                value={state.heroTitle}
                onChange={(e) => updateField("heroTitle", e.target.value)}
                className="w-full bg-black/40 border border-white/10 rounded-xl px-4 py-2.5 text-xs text-white focus:outline-none focus:border-emerald-500 transition-colors"
              />
            </div>

            <div>
              <label className="block text-xs font-bold text-gray-400 uppercase mb-2">Hero Description</label>
              <textarea
                value={state.heroDesc}
                onChange={(e) => updateField("heroDesc", e.target.value)}
                rows={3}
                className="w-full bg-black/40 border border-white/10 rounded-xl p-4 text-xs text-white focus:outline-none focus:border-emerald-500 transition-colors"
              />
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
              <div>
                <label className="block text-xs font-bold text-gray-400 uppercase mb-2">Texte bouton "Retour"</label>
                <input
                  type="text"
                  value={state.backBtn}
                  onChange={(e) => updateField("backBtn", e.target.value)}
                  className="w-full bg-black/40 border border-white/10 rounded-xl px-4 py-2.5 text-xs text-white focus:outline-none focus:border-emerald-500 transition-colors"
                />
              </div>
              <div>
                <label className="block text-xs font-bold text-gray-400 uppercase mb-2">Texte bouton "Dashboard"</label>
                <input
                  type="text"
                  value={state.dashboardBtn}
                  onChange={(e) => updateField("dashboardBtn", e.target.value)}
                  className="w-full bg-black/40 border border-white/10 rounded-xl px-4 py-2.5 text-xs text-white focus:outline-none focus:border-emerald-500 transition-colors"
                />
              </div>
            </div>
          </div>

          {/* Sections list */}
          <div className="bg-[#0f0f11]/60 border border-white/5 p-6 rounded-3xl space-y-6">
            <div className="flex items-center justify-between border-b border-white/5 pb-3">
              <h3 className="text-lg font-bold text-gray-200">Clauses / Sections de Contenu</h3>
              <button
                onClick={addSection}
                className="flex items-center gap-1.5 px-3 py-1.5 bg-emerald-500/10 hover:bg-emerald-500/20 text-emerald-400 text-xs font-bold rounded-lg border border-emerald-500/20 transition-all"
              >
                <Plus className="w-3.5 h-3.5" /> Ajouter une section
              </button>
            </div>

            {state.sections.length === 0 ? (
              <p className="text-gray-500 text-xs text-center py-6">Aucune section définie.</p>
            ) : (
              <div className="space-y-6">
                {state.sections.map((sec, idx) => (
                  <div key={idx} className="bg-black/30 border border-white/5 p-5 rounded-2xl relative space-y-4">
                    <button
                      onClick={() => deleteSection(idx)}
                      className="absolute top-4 right-4 text-gray-500 hover:text-red-400 p-1 transition-colors"
                      title="Supprimer la section"
                    >
                      <Trash2 className="w-4 h-4" />
                    </button>

                    <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                      <div className="md:col-span-2">
                        <label className="block text-[10px] font-bold text-gray-500 uppercase mb-1">Titre de section</label>
                        <input
                          type="text"
                          value={sec.title}
                          onChange={(e) => handleSectionChange(idx, "title", e.target.value)}
                          className="w-full bg-black/40 border border-white/10 rounded-lg px-3 py-1.5 text-xs text-white focus:outline-none focus:border-emerald-500"
                        />
                      </div>
                      <div>
                        <label className="block text-[10px] font-bold text-gray-500 uppercase mb-1">Couleur graphique</label>
                        <select
                          value={sec.color}
                          onChange={(e) => handleSectionChange(idx, "color", e.target.value)}
                          className="w-full bg-black/40 border border-white/10 rounded-lg px-3 py-1.5 text-xs text-white focus:outline-none"
                        >
                          <option value="emerald">Emerald (Vert)</option>
                          <option value="teal">Teal (Sarcelle)</option>
                          <option value="cyan">Cyan (Bleu clair)</option>
                          <option value="blue">Blue (Bleu)</option>
                          <option value="indigo">Indigo (Violet foncé)</option>
                          <option value="violet">Violet (Pourpre)</option>
                          <option value="purple">Purple (Violet clair)</option>
                          <option value="amber">Amber (Ambre)</option>
                          <option value="red">Red (Rouge)</option>
                        </select>
                      </div>
                    </div>

                    <div>
                      <label className="block text-[10px] font-bold text-gray-500 uppercase mb-1">Description principale</label>
                      <textarea
                        value={sec.desc}
                        onChange={(e) => handleSectionChange(idx, "desc", e.target.value)}
                        rows={2}
                        className="w-full bg-black/40 border border-white/10 rounded-lg p-3 text-xs text-white focus:outline-none focus:border-emerald-500"
                      />
                    </div>

                    {/* Bullet points sub list */}
                    <div className="space-y-2 pl-4 border-l border-white/10">
                      <div className="flex items-center justify-between">
                        <label className="block text-[10px] font-bold text-gray-400 uppercase">Points Clés / Liste à puces</label>
                        <button
                          onClick={() => addBulletPoint(idx)}
                          className="flex items-center gap-1 text-[10px] text-emerald-400 hover:underline"
                        >
                          <Plus className="w-3 h-3" /> Ajouter un point
                        </button>
                      </div>
                      {sec.items.map((item, bIdx) => (
                        <div key={bIdx} className="flex items-center gap-2">
                          <ChevronRight className="w-3.5 h-3.5 text-emerald-400 shrink-0" />
                          <input
                            type="text"
                            value={item}
                            onChange={(e) => updateBulletPoint(idx, bIdx, e.target.value)}
                            className="flex-1 bg-black/40 border border-white/10 rounded-lg px-2.5 py-1 text-xs text-white focus:outline-none"
                          />
                          <button
                            onClick={() => deleteBulletPoint(idx, bIdx)}
                            className="text-gray-500 hover:text-red-400 p-1"
                          >
                            <Trash2 className="w-3.5 h-3.5" />
                          </button>
                        </div>
                      ))}
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>

          {/* Video Section Settings (Only for Landing pages, ignored if empty) */}
          <div className="bg-[#0f0f11]/60 border border-white/5 p-6 rounded-3xl space-y-6">
            <h3 className="text-lg font-bold border-b border-white/5 pb-3 text-gray-200">Média & Console</h3>
            
            <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
              <div>
                <label className="block text-xs font-bold text-gray-400 uppercase mb-2">Titre de la Section Vidéo</label>
                <input
                  type="text"
                  value={state.videoTitle}
                  onChange={(e) => updateField("videoTitle", e.target.value)}
                  className="w-full bg-black/40 border border-white/10 rounded-xl px-4 py-2.5 text-xs text-white focus:outline-none focus:border-emerald-500"
                />
              </div>
              <div>
                <label className="block text-xs font-bold text-gray-400 uppercase mb-2">Texte Alternatif Vidéo / Image</label>
                <input
                  type="text"
                  value={state.videoAlt}
                  onChange={(e) => updateField("videoAlt", e.target.value)}
                  className="w-full bg-black/40 border border-white/10 rounded-xl px-4 py-2.5 text-xs text-white focus:outline-none focus:border-emerald-500"
                />
              </div>
            </div>

            <div>
              <label className="block text-xs font-bold text-gray-400 uppercase mb-2">Description Vidéo</label>
              <textarea
                value={state.videoDesc}
                onChange={(e) => updateField("videoDesc", e.target.value)}
                rows={2}
                className="w-full bg-black/40 border border-white/10 rounded-xl p-4 text-xs text-white focus:outline-none focus:border-emerald-500"
              />
            </div>

            {/* Optional console logs simulation */}
            <div className="border-t border-white/5 pt-6 space-y-4">
              <div className="flex items-center justify-between">
                <div>
                  <h4 className="text-sm font-bold text-gray-300">Console Interactive de Démonstration</h4>
                  <p className="text-[10px] text-gray-500 mt-0.5">Logs simulés affichés sous la vidéo de démonstration.</p>
                </div>
                <button
                  onClick={addConsoleItem}
                  className="flex items-center gap-1 px-2.5 py-1 bg-white/5 hover:bg-white/10 border border-white/10 text-[10px] font-bold rounded-lg text-gray-300"
                >
                  <Plus className="w-3 h-3" /> Ajouter un log
                </button>
              </div>

              <div className="grid grid-cols-1 gap-4">
                <div>
                  <label className="block text-[10px] font-bold text-gray-400 uppercase mb-1.5">Titre de la Console</label>
                  <input
                    type="text"
                    value={state.videoConsoleTitle || ""}
                    onChange={(e) => updateField("videoConsoleTitle", e.target.value)}
                    className="w-full bg-black/40 border border-white/10 rounded-lg px-3 py-1.5 text-xs text-white focus:outline-none focus:border-emerald-500"
                  />
                </div>

                <div className="space-y-2">
                  <label className="block text-[10px] font-bold text-gray-400 uppercase">Lignes de Log Console</label>
                  {(state.videoConsoleItems || []).map((item, cIdx) => (
                    <div key={cIdx} className="flex items-center gap-2">
                      <input
                        type="text"
                        value={item}
                        onChange={(e) => updateConsoleItem(cIdx, e.target.value)}
                        className="flex-1 bg-black/40 border border-white/10 rounded-lg px-3 py-1.5 text-xs text-white focus:outline-none font-mono"
                      />
                      <button
                        onClick={() => deleteConsoleItem(cIdx)}
                        className="text-gray-500 hover:text-red-400 p-1"
                      >
                        <Trash2 className="w-3.5 h-3.5" />
                      </button>
                    </div>
                  ))}
                </div>
              </div>
            </div>
          </div>

          {/* Comparison Table Section (Optional) */}
          <div className="bg-[#0f0f11]/60 border border-white/5 p-6 rounded-3xl space-y-6">
            <div className="flex items-center justify-between border-b border-white/5 pb-3">
              <div>
                <h3 className="text-lg font-bold text-gray-200">Tableau de Comparaison</h3>
                <p className="text-[10px] text-gray-500 mt-0.5">Utilisé principalement pour les pages comparatives (ex: vs LiteLLM).</p>
              </div>
              {!state.table ? (
                <button
                  onClick={initTable}
                  className="flex items-center gap-1.5 px-3 py-1.5 bg-emerald-500/10 hover:bg-emerald-500/20 text-emerald-400 text-xs font-bold rounded-lg border border-emerald-500/20 transition-all"
                >
                  <Plus className="w-3.5 h-3.5" /> Activer le tableau
                </button>
              ) : (
                <button
                  onClick={removeTable}
                  className="flex items-center gap-1.5 px-3 py-1.5 bg-red-500/10 hover:bg-red-500/20 text-red-400 text-xs font-bold rounded-lg border border-red-500/20 transition-all"
                >
                  <Trash2 className="w-3.5 h-3.5" /> Supprimer le tableau
                </button>
              )}
            </div>

            {state.table && (
              <div className="space-y-4">
                <div className="grid grid-cols-3 gap-4 border-b border-white/10 pb-4">
                  <div>
                    <label className="block text-[10px] font-bold text-gray-500 uppercase mb-1">En-tête 1 (Caractéristique)</label>
                    <input
                      type="text"
                      value={state.table.headers[0] || ""}
                      onChange={(e) => handleTableHeaderChange(0, e.target.value)}
                      className="w-full bg-black/40 border border-white/10 rounded-lg px-3 py-1.5 text-xs text-white focus:outline-none"
                    />
                  </div>
                  <div>
                    <label className="block text-[10px] font-bold text-gray-500 uppercase mb-1">En-tête 2 (Synapse Proxy)</label>
                    <input
                      type="text"
                      value={state.table.headers[1] || ""}
                      onChange={(e) => handleTableHeaderChange(1, e.target.value)}
                      className="w-full bg-black/40 border border-white/10 rounded-lg px-3 py-1.5 text-xs text-white focus:outline-none"
                    />
                  </div>
                  <div>
                    <label className="block text-[10px] font-bold text-gray-500 uppercase mb-1">En-tête 3 (Concurrent)</label>
                    <input
                      type="text"
                      value={state.table.headers[2] || ""}
                      onChange={(e) => handleTableHeaderChange(2, e.target.value)}
                      className="w-full bg-black/40 border border-white/10 rounded-lg px-3 py-1.5 text-xs text-white focus:outline-none"
                    />
                  </div>
                </div>

                <div className="space-y-3">
                  <div className="flex items-center justify-between">
                    <label className="block text-xs font-bold text-gray-400">Lignes du tableau</label>
                    <button
                      onClick={addTableRow}
                      className="flex items-center gap-1 text-[10px] text-emerald-400 hover:underline"
                    >
                      <Plus className="w-3 h-3" /> Ajouter une ligne
                    </button>
                  </div>

                  {state.table.rows.map((row, rIdx) => (
                    <div key={rIdx} className="grid grid-cols-12 gap-3 items-center bg-black/20 p-3 rounded-xl border border-white/5">
                      <div className="col-span-4">
                        <input
                          type="text"
                          placeholder="Caractéristique"
                          value={row.feature}
                          onChange={(e) => handleTableRowChange(rIdx, "feature", e.target.value)}
                          className="w-full bg-black/40 border border-white/10 rounded-lg px-2.5 py-1 text-xs text-white focus:outline-none"
                        />
                      </div>
                      <div className="col-span-4">
                        <input
                          type="text"
                          placeholder="Synapse Proxy status"
                          value={row.synapse}
                          onChange={(e) => handleTableRowChange(rIdx, "synapse", e.target.value)}
                          className="w-full bg-black/40 border border-white/10 rounded-lg px-2.5 py-1 text-xs text-white focus:outline-none"
                        />
                      </div>
                      <div className="col-span-3">
                        <input
                          type="text"
                          placeholder="Concurrent status"
                          value={row.other}
                          onChange={(e) => handleTableRowChange(rIdx, "other", e.target.value)}
                          className="w-full bg-black/40 border border-white/10 rounded-lg px-2.5 py-1 text-xs text-white focus:outline-none"
                        />
                      </div>
                      <div className="col-span-1 flex justify-center">
                        <button
                          onClick={() => deleteTableRow(rIdx)}
                          className="text-gray-500 hover:text-red-400 p-1"
                        >
                          <Trash2 className="w-3.5 h-3.5" />
                        </button>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
