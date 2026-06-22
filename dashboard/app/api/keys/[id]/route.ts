import { NextResponse } from "next/server";
import { getServerSession } from "next-auth";
import { prisma } from "@/lib/prisma";
import { createClient } from "redis";

export async function DELETE(req: Request, { params }: { params: { id: string } }) {
  const session = await getServerSession();
  if (!session || !session.user || !session.user.email) {
    return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
  }

  try {
    const user = await prisma.user.findUnique({ where: { email: session.user.email } });
    if (!user) return NextResponse.json({ error: "Unauthorized" }, { status: 401 });

    const keyId = params.id;
    
    const apiKey = await prisma.apiKey.findUnique({
      where: { id: keyId, userId: user.id }
    });

    if (!apiKey) {
      return NextResponse.json({ error: "Key not found" }, { status: 404 });
    }

    await prisma.apiKey.delete({
      where: { id: keyId }
    });

    const redisClient = createClient({ url: process.env.REDIS_URL || 'redis://localhost:6379' });
    await redisClient.connect();
    await redisClient.del(`synapse:keys:${apiKey.virtualKey}`);
    await redisClient.disconnect();

    return NextResponse.json({ success: true });
  } catch (error) {
    console.error(error);
    return NextResponse.json({ error: "Internal Server Error" }, { status: 500 });
  }
}

export async function PUT(req: Request, { params }: { params: { id: string } }) {
  const session = await getServerSession();
  if (!session || !session.user || !session.user.email) {
    return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
  }

  try {
    const user = await prisma.user.findUnique({ where: { email: session.user.email } });
    if (!user) return NextResponse.json({ error: "Unauthorized" }, { status: 401 });

    const keyId = params.id;
    const body = await req.json();

    const apiKey = await prisma.apiKey.findUnique({
      where: { id: keyId, userId: user.id }
    });

    if (!apiKey) {
      return NextResponse.json({ error: "Key not found" }, { status: 404 });
    }

    const dataToUpdate: any = {};
    if (body.benchmarkMode !== undefined) dataToUpdate.benchmarkMode = body.benchmarkMode;
    if (body.semanticTolerance !== undefined) dataToUpdate.semanticTolerance = parseFloat(body.semanticTolerance);
    if (body.cacheTtl !== undefined) dataToUpdate.cacheTtl = parseInt(body.cacheTtl, 10);
    if (body.monthlyBudget !== undefined) dataToUpdate.monthlyBudget = parseFloat(body.monthlyBudget);
    if (body.isolateCacheByUser !== undefined) dataToUpdate.isolateCacheByUser = !!body.isolateCacheByUser;
    if (body.zeroLog !== undefined) dataToUpdate.zeroLog = !!body.zeroLog;

    if (body.enableL1 !== undefined) dataToUpdate.enableL1 = !!body.enableL1;
    if (body.enableL2 !== undefined) dataToUpdate.enableL2 = !!body.enableL2;
    if (body.enableL3 !== undefined) dataToUpdate.enableL3 = !!body.enableL3;
    if (body.killSwitch !== undefined) dataToUpdate.killSwitch = !!body.killSwitch;
    if (body.fingerprintLoopDetect !== undefined) dataToUpdate.fingerprintLoopDetect = !!body.fingerprintLoopDetect;
    if (body.sessionTokenLimit !== undefined) dataToUpdate.sessionTokenLimit = body.sessionTokenLimit ? parseInt(body.sessionTokenLimit, 10) : null;
    if (body.allowedTools !== undefined) dataToUpdate.allowedTools = body.allowedTools;
    if (body.blockUnknownTools !== undefined) dataToUpdate.blockUnknownTools = !!body.blockUnknownTools;
    if (body.redactPII !== undefined) dataToUpdate.redactPII = !!body.redactPII;
    if (body.toolTtls !== undefined) dataToUpdate.toolTtls = body.toolTtls;

    const updatedKey = await prisma.apiKey.update({
      where: { id: keyId },
      data: dataToUpdate
    });

    const redisClient = createClient({
      url: process.env.REDIS_URL || 'redis://localhost:6379',
      socket: { connectTimeout: 5000 },
      disableOfflineQueue: true,
    });
    redisClient.on('error', (e) => console.error("[PUT /api/keys/[id]] redis error:", e?.message || e));
    try {
      await redisClient.connect();
      const redisData: Record<string, string> = {};
      if (body.benchmarkMode !== undefined) redisData.benchmark_mode = updatedKey.benchmarkMode ? "true" : "false";
      if (body.semanticTolerance !== undefined) redisData.semantic_tolerance = updatedKey.semanticTolerance.toString();
      if (body.cacheTtl !== undefined) redisData.cache_ttl = updatedKey.cacheTtl.toString();
      if (body.monthlyBudget !== undefined) redisData.monthly_budget = updatedKey.monthlyBudget.toString();
      if (body.isolateCacheByUser !== undefined) redisData.isolate_cache_by_user = updatedKey.isolateCacheByUser ? "true" : "false";
      if (body.zeroLog !== undefined) redisData.zero_log = updatedKey.zeroLog ? "true" : "false";

      if (body.enableL1 !== undefined) redisData.enable_l1 = updatedKey.enableL1 ? "true" : "false";
      if (body.enableL2 !== undefined) redisData.enable_l2 = updatedKey.enableL2 ? "true" : "false";
      if (body.enableL3 !== undefined) redisData.enable_l3 = updatedKey.enableL3 ? "true" : "false";
      if (body.killSwitch !== undefined) redisData.kill_switch = updatedKey.killSwitch ? "true" : "false";
      if (body.fingerprintLoopDetect !== undefined) redisData.fingerprint_loop_detect = updatedKey.fingerprintLoopDetect ? "true" : "false";
      if (body.sessionTokenLimit !== undefined) redisData.session_token_limit = updatedKey.sessionTokenLimit ? updatedKey.sessionTokenLimit.toString() : "0";
      if (body.allowedTools !== undefined) redisData.allowed_tools = updatedKey.allowedTools || "";
      if (body.blockUnknownTools !== undefined) redisData.block_unknown_tools = updatedKey.blockUnknownTools ? "true" : "false";
      if (body.redactPII !== undefined) redisData.redact_pii = updatedKey.redactPII ? "true" : "false";
      if (body.toolTtls !== undefined) redisData.tool_ttls = updatedKey.toolTtls || "{}";

      const redisOp = redisClient.hSet(`synapse:keys:${updatedKey.virtualKey}`, redisData);
      const timeout = new Promise((_, reject) =>
        setTimeout(() => reject(new Error("redis hSet timed out after 5s")), 5000)
      );
      await Promise.race([redisOp, timeout]);
    } finally {
      try { await redisClient.disconnect(); } catch {}
    }

    return NextResponse.json(updatedKey);
  } catch (error) {
    console.error(error);
    return NextResponse.json({ error: "Internal Server Error" }, { status: 500 });
  }
}
