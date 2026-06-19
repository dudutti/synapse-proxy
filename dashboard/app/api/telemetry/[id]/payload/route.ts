import { NextResponse } from "next/server";
import { getServerSession } from "next-auth";
import { prisma } from "@/lib/prisma";

const ALLOWED_FIELDS = new Set(["originalPayload", "optimizedPayload"]);

export async function GET(
  req: Request,
  { params }: { params: { id: string } }
) {
  try {
    const session = await getServerSession();
    if (!session?.user?.email) {
      return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
    }

    const user = await prisma.user.findUnique({
      where: { email: session.user.email },
    });
    if (!user) {
      return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
    }

    const field = new URL(req.url).searchParams.get("field") || "originalPayload";
    if (!ALLOWED_FIELDS.has(field)) {
      return NextResponse.json({ error: "Invalid field" }, { status: 400 });
    }

    const log = await prisma.requestLog.findUnique({
      where: { id: params.id },
      select: { apiKeyId: true, [field]: true },
    });

    if (!log) {
      return NextResponse.json({ error: "Not found" }, { status: 404 });
    }

    const ownsKey = await prisma.apiKey.findFirst({
      where: { id: log.apiKeyId, userId: user.id },
      select: { id: true },
    });
    if (!ownsKey && user.role !== "SUPERADMIN") {
      return NextResponse.json({ error: "Forbidden" }, { status: 403 });
    }

    const text = (log as any)[field] as string | null;
    if (!text) {
      return new NextResponse("(empty)", {
        status: 200,
        headers: { "Content-Type": "text/plain; charset=utf-8" },
      });
    }

    return new NextResponse(text, {
      status: 200,
      headers: {
        "Content-Type": "application/json; charset=utf-8",
        "Content-Disposition": `attachment; filename="${params.id}-${field}.json"`,
        "Content-Length": String(text.length),
      },
    });
  } catch (e: any) {
    console.error("[telemetry/[id]/payload] error:", e);
    return NextResponse.json(
      { error: "Internal server error", detail: String(e?.message || e) },
      { status: 500 }
    );
  }
}
