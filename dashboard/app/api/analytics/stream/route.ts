import { NextRequest } from "next/server";
import { getServerSession } from "next-auth";
import { authOptions } from "@/lib/authOptions";
import { prisma } from "@/lib/prisma";

export async function GET(req: NextRequest) {
  const session = await getServerSession(authOptions);
  if (!session?.user?.email) {
    return new Response("Unauthorized", { status: 401 });
  }

  const user = await prisma.user.findUnique({
    where: { email: session.user.email },
    include: { apiKeys: true }
  });

  if (!user || user.apiKeys.length === 0) {
    return new Response("No API Keys", { status: 404 });
  }

  const url = new URL(req.url);
  const keyIdParam = url.searchParams.get("keyId");

  let keyIds = user.apiKeys.map(k => k.id);
  if (keyIdParam) {
    if (keyIds.includes(keyIdParam)) {
      keyIds = [keyIdParam];
    } else {
      return new Response("Invalid API Key", { status: 400 });
    }
  }
  let lastCheckedTime = new Date();

  const stream = new ReadableStream({
    async start(controller) {
      const encoder = new TextEncoder();
      
      // Send initial connection message
      controller.enqueue(encoder.encode(`data: ${JSON.stringify({ type: 'connected' })}\n\n`));

      const intervalId = setInterval(async () => {
        try {
          const newLogs = await prisma.requestLog.findMany({
            where: {
              apiKeyId: { in: keyIds },
              createdAt: { gt: lastCheckedTime }
            },
            orderBy: { createdAt: 'asc' },
            select: {
              id: true,
              cacheLevel: true,
              createdAt: true,
              model: true,
              promptTokensOrig: true,
              completionTokensOrig: true,
              promptTokensOpt: true,
              completionTokensOpt: true,
              agentId: true,
              agentLabel: true,
              sessionId: true,
              turnCount: true,
              convSignature: true,
            }
          });

          if (newLogs.length > 0) {
            lastCheckedTime = newLogs[newLogs.length - 1].createdAt;

            for (const log of newLogs) {
              const payload = {
                id: log.id,
                timestamp: log.createdAt,
                reqModel: log.model,
                savedInput: log.promptTokensOrig - log.promptTokensOpt,
                savedOutput: log.completionTokensOrig - log.completionTokensOpt,
                costSaved: Number(log.costSaved) || 0,
                type: log.cacheLevel === 'NONE' ? 'Standard Routing' : `Cache Hit (${log.cacheLevel})`,
                agentId: log.agentId || '',
                agentLabel: log.agentLabel || '',
                sessionId: log.sessionId || '',
                turnCount: log.turnCount ?? 0,
                convSignature: log.convSignature || '',
              };
              // DEBUG: log the first event so we can verify the SSE
              // payload contains convSignature in production.
              // Remove once the Live Telemetry grouping bug is fixed.
              if (!globalThis.__sse_debug_logged) {
                globalThis.__sse_debug_logged = true;
                console.log('[SSE DEBUG] first payload:', JSON.stringify(payload));
              }
              controller.enqueue(encoder.encode(`data: ${JSON.stringify(payload)}\n\n`));
            }
          }
        } catch (err) {
          console.error("SSE Poll error:", err);
        }
      }, 2000);

      // Keep connection alive with a ping
      const pingId = setInterval(() => {
        controller.enqueue(encoder.encode(`: ping\n\n`));
      }, 15000);

      req.signal.addEventListener('abort', () => {
        clearInterval(intervalId);
        clearInterval(pingId);
        controller.close();
      });
    }
  });

  return new Response(stream, {
    headers: {
      "Content-Type": "text/event-stream",
      "Cache-Control": "no-cache",
      "Connection": "keep-alive"
    }
  });
}
