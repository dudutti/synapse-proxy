import { NextResponse } from "next/server";
import { prisma } from "@/lib/prisma";
import { getServerSession } from "next-auth";
import { authOptions } from "@/lib/authOptions";

export const dynamic = "force-dynamic";

export async function GET() {
  const session = await getServerSession(authOptions);
  if (!session || (session.user as any).role !== "SUPERADMIN") {
    return new NextResponse("Unauthorized", { status: 401 });
  }

  try {
    const templates = await prisma.emailTemplate.findMany({
      orderBy: { id: 'asc' }
    });
    return NextResponse.json(templates);
  } catch (error) {
    console.error(error);
    return new NextResponse("Internal Error", { status: 500 });
  }
}

export async function PUT(req: Request) {
  const session = await getServerSession(authOptions);
  if (!session || (session.user as any).role !== "SUPERADMIN") {
    return new NextResponse("Unauthorized", { status: 401 });
  }

  try {
    const { id, subject, html } = await req.json();

    if (!id || !subject || !html) {
      return new NextResponse("Missing fields", { status: 400 });
    }

    const template = await prisma.emailTemplate.upsert({
      where: { id },
      update: { subject, html },
      create: { id, subject, html }
    });

    return NextResponse.json(template);
  } catch (error) {
    console.error(error);
    return new NextResponse("Internal Error", { status: 500 });
  }
}
