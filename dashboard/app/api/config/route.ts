import { NextResponse } from "next/server";
import { prisma } from "@/lib/prisma";

export const dynamic = "force-dynamic";

export async function GET() {
  try {
    let config = await prisma.systemConfig.findUnique({
      where: { id: "global" }
    });

    if (!config) {
      config = await prisma.systemConfig.create({
        data: { id: "global", registrationOpen: false }
      });
    }

    return NextResponse.json(config);
  } catch (error) {
    return NextResponse.json({ registrationOpen: false }, { status: 500 });
  }
}
