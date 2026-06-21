import { NextResponse } from "next/server";
import { prisma } from "@/lib/prisma";
import { getServerSession } from "next-auth";
import { authOptions } from "@/lib/authOptions";

async function isAdmin() {
  const session = await getServerSession(authOptions);
  return session && (session.user as any).role === "SUPERADMIN";
}

export async function GET() {
  if (!(await isAdmin())) return new NextResponse("Unauthorized", { status: 401 });
  
  const models = await prisma.providerModel.findMany({
    where: { userId: "global" },
    orderBy: [{ provider: 'asc' }, { modelName: 'asc' }]
  });
  return NextResponse.json(models);
}

export async function POST(req: Request) {
  if (!(await isAdmin())) return new NextResponse("Unauthorized", { status: 401 });

  try {
    const { provider, modelName, costPromptPer1M, costCompletionPer1M } = await req.json();
    
    const model = await prisma.providerModel.upsert({
      where: {
        provider_modelName_userId: { provider, modelName, userId: "global" }
      },
      update: { costPromptPer1M, costCompletionPer1M },
      create: { userId: "global", provider, modelName, costPromptPer1M, costCompletionPer1M }
    });

    return NextResponse.json(model);
  } catch (error) {
    return new NextResponse("Error creating model", { status: 500 });
  }
}

export async function DELETE(req: Request) {
  if (!(await isAdmin())) return new NextResponse("Unauthorized", { status: 401 });

  try {
    const url = new URL(req.url);
    const id = url.searchParams.get("id");
    
    if (!id) return new NextResponse("Missing id", { status: 400 });

    await prisma.providerModel.delete({ where: { id } });
    return new NextResponse("Deleted", { status: 200 });
  } catch (error) {
    return new NextResponse("Error deleting model", { status: 500 });
  }
}
