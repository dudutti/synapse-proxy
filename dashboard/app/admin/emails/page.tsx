"use client";

import { useState, useEffect } from "react";
import { toast } from "sonner";
import { Mail, Save, Plus, Trash2, Eye } from "lucide-react";

export default function EmailsPage() {
  const [templates, setTemplates] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  
  // Selected Template State
  const [selectedId, setSelectedId] = useState<string>("");
  const [subject, setSubject] = useState("");
  const [html, setHtml] = useState("");

  const [previewMode, setPreviewMode] = useState(false);

  useEffect(() => {
    fetchTemplates();
  }, []);

  const fetchTemplates = async () => {
    setLoading(true);
    try {
      const res = await fetch("/api/admin/emails");
      if (res.ok) {
        const data = await res.json();
        setTemplates(data);
        if (data.length > 0 && !selectedId) {
          selectTemplate(data[0]);
        }
      }
    } catch (error) {
      toast.error("Failed to load templates");
    } finally {
      setLoading(false);
    }
  };

  const selectTemplate = (t: any) => {
    setSelectedId(t.id);
    setSubject(t.subject);
    setHtml(t.html);
  };

  const createNew = () => {
    const id = prompt("Enter new Template ID (e.g. WELCOME_VERIFY):");
    if (!id) return;
    setSelectedId(id.toUpperCase().replace(/\s+/g, '_'));
    setSubject("");
    setHtml("");
  };

  const saveTemplate = async () => {
    if (!selectedId || !subject || !html) {
      toast.error("Please fill in all fields");
      return;
    }

    const promise = fetch("/api/admin/emails", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ id: selectedId, subject, html })
    }).then(async (res) => {
      if (!res.ok) throw new Error("Failed to save");
      await fetchTemplates();
      return "Template saved successfully";
    });

    toast.promise(promise, {
      loading: "Saving template...",
      success: (data) => data,
      error: "Could not save template"
    });
  };

  if (loading) return <div className="p-8 text-gray-400">Loading templates...</div>;

  return (
    <div className="p-8 max-w-6xl mx-auto">
      <div className="flex items-center justify-between mb-8">
        <div>
          <h1 className="text-3xl font-black text-white flex items-center gap-3">
            <Mail className="w-8 h-8 text-indigo-500" />
            Email Templates
          </h1>
          <p className="text-gray-400 mt-2">Manage the HTML content for automated system emails.</p>
        </div>
        <button 
          onClick={saveTemplate}
          className="flex items-center gap-2 bg-indigo-600 hover:bg-indigo-500 text-white px-6 py-2 rounded-xl font-bold transition-colors shadow-lg shadow-indigo-500/20"
        >
          <Save className="w-4 h-4" /> Save Template
        </button>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-4 gap-8">
        {/* Sidebar List */}
        <div className="lg:col-span-1 flex flex-col gap-3">
          <button 
            onClick={createNew}
            className="flex items-center justify-center gap-2 bg-white/5 border border-white/10 hover:bg-white/10 text-white px-4 py-3 rounded-xl font-medium transition-colors border-dashed"
          >
            <Plus className="w-4 h-4" /> New Template
          </button>
          
          <div className="flex flex-col gap-2 mt-4">
            <h3 className="text-xs font-bold text-gray-500 uppercase tracking-wider mb-2">Existing Templates</h3>
            {templates.map(t => (
              <button
                key={t.id}
                onClick={() => selectTemplate(t)}
                className={`text-left px-4 py-3 rounded-xl transition-all font-mono text-xs ${
                  selectedId === t.id 
                    ? "bg-indigo-500/20 text-indigo-300 border border-indigo-500/30" 
                    : "bg-black/40 text-gray-400 border border-white/5 hover:bg-white/5 hover:text-white"
                }`}
              >
                {t.id}
              </button>
            ))}
            {templates.length === 0 && (
              <p className="text-sm text-gray-600 italic">No templates created yet.</p>
            )}
          </div>
        </div>

        {/* Editor */}
        <div className="lg:col-span-3 bg-[#0a0a0c] border border-white/10 rounded-2xl p-6 shadow-2xl flex flex-col h-[700px]">
          {selectedId ? (
            <>
              <div className="flex flex-col gap-4 mb-6">
                <div>
                  <label className="block text-xs font-bold text-gray-500 uppercase tracking-wider mb-2">Template ID</label>
                  <input 
                    type="text" 
                    value={selectedId}
                    disabled
                    className="w-full bg-black/50 border border-white/5 rounded-xl px-4 py-3 text-white font-mono text-sm opacity-70"
                  />
                </div>
                <div>
                  <label className="block text-xs font-bold text-gray-500 uppercase tracking-wider mb-2">Subject Line</label>
                  <input 
                    type="text" 
                    value={subject}
                    onChange={(e) => setSubject(e.target.value)}
                    placeholder="Verify your email..."
                    className="w-full bg-black/50 border border-white/10 rounded-xl px-4 py-3 text-white focus:border-indigo-500 outline-none transition-colors"
                  />
                </div>
              </div>

              <div className="flex items-center justify-between mb-2">
                <label className="block text-xs font-bold text-gray-500 uppercase tracking-wider">HTML Body</label>
                <div className="flex gap-2">
                  <button 
                    onClick={() => setPreviewMode(false)}
                    className={`px-3 py-1 text-xs font-bold rounded ${!previewMode ? 'bg-indigo-500/20 text-indigo-400' : 'text-gray-500 hover:text-gray-300'}`}
                  >
                    Code
                  </button>
                  <button 
                    onClick={() => setPreviewMode(true)}
                    className={`flex items-center gap-1 px-3 py-1 text-xs font-bold rounded ${previewMode ? 'bg-indigo-500/20 text-indigo-400' : 'text-gray-500 hover:text-gray-300'}`}
                  >
                    <Eye className="w-3 h-3" /> Preview
                  </button>
                </div>
              </div>

              <div className="flex-1 bg-black/50 border border-white/10 rounded-xl overflow-hidden relative">
                {previewMode ? (
                  <div className="w-full h-full bg-white p-8 overflow-auto">
                    <div dangerouslySetInnerHTML={{ __html: html || "<p style='color:gray'>Empty template</p>" }} />
                  </div>
                ) : (
                  <textarea
                    value={html}
                    onChange={(e) => setHtml(e.target.value)}
                    placeholder="<div>...</div>"
                    className="w-full h-full bg-transparent p-4 text-emerald-400 font-mono text-sm resize-none focus:outline-none"
                    spellCheck={false}
                  />
                )}
              </div>
              <p className="text-xs text-gray-500 mt-4">
                Available variables: <code className="text-indigo-400 bg-indigo-500/10 px-1 rounded">{"{{URL}}"}</code>
              </p>
            </>
          ) : (
            <div className="flex-1 flex flex-col items-center justify-center text-gray-500">
              <Mail className="w-16 h-16 mb-4 opacity-20" />
              <p>Select or create a template to start editing</p>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
