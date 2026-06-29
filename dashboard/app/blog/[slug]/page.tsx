import { prisma } from "@/lib/prisma";
import { notFound } from "next/navigation";
import Link from "next/link";
import { ArrowLeft, Calendar, User } from "lucide-react";
import { cookies } from "next/headers";
import type { Metadata } from "next";
import PublicHeader from "@/components/PublicHeader";
import Footer from "@/components/Footer";
import ParticleBackground from "@/components/ParticleBackground";

export const dynamic = "force-dynamic";

export async function generateMetadata({ params }: { params: { slug: string } }): Promise<Metadata> {
  const post = await prisma.blogPost.findUnique({
    where: { slug: params.slug },
  });

  if (!post || !post.published) {
    return {
      title: "Article Introuvable | Synapse Proxy",
    };
  }

  return {
    title: `${post.title} | Blog Synapse Proxy`,
    description: post.excerpt || `Lisez l'article complet "${post.title}" sur le blog de Synapse Proxy.`,
  };
}



export default async function BlogPostPage({ params }: { params: { slug: string } }) {
  const post = await prisma.blogPost.findUnique({
    where: { slug: params.slug },
  });

  if (!post || !post.published) {
    notFound();
  }

  const cookieStore = cookies();
  const lang = cookieStore.get("lang")?.value || "fr";

  const t = {
    fr: { back: "Retour au blog", dateLocale: "fr-FR" },
    en: { back: "Back to blog", dateLocale: "en-US" }
  }[lang] || { back: "Retour au blog", dateLocale: "fr-FR" };

  return (
    <div className="min-h-screen bg-[#050505] text-white selection:bg-emerald-500/30 font-sans pt-24 pb-20 flex flex-col relative overflow-hidden">
      <ParticleBackground />
      
      {/* MASSIVE WATERMARK LOGO */}
      <div className="absolute inset-0 pointer-events-none opacity-[0.08] z-0 flex items-center justify-center overflow-hidden fixed">
        <img src="/logo01.png" alt="Watermark" className="w-[150%] h-[150%] object-cover drop-shadow-[0_0_100px_rgba(52,211,153,0.8)] opacity-50" />
      </div>

      <PublicHeader lang={lang as "fr" | "en"} />

      <div className="flex-1 max-w-4xl mx-auto px-4 sm:px-6 lg:px-8 pt-16 w-full relative z-10">
        
        {/* Back Link */}
        <Link href="/blog" className="inline-flex items-center gap-2 text-emerald-400 font-bold hover:text-emerald-300 transition-colors mb-10 text-sm">
          <ArrowLeft className="w-4 h-4" /> {t.back}
        </Link>

        {/* Header */}
        <header className="mb-14 space-y-8 text-center max-w-3xl mx-auto">
          {post.category && (
            <div className="inline-flex items-center gap-2 px-3 py-1.5 rounded-full border border-emerald-500/20 bg-emerald-500/10 text-emerald-400 text-xs font-bold uppercase tracking-widest">
              {post.category}
            </div>
          )}
          
          <h1 className="text-4xl md:text-5xl lg:text-6xl font-black tracking-tight leading-[1.1] text-transparent bg-clip-text bg-gradient-to-r from-white to-gray-400">
            {post.title}
          </h1>

          <div className="flex flex-wrap items-center justify-center gap-6 text-sm font-bold text-gray-400">
            <span className="flex items-center gap-2">
              <Calendar className="w-4 h-4 text-emerald-400" />
              {post.publishedAt ? new Date(post.publishedAt).toLocaleDateString(t.dateLocale, { year: 'numeric', month: 'long', day: 'numeric' }) : ""}
            </span>
            {post.author && (
              <span className="flex items-center gap-2">
                <User className="w-4 h-4 text-emerald-400" />
                {post.author}
              </span>
            )}
          </div>
        </header>

        {/* Cover Image */}
        {post.coverImage && (
          <div className="mb-14 w-full h-[400px] md:h-[500px] rounded-3xl overflow-hidden border border-white/10 shadow-[0_0_50px_rgba(16,185,129,0.1)]">
            <img 
              src={post.coverImage} 
              alt={post.title} 
              className="w-full h-full object-cover"
            />
          </div>
        )}

        {/* Content */}
        <article className="prose prose-invert prose-emerald prose-lg max-w-none 
          prose-headings:font-black prose-headings:tracking-tight
          prose-a:text-emerald-400 prose-a:no-underline hover:prose-a:underline
          prose-strong:text-white prose-strong:font-bold
          prose-code:text-emerald-300 prose-code:bg-emerald-500/10 prose-code:px-1.5 prose-code:py-0.5 prose-code:rounded-md prose-code:before:content-none prose-code:after:content-none
          prose-pre:bg-[#050505] prose-pre:border prose-pre:border-white/10
          prose-img:rounded-2xl prose-img:border prose-img:border-white/10"
        >
          <div dangerouslySetInnerHTML={{ __html: post.content }} />
        </article>

      </div>
      <div className="mt-auto">
        <Footer />
      </div>
    </div>
  );
}
