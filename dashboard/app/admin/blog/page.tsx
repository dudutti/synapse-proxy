"use client";

import { useState, useEffect } from "react";
import { Plus, Trash2, Edit, FileText, Globe } from "lucide-react";
import { toast } from "sonner";
import Link from "next/link";
import { useRouter } from "next/navigation";

export default function AdminBlogPage() {
  const [posts, setPosts] = useState<any[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const router = useRouter();

  const fetchPosts = async () => {
    setIsLoading(true);
    try {
      const res = await fetch("/api/admin/blog");
      if (!res.ok) throw new Error("Failed to fetch posts");
      const data = await res.json();
      setPosts(data);
    } catch (err) {
      toast.error("Erreur lors du chargement des articles");
    } finally {
      setIsLoading(false);
    }
  };

  useEffect(() => {
    fetchPosts();
  }, []);

  const handleDelete = async (id: string) => {
    if (!confirm("Voulez-vous vraiment supprimer cet article ?")) return;
    try {
      const res = await fetch(`/api/admin/blog/${id}`, { method: "DELETE" });
      if (!res.ok) throw new Error("Failed to delete");
      toast.success("Article supprimé");
      fetchPosts();
    } catch (err) {
      toast.error("Erreur lors de la suppression");
    }
  };

  return (
    <div className="p-8 max-w-5xl mx-auto space-y-8">
      {/* Header */}
      <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 border-b border-white/10 pb-6">
        <div>
          <h1 className="text-3xl font-black tracking-tight text-white flex items-center gap-3">
            <FileText className="w-8 h-8 text-emerald-400" />
            Gestion du Blog
          </h1>
          <p className="text-gray-400 text-sm mt-1">
            Gérez vos articles de blog, publiés ou en brouillon.
          </p>
        </div>

        <Link
          href="/admin/blog/new"
          className="flex items-center justify-center gap-2 px-5 py-2.5 bg-emerald-500 hover:bg-emerald-400 text-black font-bold rounded-xl transition-all shadow-[0_0_20px_rgba(16,185,129,0.2)] shrink-0"
        >
          <Plus className="w-4 h-4" />
          Nouvel Article
        </Link>
      </div>

      {isLoading ? (
        <div className="flex flex-col items-center justify-center py-20 gap-4">
          <div className="w-10 h-10 border-4 border-emerald-500/20 border-t-emerald-500 rounded-full animate-spin" />
        </div>
      ) : posts.length === 0 ? (
        <div className="bg-[#0f0f11]/60 border border-white/5 p-12 rounded-3xl text-center space-y-4">
          <p className="text-gray-400">Aucun article pour le moment.</p>
        </div>
      ) : (
        <div className="bg-[#0f0f11]/60 border border-white/5 rounded-3xl overflow-hidden">
          <table className="w-full text-left text-sm text-gray-300">
            <thead className="text-xs uppercase bg-black/40 text-gray-500 font-bold">
              <tr>
                <th className="px-6 py-4">Titre</th>
                <th className="px-6 py-4">Langue</th>
                <th className="px-6 py-4">Auteur</th>
                <th className="px-6 py-4">Statut</th>
                <th className="px-6 py-4">Date</th>
                <th className="px-6 py-4 text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-white/5">
              {posts.map((post) => (
                <tr key={post.id} className="hover:bg-white/[0.02] transition-colors">
                  <td className="px-6 py-4 font-bold text-white">{post.title}</td>
                  <td className="px-6 py-4 uppercase text-xs font-bold text-gray-500">{post.lang || "fr"}</td>
                  <td className="px-6 py-4">{post.author || "Anonyme"}</td>
                  <td className="px-6 py-4">
                    {post.published ? (
                      <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-[10px] font-bold bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">
                        <Globe className="w-3 h-3" /> Publié
                      </span>
                    ) : (
                      <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-[10px] font-bold bg-yellow-500/10 text-yellow-400 border border-yellow-500/20">
                        Brouillon
                      </span>
                    )}
                  </td>
                  <td className="px-6 py-4">
                    {new Date(post.createdAt).toLocaleDateString("fr-FR")}
                  </td>
                  <td className="px-6 py-4 text-right space-x-3">
                    <button
                      onClick={() => router.push(`/admin/blog/${post.id}`)}
                      className="text-gray-400 hover:text-white transition-colors"
                      title="Éditer"
                    >
                      <Edit className="w-4 h-4 inline" />
                    </button>
                    <button
                      onClick={() => handleDelete(post.id)}
                      className="text-gray-500 hover:text-red-400 transition-colors"
                      title="Supprimer"
                    >
                      <Trash2 className="w-4 h-4 inline" />
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
