"use client";

import { useState, useEffect, useRef, useMemo } from "react";
import { useRouter, useParams } from "next/navigation";
import { Save, ArrowLeft, Globe, EyeOff, FileText, User } from "lucide-react";
import { toast } from "sonner";
import Link from "next/link";
import dynamic from "next/dynamic";

// ReactQuill must be loaded dynamically because it doesn't support SSR
const ReactQuill = dynamic(() => import("react-quill"), { ssr: false });
import "react-quill/dist/quill.snow.css";

export default function AdminBlogEditor() {
  const router = useRouter();
  const params = useParams();
  const isNew = params.id === "new";
  
  const quillRef = useRef<any>(null);

  const [isLoading, setIsLoading] = useState(!isNew);
  const [isSaving, setIsSaving] = useState(false);
  const [showHtml, setShowHtml] = useState(false);
  const [title, setTitle] = useState("");
  const [slug, setSlug] = useState("");
  const [content, setContent] = useState("");
  const [excerpt, setExcerpt] = useState("");
  const [coverImage, setCoverImage] = useState("");
  const [category, setCategory] = useState("");
  const [author, setAuthor] = useState("");
  const [lang, setLang] = useState("fr");
  const [published, setPublished] = useState(false);
  const [categories, setCategories] = useState<string[]>([]);

  useEffect(() => {
    fetchCategories();
    if (!isNew) {
      fetchPost();
    }
  }, [params.id]);

  const fetchCategories = async () => {
    try {
      const res = await fetch("/api/admin/blog/categories");
      if (res.ok) {
        const data = await res.json();
        setCategories(data);
      }
    } catch (e) {
      console.error(e);
    }
  };

  const fetchPost = async () => {
    setIsLoading(true);
    try {
      // In a real app we'd have a GET /api/admin/blog/[id]
      // Since we don't have it, we'll fetch all and find the one.
      const res = await fetch("/api/admin/blog");
      if (!res.ok) throw new Error("Failed to fetch");
      const data = await res.json();
      const post = data.find((p: any) => p.id === params.id);
      
      if (!post) {
        toast.error("Article introuvable");
        router.push("/admin/blog");
        return;
      }
      
      setTitle(post.title);
      setSlug(post.slug);
      setContent(post.content);
      setExcerpt(post.excerpt || "");
      setCoverImage(post.coverImage || "");
      setCategory(post.category || "");
      setLang(post.lang || "fr");
      setAuthor(post.author || "");
      setPublished(post.published);
    } catch (err) {
      toast.error("Erreur lors du chargement");
    } finally {
      setIsLoading(false);
    }
  };

  const handleTitleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const newTitle = e.target.value;
    setTitle(newTitle);
    if (isNew) {
      // Auto-generate slug
      setSlug(
        newTitle
          .toLowerCase()
          .replace(/[^a-z0-9]+/g, "-")
          .replace(/^-|-$/g, "")
      );
    }
  };

  const handleSave = async () => {
    if (!title || !slug || !content) {
      toast.error("Le titre, le slug et le contenu sont requis");
      return;
    }

    setIsSaving(true);
    try {
      const url = isNew ? "/api/admin/blog" : `/api/admin/blog/${params.id}`;
      const method = isNew ? "POST" : "PUT";

      const res = await fetch(url, {
        method,
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          title,
          slug,
          content,
          excerpt,
          coverImage,
          category,
          lang,
          author,
          published,
        }),
      });

      if (!res.ok) {
        const errText = await res.text();
        throw new Error(errText);
      }

      toast.success(isNew ? "Article créé avec succès" : "Article mis à jour");
      router.push("/admin/blog");
    } catch (err: any) {
      toast.error(err.message || "Erreur d'enregistrement");
    } finally {
      setIsSaving(false);
    }
  };

  const handleCoverUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;

    const formData = new FormData();
    formData.append("file", file);

    try {
      const toastId = toast.loading("Upload de l'image de couverture...");
      const res = await fetch("/api/admin/upload", {
        method: "POST",
        body: formData,
      });

      if (!res.ok) throw new Error("Upload failed");
      const data = await res.json();
      
      setCoverImage(data.url);
      toast.dismiss(toastId);
      toast.success("Image de couverture ajoutée");
    } catch (err) {
      toast.dismiss();
      toast.error("Erreur lors de l'upload");
    }
  };

  const imageHandler = function(this: any) {
    const quill = this.quill;
    const input = document.createElement("input");
    input.setAttribute("type", "file");
    input.setAttribute("accept", "image/*,video/*");
    input.click();

    input.onchange = async () => {
      const file = input.files ? input.files[0] : null;
      if (!file) return;

      const formData = new FormData();
      formData.append("file", file);

      try {
        const toastId = toast.loading("Téléchargement du média...");
        const res = await fetch("/api/admin/upload", {
          method: "POST",
          body: formData,
        });

        if (!res.ok) throw new Error("Upload failed");
        const data = await res.json();
        
        toast.dismiss(toastId);
        toast.success("Média téléchargé");

        if (quill) {
          quill.focus();
          const range = quill.getSelection(true);
          const cursorPosition = range ? range.index : quill.getLength();
          
          if (file.type.startsWith("video/")) {
            quill.clipboard.dangerouslyPasteHTML(cursorPosition, `<video controls src="${data.url}" style="max-width: 100%; border-radius: 8px;"></video><p><br></p>`);
          } else {
            quill.insertEmbed(cursorPosition, "image", data.url);
          }
        }
      } catch (e) {
        toast.dismiss();
        toast.error("Erreur lors du téléchargement du média");
      }
    };
  };

  const modules = useMemo(() => ({
    toolbar: {
      container: [
        [{ header: [1, 2, 3, false] }],
        ["bold", "italic", "underline", "strike", "blockquote"],
        [{ list: "ordered" }, { list: "bullet" }, { indent: "-1" }, { indent: "+1" }],
        ["link", "image", "video"],
        ["clean"],
      ],
      handlers: {
        image: imageHandler,
        video: imageHandler,
      },
    },
  }), []);

  return (
    <div className="p-8 max-w-5xl mx-auto space-y-8">
      {/* Header */}
      <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 border-b border-white/10 pb-6">
        <div className="flex items-center gap-4">
          <Link href="/admin/blog" className="p-2 bg-white/5 hover:bg-white/10 rounded-xl transition-colors border border-white/10 text-gray-400 hover:text-white">
            <ArrowLeft className="w-5 h-5" />
          </Link>
          <div>
            <h1 className="text-3xl font-black tracking-tight text-white flex items-center gap-3">
              <FileText className="w-8 h-8 text-emerald-400" />
              {isNew ? "Nouvel Article" : "Éditer l'article"}
            </h1>
          </div>
        </div>

        <button
          onClick={handleSave}
          disabled={isSaving || isLoading}
          className="flex items-center justify-center gap-2 px-5 py-2.5 bg-emerald-500 hover:bg-emerald-400 disabled:bg-emerald-800 disabled:text-gray-400 text-black font-bold rounded-xl transition-all shadow-[0_0_20px_rgba(16,185,129,0.2)] shrink-0"
        >
          <Save className="w-4 h-4" />
          {isSaving ? "Enregistrement..." : "Enregistrer"}
        </button>
      </div>

      {isLoading ? (
        <div className="flex flex-col items-center justify-center py-20 gap-4">
          <div className="w-10 h-10 border-4 border-emerald-500/20 border-t-emerald-500 rounded-full animate-spin" />
        </div>
      ) : (
        <div className="grid grid-cols-1 lg:grid-cols-3 gap-8">
          {/* Main content area */}
          <div className="lg:col-span-2 space-y-6">
            <div className="bg-[#0f0f11]/60 border border-white/5 p-6 rounded-3xl space-y-6">
              <div>
                <label className="block text-xs font-bold text-gray-400 uppercase mb-2">Titre de l'article</label>
                <input
                  type="text"
                  value={title}
                  onChange={handleTitleChange}
                  className="w-full bg-black/40 border border-white/10 rounded-xl px-4 py-3 text-lg font-bold text-white focus:outline-none focus:border-emerald-500 transition-colors"
                  placeholder="Mon super article SEO..."
                />
              </div>

              <div className="flex items-center justify-between mb-2">
                <label className="block text-xs font-bold text-gray-400 uppercase">Contenu (HTML / WYSIWYG)</label>
                <button 
                  onClick={() => setShowHtml(!showHtml)}
                  className="text-xs font-bold text-emerald-400 hover:text-emerald-300 transition-colors"
                >
                  {showHtml ? "Passer en Mode Visuel" : "Voir le code HTML"}
                </button>
              </div>
              <div className="bg-white text-black rounded-xl overflow-hidden min-h-[400px]">
                {showHtml ? (
                  <textarea 
                    value={content}
                    onChange={(e) => setContent(e.target.value)}
                    className="w-full h-[400px] p-4 text-sm font-mono bg-[#0f0f11] text-gray-300 focus:outline-none border-none resize-y"
                  />
                ) : (
                  <ReactQuill 
                    ref={quillRef}
                    theme="snow" 
                    value={content} 
                    onChange={setContent} 
                    modules={modules}
                    className="h-[350px] border-none"
                  />
                )}
              </div>
            </div>
          </div>

          {/* Sidebar */}
          <div className="space-y-6">
            <div className="bg-[#0f0f11]/60 border border-white/5 p-6 rounded-3xl space-y-6">
              <h3 className="text-sm font-bold text-gray-200 border-b border-white/5 pb-3">Paramètres</h3>

              <div>
                <label className="block text-xs font-bold text-gray-400 uppercase mb-2">URL Slug</label>
                <input
                  type="text"
                  value={slug}
                  onChange={(e) => setSlug(e.target.value)}
                  className="w-full bg-black/40 border border-white/10 rounded-xl px-4 py-2.5 text-xs text-white focus:outline-none focus:border-emerald-500 transition-colors"
                />
                <p className="text-[10px] text-gray-500 mt-1">L'URL sera: /blog/{slug || "..."}</p>
              </div>

              <div>
                <label className="block text-xs font-bold text-gray-400 uppercase mb-2">Extrait (SEO / Carte)</label>
                <textarea
                  value={excerpt}
                  onChange={(e) => setExcerpt(e.target.value)}
                  rows={3}
                  className="w-full bg-black/40 border border-white/10 rounded-xl p-3 text-xs text-white focus:outline-none focus:border-emerald-500 transition-colors"
                  placeholder="Un court résumé accrocheur..."
                />
              </div>

              <div>
                <label className="block text-xs font-bold text-gray-400 uppercase mb-2">Langue</label>
                <select
                  value={lang}
                  onChange={(e) => setLang(e.target.value)}
                  className="w-full bg-black/40 border border-white/10 rounded-xl px-4 py-2.5 text-xs text-white focus:outline-none focus:border-emerald-500 transition-colors"
                >
                  <option value="fr">🇫🇷 Français (FR)</option>
                  <option value="en">🇬🇧 English (EN)</option>
                </select>
              </div>

              <div>
                <label className="block text-xs font-bold text-gray-400 uppercase mb-2">Catégorie</label>
                <input
                  type="text"
                  list="category-list"
                  value={category}
                  onChange={(e) => setCategory(e.target.value)}
                  className="w-full bg-black/40 border border-white/10 rounded-xl px-4 py-2.5 text-xs text-white focus:outline-none focus:border-emerald-500 transition-colors"
                  placeholder="Ex: Tutoriel, Mise à jour..."
                />
                <datalist id="category-list">
                  {categories.map((cat, idx) => (
                    <option key={idx} value={cat} />
                  ))}
                </datalist>
              </div>

              <div>
                <label className="block text-xs font-bold text-gray-400 uppercase mb-2 flex items-center gap-1.5">
                  <User className="w-3.5 h-3.5" /> Auteur
                </label>
                <input
                  type="text"
                  value={author}
                  onChange={(e) => setAuthor(e.target.value)}
                  className="w-full bg-black/40 border border-white/10 rounded-xl px-4 py-2.5 text-xs text-white focus:outline-none focus:border-emerald-500 transition-colors"
                  placeholder="Nom de l'auteur"
                />
              </div>

              <div>
                <label className="block text-xs font-bold text-gray-400 uppercase mb-2">Image de couverture (URL)</label>
                <div className="flex gap-2">
                  <input
                    type="text"
                    value={coverImage}
                    onChange={(e) => setCoverImage(e.target.value)}
                    className="flex-1 bg-black/40 border border-white/10 rounded-xl px-4 py-2.5 text-xs text-white focus:outline-none focus:border-emerald-500 transition-colors"
                    placeholder="https://..."
                  />
                  <div className="relative">
                    <input 
                      type="file" 
                      accept="image/*" 
                      onChange={handleCoverUpload}
                      className="absolute inset-0 opacity-0 cursor-pointer w-full h-full"
                    />
                    <div className="px-4 py-2.5 bg-white/5 hover:bg-white/10 border border-white/10 text-xs text-gray-300 font-bold rounded-xl transition-colors cursor-pointer flex items-center justify-center h-full">
                      Upload
                    </div>
                  </div>
                </div>
              </div>

              <div className="pt-4 border-t border-white/5">
                <label className="flex items-center justify-between cursor-pointer group">
                  <div className="flex items-center gap-2">
                    <div className={`p-1.5 rounded-lg border ${published ? 'bg-emerald-500/10 border-emerald-500/20 text-emerald-400' : 'bg-white/5 border-white/10 text-gray-400'}`}>
                      {published ? <Globe className="w-4 h-4" /> : <EyeOff className="w-4 h-4" />}
                    </div>
                    <div>
                      <div className="text-sm font-bold text-white group-hover:text-emerald-400 transition-colors">Publier l'article</div>
                      <div className="text-[10px] text-gray-500">Rendre visible au public</div>
                    </div>
                  </div>
                  <div className="relative">
                    <input type="checkbox" className="sr-only" checked={published} onChange={(e) => setPublished(e.target.checked)} />
                    <div className={`block w-10 h-6 rounded-full transition-colors ${published ? 'bg-emerald-500' : 'bg-gray-600'}`}></div>
                    <div className={`dot absolute left-1 top-1 bg-white w-4 h-4 rounded-full transition-transform ${published ? 'transform translate-x-4' : ''}`}></div>
                  </div>
                </label>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
