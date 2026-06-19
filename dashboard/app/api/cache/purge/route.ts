import { NextResponse } from "next/server";
import { getServerSession } from "next-auth";
import { authOptions } from "@/lib/authOptions";

export async function DELETE(req: Request) {
  const session = await getServerSession(authOptions);
  if (!session) {
    return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
  }

  const { searchParams } = new URL(req.url);
  const vk = searchParams.get("vk") ?? "";

  try {
    const proxyUrl = process.env.PROXY_URL || "http://proxy:8080";
    const res = await fetch(`${proxyUrl}/v1/cache/purge?vk=${encodeURIComponent(vk)}`, {
      method: "DELETE",
    });

    const data = await res.json().catch(() => ({}));
    return NextResponse.json(
      { ok: res.ok, status: res.status, ...data },
      { status: res.ok ? 200 : res.status }
    );
  } catch (error) {
    return NextResponse.json({ error: "Failed to reach proxy" }, { status: 500 });
  }
}
