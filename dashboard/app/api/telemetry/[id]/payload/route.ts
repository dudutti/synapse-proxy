import { NextResponse } from "next/server";
import { getServerSession } from "next-auth";
import { prisma } from "@/lib/prisma";

const ALLOWED_FIELDS = new Set([
  "originalPayload",
  "optimizedPayload",
  "responsePayload",
]);

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

    // Wrap the raw payload in a JSON envelope so the client can call
    // r.json() safely. The previous version returned the raw text
    // with Content-Type: application/json, which crashed the client
    // whenever the payload contained unescaped characters (common
    // with LLM responses that include control bytes, raw newlines in
    // JSON values, etc.).
    //
    // IMPORTANT: do NOT set Content-Length manually. JavaScript's
    // String.prototype.length counts UTF-16 code units, not bytes, so
    // for payloads with non-ASCII characters (french, emojis, etc.)
    // the declared length is smaller than the actual UTF-8 byte
    // length. The browser / proxy reads Content-Length and truncates
    // the body to that smaller size, leaving a half-formed JSON
    // string at the end. r.json() then throws "Unterminated string
    // in JSON at position N". Next.js / the runtime computes the
    // correct byte length automatically when we omit the header.
    const body = JSON.stringify({ payload: text });
    return new NextResponse(body, {
      status: 200,
      headers: {
        "Content-Type": "application/json; charset=utf-8",
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
