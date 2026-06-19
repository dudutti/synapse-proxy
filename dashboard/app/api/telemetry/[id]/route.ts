import { NextResponse } from "next/server";
import { getServerSession } from "next-auth";
import { prisma } from "@/lib/prisma";

const MAX_PAYLOAD_BYTES = 500_000; // 500KB cap per payload field

function truncatePayload(raw: string | null | undefined): {
  text: string | null;
  truncated: boolean;
} {
  if (raw == null) return { text: null, truncated: false };
  if (raw.length <= MAX_PAYLOAD_BYTES) return { text: raw, truncated: false };
  return {
    text: raw.slice(0, MAX_PAYLOAD_BYTES) + "\n[…payload truncated to 500KB…]",
    truncated: true,
  };
}

export async function GET(
  _req: Request,
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

    const log = await prisma.requestLog.findUnique({
      where: { id: params.id },
      select: {
        id: true,
        apiKeyId: true,
        cacheLevel: true,
        model: true,
        provider: true,
        agentId: true,
        agentLabel: true,
        sessionId: true,
        createdAt: true,
        promptTokensOrig: true,
        completionTokensOrig: true,
        promptTokensOpt: true,
        completionTokensOpt: true,
        costSaved: true,
        durationMs: true,
        originalPayload: true,
        optimizedPayload: true,
        // responsePayload is intentionally NOT selected: the column
        // was added in a later migration that may not have been
        // applied to older deployments, and Prisma would otherwise
        // generate a SQL query that fails on missing column.
      },
    });

    if (!log) {
      return NextResponse.json({ error: "Not found" }, { status: 404 });
    }

    // Verify the log belongs to a key owned by this user
    const ownsKey = await prisma.apiKey.findFirst({
      where: { id: log.apiKeyId, userId: user.id },
      select: { id: true },
    });
    if (!ownsKey && user.role !== "SUPERADMIN") {
      return NextResponse.json({ error: "Forbidden" }, { status: 403 });
    }

    // Cap each payload field to 100KB so the JSON response stays
    // bounded and the client diff stays responsive. The dashboard
    // also has a hard time rendering multi-MB strings. The full
    // payload remains downloadable via /api/telemetry/[id]/payload.
    const orig = truncatePayload(log.originalPayload);
    const opt = truncatePayload(log.optimizedPayload);

    return NextResponse.json({
      id: log.id,
      apiKeyId: log.apiKeyId,
      cacheLevel: log.cacheLevel,
      model: log.model,
      provider: log.provider,
      agentId: log.agentId,
      agentLabel: log.agentLabel,
      sessionId: log.sessionId,
      createdAt: log.createdAt,
      promptTokensOrig: log.promptTokensOrig,
      completionTokensOrig: log.completionTokensOrig,
      promptTokensOpt: log.promptTokensOpt,
      completionTokensOpt: log.completionTokensOpt,
      costSaved: log.costSaved,
      durationMs: log.durationMs,
      originalPayload: orig.text,
      optimizedPayload: opt.text,
      payloadsTruncated: {
        original: orig.truncated,
        optimized: opt.truncated,
        originalFullLength: log.originalPayload?.length || 0,
        optimizedFullLength: log.optimizedPayload?.length || 0,
      },
    });
  } catch (e: any) {
    console.error("[telemetry/[id]] error:", e);
    return NextResponse.json(
      { error: "Internal server error", detail: String(e?.message || e) },
      { status: 500 }
    );
  }
}
