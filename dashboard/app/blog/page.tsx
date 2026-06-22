import { prisma } from "@/lib/prisma";
import Link from "next/link";
import { ArrowRight, Calendar, User } from "lucide-react";
import { cookies } from "next/headers";
import type { Metadata } from "next";
import HeaderNav from "@/components/HeaderNav";
import Footer from "@/components/Footer";
import BlogFilters from "@/components/blog/BlogFilters";
import BlogPagination from "@/components/blog/BlogPagination";
import ParticleBackground from "@/components/ParticleBackground";

export const metadata: Metadata = {
  title: "Blog & Actualités | Synapse Proxy",
  description: "Dernières actualités, tutoriels et guides autour des agents IA, du firewalling et de l'optimisation des coûts.",
};

export const dynamic = "force-dynamic";

export default async function BlogIndexPage({ searchParams }: { searchParams: { page?: string, category?: string } }) {
  const cookieStore = cookies();
  const lang = cookieStore.get("lang")?.value || "fr";

  const page = Number(searchParams.page) || 1;
  const category = searchParams.category;
  const POSTS_PER_PAGE = 9;

  const where: any = { published: true, lang };
  if (category) {
    where.category = category;
  }

  const totalPosts = await prisma.blogPost.count({ where });
  const totalPages = Math.ceil(totalPosts / POSTS_PER_PAGE);

  const posts = await prisma.blogPost.findMany({
    where,
    orderBy: { publishedAt: "desc" },
    skip: (page - 1) * POSTS_PER_PAGE,
    take: POSTS_PER_PAGE,
  });

  const distinctCategories = await prisma.blogPost.findMany({
    where: { published: true, lang, category: { not: null, not: "" } },
    select: { category: true },
    distinct: ["category"],
  });
  const categories = distinctCategories.map(c => c.category as string);

  const t = {
    fr: {
      tag: "Ressources",
      title: "Le Blog ",
      subtitle: "Guides, actualités et bonnes pratiques pour sécuriser et optimiser vos agents IA.",
      empty: "Aucun article publié pour le moment.",
      readMore: "Lire l'article",
      dateLocale: "fr-FR"
    },
    en: {
      tag: "Resources",
      title: "The ",
      titleAppend: " Blog",
      subtitle: "Guides, news, and best practices to secure and optimize your AI agents.",
      empty: "No articles published yet.",
      readMore: "Read article",
      dateLocale: "en-US"
    }
  }[lang] || {
    tag: "Ressources",
    title: "Le Blog ",
    subtitle: "Guides, actualités et bonnes pratiques pour sécuriser et optimiser vos agents IA.",
    empty: "Aucun article publié pour le moment.",
    readMore: "Lire l'article",
    dateLocale: "fr-FR"
  };

  return (
    <div className="min-h-screen bg-[#050505] text-white selection:bg-emerald-500/30 font-sans pt-24 pb-20 flex flex-col relative overflow-hidden">
      <ParticleBackground />
      
      {/* MASSIVE WATERMARK LOGO */}
      <div className="absolute inset-0 pointer-events-none opacity-[0.08] z-0 flex items-center justify-center overflow-hidden">
        <img src="/logo01.png" alt="Watermark" className="w-[150%] h-[150%] object-cover drop-shadow-[0_0_100px_rgba(52,211,153,0.8)] opacity-50" />
      </div>

      <div className="fixed top-0 left-0 right-0 z-50 p-4 pointer-events-none">
        <div className="max-w-7xl mx-auto pointer-events-auto">
          <header className="flex justify-between items-center bg-[#050505]/80 border border-white/10 p-4 rounded-2xl backdrop-blur-xl shadow-2xl relative z-50">
            <Link href="/" className="flex items-center gap-3 group">
              <div className="w-10 h-10 rounded-xl bg-[#0f0f11] border border-white/10 shadow-[0_0_20px_rgba(52,211,153,0.2)] group-hover:shadow-[0_0_30px_rgba(52,211,153,0.4)] ring-1 ring-emerald-500/30 overflow-hidden flex items-center justify-center transition-all">
                <img src="/logo01.png" alt="Synapse Proxy Icon" className="w-[150%] h-[150%] object-cover max-w-none translate-y-1.5" />
              </div>
              <div>
                <h1 className="text-xl font-bold tracking-tight text-white group-hover:text-emerald-400 transition-colors">Synapse Proxy</h1>
                <p className="text-gray-500 text-xs hidden sm:block">Intelligent LLM Gateway</p>
              </div>
            </Link>
            <HeaderNav />
          </header>
        </div>
      </div>

      <div className="flex-1 max-w-6xl mx-auto px-4 sm:px-6 lg:px-8 pt-16">
        
        {/* Header */}
        <div className="text-center max-w-3xl mx-auto mb-16 space-y-6">
          <div className="inline-flex items-center gap-2 px-3 py-1.5 rounded-full border border-emerald-500/20 bg-emerald-500/10 text-emerald-400 text-xs font-bold uppercase tracking-widest mx-auto">
            <span>{t.tag}</span>
          </div>
          <h1 className="text-5xl md:text-6xl font-black tracking-tight leading-[1.1] text-transparent bg-clip-text bg-gradient-to-r from-white to-gray-500">
            {t.title}<span className="text-emerald-400">Synapse Proxy</span>{'titleAppend' in t ? t.titleAppend : ''}
          </h1>
          <p className="text-lg text-gray-400">
            {t.subtitle}
          </p>
        </div>

        <div className="relative z-10">
          {categories.length > 0 && (
            <BlogFilters categories={categories} lang={lang} />
          )}
        </div>

        {/* Posts Grid */}
        {posts.length === 0 ? (
          <div className="text-center py-20 border border-white/5 rounded-3xl bg-[#0f0f11]/60">
            <p className="text-gray-400 text-lg">{t.empty}</p>
          </div>
        ) : (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-8">
            {posts.map((post) => (
              <Link key={post.id} href={`/blog/${post.slug}`} className="group relative flex flex-col h-full bg-[#050505] border border-white/10 hover:border-emerald-500/50 rounded-3xl overflow-hidden transition-all hover:shadow-[0_0_30px_rgba(16,185,129,0.1)]">
                
                {/* Cover Image */}
                {post.coverImage && (
                  <div className="w-full h-48 bg-[#0a0a0c] border-b border-white/5 relative overflow-hidden shrink-0">
                    <img 
                      src={post.coverImage} 
                      alt={post.title} 
                      className="w-full h-full object-cover transition-transform duration-700 group-hover:scale-105"
                    />
                  </div>
                )}
                
                <div className="p-6 flex flex-col justify-between flex-1">
                  <div className="space-y-4">
                    <div className="flex flex-wrap items-center gap-4 text-xs font-bold text-gray-500">
                      {post.category && (
                        <span className="inline-block px-2 py-1 bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 rounded-md">
                          {post.category}
                        </span>
                      )}
                      <span className="flex items-center gap-1.5">
                        <Calendar className="w-3.5 h-3.5 text-emerald-400" />
                      {post.publishedAt ? new Date(post.publishedAt).toLocaleDateString(t.dateLocale, { year: 'numeric', month: 'long', day: 'numeric' }) : ""}
                    </span>
                    {post.author && (
                      <span className="flex items-center gap-1.5">
                        <User className="w-3.5 h-3.5 text-emerald-400" />
                        {post.author}
                      </span>
                    )}
                  </div>
                  
                  <h2 className="text-2xl font-black leading-tight text-white group-hover:text-emerald-400 transition-colors">
                    {post.title}
                  </h2>
                  
                  {post.excerpt && (
                    <p className="text-sm text-gray-400 line-clamp-3 leading-relaxed">
                      {post.excerpt}
                    </p>
                  )}
                </div>

                  <div className="mt-8 pt-6 border-t border-white/5 flex items-center text-emerald-400 text-sm font-bold group-hover:gap-3 gap-2 transition-all">
                    {t.readMore} <ArrowRight className="w-4 h-4" />
                  </div>
                </div>
              </Link>
            ))}
          </div>
        )}

        <div className="relative z-10">
          <BlogPagination currentPage={page} totalPages={totalPages} />
        </div>
      </div>
      
      <div className="mt-auto pt-20 relative z-10">
        <Footer />
      </div>
    </div>
  );
}
