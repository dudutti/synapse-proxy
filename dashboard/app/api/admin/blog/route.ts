import { NextResponse } from "next/server";
import { getServerSession } from "next-auth";
import { revalidatePath } from "next/cache";
import { authOptions } from "@/lib/authOptions";
import { prisma } from "@/lib/prisma";

export async function GET(request: Request) {
  const session = await getServerSession(authOptions);
  if (!session || (session.user as any).role !== "SUPERADMIN") {
    return new NextResponse("Unauthorized", { status: 401 });
  }

  try {
    const posts = await prisma.blogPost.findMany({
      orderBy: { createdAt: "desc" },
    });
    return NextResponse.json(posts);
  } catch (error) {
    console.error("[ADMIN_BLOG_GET]", error);
    return new NextResponse("Internal Error", { status: 500 });
  }
}

export async function POST(request: Request) {
  const session = await getServerSession(authOptions);
  if (!session || (session.user as any).role !== "SUPERADMIN") {
    return new NextResponse("Unauthorized", { status: 401 });
  }

  try {
    const body = await request.json();
    const { title, slug, content, excerpt, author, published, coverImage, category, lang } = body;

    if (!title || !slug || !content) {
      return new NextResponse("Title, slug and content are required", { status: 400 });
    }

    const existing = await prisma.blogPost.findUnique({ where: { slug } });
    if (existing) {
      return new NextResponse("Slug already exists", { status: 400 });
    }

    const post = await prisma.blogPost.create({
      data: {
        title,
        slug,
        content,
        excerpt,
        coverImage,
        category,
        lang: lang || "fr",
        author,
        published: !!published,
        publishedAt: published ? new Date() : null,
      },
    });

    // Revalidate public pages and sitemap
    revalidatePath("/blog");
    revalidatePath("/sitemap.xml");
    revalidatePath("/");

    return NextResponse.json(post);
  } catch (error) {
    console.error("[ADMIN_BLOG_POST]", error);
    return new NextResponse("Internal Error", { status: 500 });
  }
}
