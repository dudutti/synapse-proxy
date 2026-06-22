import { NextResponse } from "next/server";
import { getServerSession } from "next-auth";
import { revalidatePath } from "next/cache";
import { authOptions } from "@/lib/authOptions";
import { prisma } from "@/lib/prisma";

export async function PUT(request: Request, { params }: { params: { id: string } }) {
  const session = await getServerSession(authOptions);
  if (!session || (session.user as any).role !== "SUPERADMIN") {
    return new NextResponse("Unauthorized", { status: 401 });
  }

  try {
    const body = await request.json();
    const { title, slug, content, excerpt, author, published, coverImage, category, lang } = body;

    const postToUpdate = await prisma.blogPost.findUnique({ where: { id: params.id } });
    if (!postToUpdate) {
      return new NextResponse("Post not found", { status: 404 });
    }

    const data: any = { title, slug, content, excerpt, author, published: !!published, coverImage, category, lang: lang || "fr" };
    
    // Set publishedAt only if transitioning from unpublished to published
    if (!postToUpdate.published && published) {
        data.publishedAt = new Date();
    } else if (!published) {
        data.publishedAt = null;
    }

    const post = await prisma.blogPost.update({
      where: { id: params.id },
      data,
    });

    // Revalidate public pages and sitemap
    revalidatePath("/blog");
    revalidatePath("/sitemap.xml");
    revalidatePath("/");
    revalidatePath(`/blog/${post.slug}`);

    return NextResponse.json(post);
  } catch (error) {
    console.error("[ADMIN_BLOG_PUT]", error);
    return new NextResponse("Internal Error", { status: 500 });
  }
}

export async function DELETE(request: Request, { params }: { params: { id: string } }) {
  const session = await getServerSession(authOptions);
  if (!session || (session.user as any).role !== "SUPERADMIN") {
    return new NextResponse("Unauthorized", { status: 401 });
  }

  try {
    const post = await prisma.blogPost.findUnique({ where: { id: params.id } });
    
    await prisma.blogPost.delete({
      where: { id: params.id },
    });
    
    revalidatePath("/blog");
    revalidatePath("/sitemap.xml");
    revalidatePath("/");
    if (post) {
      revalidatePath(`/blog/${post.slug}`);
    }

    return NextResponse.json({ success: true });
  } catch (error) {
    console.error("[ADMIN_BLOG_DELETE]", error);
    return new NextResponse("Internal Error", { status: 500 });
  }
}
