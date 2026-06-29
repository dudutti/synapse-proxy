import { NextRequest, NextResponse } from "next/server";
import { prisma } from "@/lib/prisma";

export const dynamic = "force-dynamic";

export async function POST(req: NextRequest) {
  try {
    const { licenseKey } = await req.json();
    if (!licenseKey || typeof licenseKey !== "string" || licenseKey.length < 10) {
      return NextResponse.json({ valid: false, error: "Invalid license format" }, { status: 400 });
    }

    // Lookup user by their unique, cryptographically random licenseKey
    const user = await prisma.user.findUnique({
      where: { licenseKey },
    });

    if (!user) {
      return NextResponse.json({ valid: false, error: "License key not found" }, { status: 404 });
    }

    let quotaLimit = 10000000; // 10M for FREE
    if (user.tier === "PRO" || user.tier === "TIER1") {
      quotaLimit = 50000000; // 50M
    } else if (user.tier === "ENTERPRISE" || user.tier === "TIER2") {
      quotaLimit = -1; // Unlimited
    }

    return NextResponse.json({
      valid: true,
      tier: user.tier,
      quotaLimit,
      quotaUsed: user.currentMonthTokens,
    });
  } catch (error: any) {
    return NextResponse.json({ valid: false, error: error.message }, { status: 500 });
  }
}
