import { NextRequest } from "next/server";
import { getServerSession } from "next-auth";
import { authOptions } from "@/lib/authOptions";
import { prisma } from "@/lib/prisma";

export const dynamic = "force-dynamic";

// /api/admin/logs/stream ”” SSE stream of every RequestLog in the
// system, for the SUPERADMIN live console.
//
// Polls Postgres every 1 second. For higher traffic we'd switch to
// Redis Streams (synapse:telemetry:logs) which the proxy already
// writes to, but at current volumes the DB poll is plenty fast and
// survives Postgres restarts without manual re-seeding.
//
// Requires SUPERADMIN role ”” exposes ALL users' traffic, including
// prompt metadata. SUPERADMIN is gated in lib/authOptions.

const FORMAT_BUDGET_MS = 1500; // don't fall behind faster than this

export async function GET(req: NextRequest) {
  const session = await getServerSession(authOptions);
  if (!session || (session.user as any).role !== "SUPERADMIN") {
    return new Response("Unauthorized", { status: 401 });
  }

  let lastCheckedTime = new Date(Date.now() - 10_000); // start with last 10s on connect

  const stream = new ReadableStream({
    async start(controller) {
      const encoder = new TextEncoder();

      // Connection established message
      controller.enqueue(
        encoder.encode(
          `event: connected\ndata: ${JSON.stringify({ at: new Date().toISOString() })}\n\n`
        )
      );

      const intervalId = setInterval(async () => {
        const t0 = Date.now();
        try {
          const newLogs = await prisma.requestLog.findMany({
            where: {
              createdAt: { gt: lastCheckedTime },
            },
            orderBy: { createdAt: "asc" },
            take: 50, // cap per poll
            select: {
              id: true,
              cacheLevel: true,
              createdAt: true,
              model: true,
              provider: true,
              promptTokensOrig: true,
              completionTokensOrig: true,
              promptTokensOpt: true,
              completionTokensOpt: true,
              durationMs: true,
              costSaved: true,
              agentId: true,
              agentLabel: true,
              sessionId: true,
              apiKeyId: true,
              perHookSavings: true,
            },
          });

          if (newLogs.length > 0) {
            lastCheckedTime = newLogs[newLogs.length - 1].createdAt;
            for (const log of newLogs) {
              controller.enqueue(
                encoder.encode(
                  `data: ${JSON.stringify({
                    id: log.id,
                    ts: log.createdAt,
                    cacheLevel: log.cacheLevel,
                    model: log.model,
                    provider: log.provider,
                    tokensIn: log.promptTokensOrig,
                    tokensOut: log.completionTokensOrig,
                    tokensInOpt: log.promptTokensOpt,
                    tokensOutOpt: log.completionTokensOpt,
                    durationMs: log.durationMs,
                    costSaved: Number(log.costSaved) || 0,
                    agentId: log.agentId || "",
                    agentLabel: log.agentLabel || "",
                    sessionId: log.sessionId || "",
                    apiKeyId: log.apiKeyId,
                    perHookSavings: log.perHookSavings,
                  })}\n\n`
                )
              );
            }
          }

          // If the format phase took too long, log a warning so we know
          // to switch to Redis Streams.
          const took = Date.now() - t0;
          if (took > FORMAT_BUDGET_MS) {
            controller.enqueue(
              encoder.encode(
                `: warning budget_exceeded ${took}ms\n\n`
              )
            );
          }
        } catch (err) {
          console.error("[admin/logs/stream] poll error:", err);
          controller.enqueue(
            encoder.encode(`event: error\ndata: ${JSON.stringify({ message: String(err) })}\n\n`)
          );
        }
      }, 1000);

      const pingId = setInterval(() => {
        try {
          controller.enqueue(encoder.encode(`: ping ${Date.now()}\n\n`));
        } catch {}
      }, 15000);

      req.signal.addEventListener("abort", () => {
        clearInterval(intervalId);
        clearInterval(pingId);
        try {
          controller.close();
        } catch {}
      });
    },
  });

  return new Response(stream, {
    headers: {
      "Content-Type": "text/event-stream",
      "Cache-Control": "no-cache, no-transform",
      Connection: "keep-alive",
      "X-Accel-Buffering": "no", // disable nginx-style buffering
    },
  });
}
