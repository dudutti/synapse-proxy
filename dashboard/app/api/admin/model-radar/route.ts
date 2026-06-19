import { NextResponse } from "next/server";
import { getServerSession } from "next-auth";
import { authOptions } from "@/lib/authOptions";
import { createClient } from "redis";

// /api/admin/model-radar ”” read the in-flight auto-detection state
// from Redis (radar entries, sample counts, discovered usage mappings)
// so the dashboard can render a "Model Radar" panel.
//
// Requires SUPERADMIN. Read-only.
export async function GET() {
  const session = await getServerSession(authOptions);
  if (!session || (session.user as any).role !== "SUPERADMIN") {
    return new NextResponse("Unauthorized", { status: 401 });
  }

  const redisUrl = process.env.REDIS_URL || "redis://redis:6379";
  const client = createClient({ url: redisUrl });
  try {
    await client.connect();

    // 1) List all radar entries
    const entryKeys = await client.keys("synapse:radar:models:*");
    const entries: Array<{
      model_id: string;
      provider: string;
      status: string;
      first_seen: string;
      last_seen: string;
      sample_count: number;
      has_usage_map: boolean;
      prompt_field: string;
      completion_field: string;
      confidence: number;
    }> = [];
    for (const k of entryKeys) {
      const raw = await client.get(k);
      if (!raw) continue;
      try {
        const e = JSON.parse(raw);
        const usageMap = e.UsageMap || e.usage_map;
        let pm = "";
        let cm = "";
        let conf = 0;
        if (usageMap) {
          try {
            const m = JSON.parse(usageMap);
            pm = m.prompt_field || "";
            cm = m.completion_field || "";
            conf = m.confidence_score || 0;
          } catch {}
        }
        entries.push({
          model_id: e.ModelID || e.model_id,
          provider: e.Provider || e.provider,
          status: e.Status || e.status,
          first_seen: e.FirstSeen || e.first_seen,
          last_seen: e.LastSeen || e.last_seen,
          sample_count: e.SampleCnt ?? e.sample_count ?? 0,
          has_usage_map: !!usageMap,
          prompt_field: pm,
          completion_field: cm,
          confidence: conf,
        });
      } catch {}
    }

    // 2) Known-models set size (for context)
    const knownSet = await client.sMembers("synapse:radar:known_models");

    return NextResponse.json({
      entries: entries.sort((a, b) => (a.last_seen < b.last_seen ? 1 : -1)),
      known_models_count: knownSet.length,
    });
  } catch (e: any) {
    return NextResponse.json({ error: e?.message || "redis error" }, { status: 500 });
  } finally {
    try { await client.quit(); } catch {}
  }
}
