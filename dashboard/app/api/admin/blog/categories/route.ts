import { NextResponse } from "next/server";
import { getServerSession } from "next-auth";
import { authOptions } from "@/lib/authOptions";
import { prisma } from "@/lib/prisma";

export async function GET() {
  const session = await getServerSession(authOptions);
  if (!session || (session.user as any).role !== "SUPERADMIN") {
    return new NextResponse("Unauthorized", { status: 401 });
  }

  try {
    const categories = await prisma.blogPost.findMany({
      where: { category: { not: null } },
      select: { category: true },
      distinct: ['category'],
    });
    
    // Extract strings and filter out empty ones
    const list = categories.map(c => c.category).filter(Boolean);
    
    return NextResponse.json(list);
  } catch (error) {
    console.error("[ADMIN_BLOG_CATEGORIES]", error);
    return new NextResponse("Internal Error", { status: 500 });
  }
}
