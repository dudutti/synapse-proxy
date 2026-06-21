import { NextResponse } from "next/server";
import { getServerSession } from "next-auth";
import { prisma } from "@/lib/prisma";

export async function GET(req: Request) {
  const session = await getServerSession();
  if (!session || !session.user || !session.user.email) {
    return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
  }

  const user = await prisma.user.findUnique({ where: { email: session.user.email } });
  if (!user) return NextResponse.json({ error: "Unauthorized" }, { status: 401 });

  const url = new URL(req.url);
  const sessionId = url.searchParams.get("sessionId");

  if (!sessionId) {
    return NextResponse.json({ error: "sessionId is required" }, { status: 400 });
  }

  const isSuper = user.role === "SUPERADMIN";

  const apiKeys = await prisma.apiKey.findMany({ where: { userId: user.id } });
  const keyIds = apiKeys.map(k => k.id);

  const where: any = { sessionId: sessionId };
  if (!isSuper) {
    where.apiKeyId = { in: keyIds };
  }

  const logs = await prisma.requestLog.findMany({
    where,
    orderBy: { createdAt: "asc" },
  });

  // Generate JSONL
  let jsonl = "";
  for (const log of logs) {
    if (log.originalPayload && log.responsePayload) {
      try {
        const reqPayload = JSON.parse(log.originalPayload);
        const resPayload = JSON.parse(log.responsePayload);
        
        const entry = {
          messages: reqPayload.messages || [],
          output: resPayload.choices && resPayload.choices.length > 0 ? resPayload.choices[0].message : null
        };
        jsonl += JSON.stringify(entry) + "\\n";
      } catch (e) {
        // Skip malformed
      }
    }
  }

  return new NextResponse(jsonl, {
    status: 200,
    headers: {
      "Content-Type": "application/x-ndjson",
      "Content-Disposition": `attachment; filename="session-${sessionId}.jsonl"`
    }
  });
}
