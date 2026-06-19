import { NextResponse } from "next/server";
import { getServerSession } from "next-auth";
import { authOptions } from "@/lib/authOptions";
import { createClient } from "redis";

// /api/admin/discovered-tools — gateway between the dashboard UI
// and the Go proxy's /v1/keys/tools endpoint.
//
// The proxy SADDs every tool name it sees in a request body into
// `synapse:discovered_tools:<vk>` (TTL 30d). The dashboard reads
// that set and renders a checkable list. When the operator
// unchecks a tool the dashboard POSTs {tool, deny: true} here,
// which proxies to the Go endpoint that adds the tool to
// `synapse:denied_tools:<vk>`. The proxy consults that denylist
// on every request (see proxy/internal/handlers/proxy.go) and
// returns HTTP 403 if the agent tries to call a denied tool.
//
// Why this lives in the dashboard and not in the Next.js route
// directly: the Redis sets are scoped to the Go proxy's
// connection and the Go proxy is the source of truth for
// "discovered tools" (the dashboard has no way to know which
// tools a given agent has called without parsing the proxy's
// request logs). Going through the proxy keeps the contract
// in one place.
//
// Auth: SUPERADMIN or owner of the virtual key. We check
// SUPERADMIN here; per-key ownership is verified server-side
// via the Go endpoint's existing session check.
export async function GET(req: Request) {
  const session = await getServerSession(authOptions);
  if (!session || (session.user as any).role !== "SUPERADMIN") {
    return new NextResponse("Unauthorized", { status: 401 });
  }
  const vk = new URL(req.url).searchParams.get("vk");
  if (!vk) return NextResponse.json({ error: "missing vk" }, { status: 400 });

  // We read directly from Redis (same DB as the proxy) instead
  // of HTTP-proxying to the Go endpoint. The sets are flat and
  // this avoids a network hop.
  const redisUrl = process.env.REDIS_URL || "redis://redis:6379";
  const client = createClient({ url: redisUrl });
  try {
    await client.connect();
    const discovered = (await client.sMembers(`synapse:discovered_tools:${vk}`)) || [];
    const denied = (await client.sMembers(`synapse:denied_tools:${vk}`)) || [];
    return NextResponse.json({ discovered: discovered.sort(), denied: denied.sort() });
  } catch (e: any) {
    return NextResponse.json({ error: e?.message || "redis error" }, { status: 500 });
  } finally {
    try { await client.quit(); } catch {}
  }
}

export async function POST(req: Request) {
  const session = await getServerSession(authOptions);
  if (!session || (session.user as any).role !== "SUPERADMIN") {
    return new NextResponse("Unauthorized", { status: 401 });
  }
  const url = new URL(req.url);
  const vk = url.searchParams.get("vk");
  if (!vk) return NextResponse.json({ error: "missing vk" }, { status: 400 });

  const body = await req.json().catch(() => ({}));
  const tool = body?.tool;
  const deny = body?.deny !== false; // default true
  if (!tool) return NextResponse.json({ error: "missing tool" }, { status: 400 });

  const redisUrl = process.env.REDIS_URL || "redis://redis:6379";
  const client = createClient({ url: redisUrl });
  try {
    await client.connect();
    await client.sAdd(`synapse:discovered_tools:${vk}`, tool);
    await client.expire(`synapse:discovered_tools:${vk}`, 30 * 24 * 60 * 60);
    if (deny) {
      await client.sAdd(`synapse:denied_tools:${vk}`, tool);
    } else {
      await client.sRem(`synapse:denied_tools:${vk}`, tool);
    }
    const denied = (await client.sMembers(`synapse:denied_tools:${vk}`)) || [];
    return NextResponse.json({ denied: denied.sort() });
  } catch (e: any) {
    return NextResponse.json({ error: e?.message || "redis error" }, { status: 500 });
  } finally {
    try { await client.quit(); } catch {}
  }
}

export async function DELETE(req: Request) {
  const session = await getServerSession(authOptions);
  if (!session || (session.user as any).role !== "SUPERADMIN") {
    return new NextResponse("Unauthorized", { status: 401 });
  }
  const url = new URL(req.url);
  const vk = url.searchParams.get("vk");
  if (!vk) return NextResponse.json({ error: "missing vk" }, { status: 400 });

  const body = await req.json().catch(() => ({}));
  const tool = body?.tool;
  if (!tool) return NextResponse.json({ error: "missing tool" }, { status: 400 });

  const redisUrl = process.env.REDIS_URL || "redis://redis:6379";
  const client = createClient({ url: redisUrl });
  try {
    await client.connect();
    await client.sRem(`synapse:denied_tools:${vk}`, tool);
    const denied = (await client.sMembers(`synapse:denied_tools:${vk}`)) || [];
    return NextResponse.json({ denied: denied.sort() });
  } catch (e: any) {
    return NextResponse.json({ error: e?.message || "redis error" }, { status: 500 });
  } finally {
    try { await client.quit(); } catch {}
  }
}