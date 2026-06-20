import { NextResponse } from "next/server";
import { prisma } from "@/lib/prisma";

export async function GET() {
  try {
    const plans = await prisma.stripePlan.findMany({
      orderBy: { amount: "asc" }
    });
    return NextResponse.json(plans);
  } catch (error) {
    console.error("[PLANS_GET]", error);
    return new NextResponse("Internal Error", { status: 500 });
  }
}
