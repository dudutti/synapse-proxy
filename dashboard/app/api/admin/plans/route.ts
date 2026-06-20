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
    const plans = await prisma.stripePlan.findMany({
      orderBy: { amount: "asc" }
    });
    return NextResponse.json(plans);
  } catch (error) {
    console.error("[ADMIN_PLANS_GET]", error);
    return new NextResponse("Internal Error", { status: 500 });
  }
}

export async function POST(req: Request) {
  const session = await getServerSession(authOptions);
  if (!session || (session.user as any).role !== "SUPERADMIN") {
    return new NextResponse("Unauthorized", { status: 401 });
  }

  try {
    const body = await req.json();
    const { name, tier, priceId, amount, tokens } = body;

    if (!name || !tier || !priceId || amount === undefined || tokens === undefined) {
      return NextResponse.json({ error: "Missing fields" }, { status: 400 });
    }

    const plan = await prisma.stripePlan.upsert({
      where: { priceId },
      update: {
        name,
        tier,
        amount: parseFloat(amount),
        tokens: parseInt(tokens)
      },
      create: {
        name,
        tier,
        priceId,
        amount: parseFloat(amount),
        tokens: parseInt(tokens)
      }
    });

    return NextResponse.json(plan);
  } catch (error) {
    console.error("[ADMIN_PLANS_POST]", error);
    return new NextResponse("Internal Error", { status: 500 });
  }
}

export async function DELETE(req: Request) {
  const session = await getServerSession(authOptions);
  if (!session || (session.user as any).role !== "SUPERADMIN") {
    return new NextResponse("Unauthorized", { status: 401 });
  }

  try {
    const { searchParams } = new URL(req.url);
    const id = searchParams.get("id");
    if (!id) {
      return NextResponse.json({ error: "Missing id parameter" }, { status: 400 });
    }

    await prisma.stripePlan.delete({
      where: { id }
    });

    return NextResponse.json({ success: true });
  } catch (error) {
    console.error("[ADMIN_PLANS_DELETE]", error);
    return new NextResponse("Internal Error", { status: 500 });
  }
}
