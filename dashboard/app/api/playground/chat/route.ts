import { NextResponse } from "next/server";
export const dynamic = 'force-dynamic';

// Extract usage + cache metadata from an OpenAI-compatible response.
// Returns null if usage is missing.
function extractUsage(parsed: any): {
  cacheLevel?: string;
  promptTokensOrig?: number;
  completionTokensOrig?: number;
  promptTokensOpt?: number;
  completionTokensOpt?: number;
  costSaved?: number;
} {
  // Synapse Proxy custom headers are exposed via X-SynapseProxy-* (we read them
  // from proxyRes.headers at the call site). For now parse the upstream
  // response body if it includes the standard usage block.
  const usage = parsed?.usage;
  if (!usage) return {};
  return {
    promptTokensOrig: usage.prompt_tokens,
    completionTokensOrig: usage.completion_tokens,
    promptTokensOpt: usage.prompt_tokens,
    completionTokensOpt: usage.completion_tokens,
    costSaved: 0,
  };
}

export async function POST(req: Request) {
  try {
    const { virtualKey, model, messages, stream, bypass, sessionId } = await req.json();

    if (!virtualKey) {
      return NextResponse.json({ error: "No virtual key provided" }, { status: 400 });
    }

    const startTime = Date.now();
    const headers: any = {
      "Content-Type": "application/json",
      "Authorization": `Bearer ${virtualKey}`,
    };
    if (bypass) {
      headers["X-Bypass-Cache"] = "true";
    }
    // Propagate the Session Recording id to the proxy so the
    // generated RequestLog row is tagged with the same session
    // id. The proxy reads this header in proxy.go:102-104 and
    // passes it to PushTelemetry, which writes it into the
    // RequestLog.sessionId column. Without this, the per-session
    // /api/analytics/session query returns zero rows.
    if (sessionId) {
      headers["X-SynapseProxy-Session"] = sessionId;
    }

    const proxyUrl = process.env.PROXY_URL || "http://localhost:8080";
    const proxyRes = await fetch(`${proxyUrl}/v1/chat/completions`, {
      method: "POST",
      headers,
      body: JSON.stringify({ model, messages, stream: !!stream }),
      cache: "no-store",
    });

    const proxyContentType = proxyRes.headers.get("content-type") || "";
    // Synapse Proxy-specific telemetry headers exposed by the proxy on each
    // response. The proxy forwards these on cached (L1/L2/LOOP) and
    // uncompressed (L3) responses alike. Empty when bypassed through
    // a provider that doesn't set them.
    const cacheLevel = proxyRes.headers.get("X-SynapseProxy-Cache") || "";
    const tokensIn = parseInt(proxyRes.headers.get("X-SynapseProxy-Tokens-In") || "0", 10);
    const tokensOut = parseInt(proxyRes.headers.get("X-SynapseProxy-Tokens-Out") || "0", 10);
    const costSaved = parseFloat(proxyRes.headers.get("X-SynapseProxy-Cost-Saved") || "0");
    const costWithout = parseFloat(proxyRes.headers.get("X-SynapseProxy-Cost-Without") || "0");
    const costWith = parseFloat(proxyRes.headers.get("X-SynapseProxy-Cost-With") || "0");

    // If streaming was requested AND the proxy returned a stream, pipe the
    // SSE stream directly through and append an `event: stats` line at the
    // end so the client can attach metadata to the bubble.
    if (stream && proxyRes.body && proxyContentType.includes("text/event-stream")) {
      const latency = Date.now() - startTime;
      const statsEvent = `event: stats\ndata: ${JSON.stringify({
        latencyMs: latency,
        cacheLevel,
        tokensIn,
        tokensOut,
        costSaved,
        costWithout,
        costWith,
      })}\n\n`;

      // TransformStream to inject the stats event just before the final [DONE].
      const { readable, writable } = new TransformStream();
      const reader = proxyRes.body.getReader();
      const writer = writable.getWriter();
      const encoder = new TextEncoder();
      (async () => {
        try {
          let buffer = "";
          while (true) {
            const { done, value } = await reader.read();
            if (done) break;
            buffer += new TextDecoder().decode(value, { stream: true });
            // Find the last [DONE] sentinel and append stats before it.
            const idx = buffer.lastIndexOf("data: [DONE]");
            if (idx !== -1) {
              const head = buffer.slice(0, idx);
              await writer.write(encoder.encode(head));
              await writer.write(encoder.encode(statsEvent));
              await writer.write(encoder.encode("data: [DONE]\n\n"));
              buffer = "";
            } else {
              // Hold partial chunks until we see the sentinel.
              // If we have lots of buffered text without DONE, just flush.
              if (buffer.length > 64 * 1024) {
                await writer.write(encoder.encode(buffer));
                buffer = "";
              }
            }
          }
          // Stream ended without [DONE] — flush + append stats.
          if (buffer) await writer.write(encoder.encode(buffer));
          await writer.write(encoder.encode(statsEvent));
        } finally {
          try { writer.close(); } catch {}
        }
      })();

      return new Response(readable, {
        headers: {
          "Content-Type": "text/event-stream",
          "Cache-Control": "no-cache",
          "Connection": "keep-alive",
          "X-Latency-Ms": latency.toString(),
        },
      });
    }

    // Non-streaming: return the full JSON as before + stats
    const data = await proxyRes.text();
    const latency = Date.now() - startTime;

    let parsed: any = {};
    try { parsed = JSON.parse(data); } catch {}
    const usage = extractUsage(parsed);

    return NextResponse.json({
      response: data,
      latencyMs: latency,
      status: proxyRes.status,
      stats: {
        cacheLevel,
        tokensIn: tokensIn || usage.promptTokensOrig || 0,
        tokensOut: tokensOut || usage.completionTokensOrig || 0,
        costSaved: costSaved || usage.costSaved || 0,
        costWithout: costWithout || 0,
        costWith: costWith || 0,
      },
    }, { status: proxyRes.status });
  } catch (error) {
    console.error(error);
    return NextResponse.json({ error: "Failed to reach Synapse Proxy Go Proxy" }, { status: 500 });
  }
}
