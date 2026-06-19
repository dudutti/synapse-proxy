import { NextResponse } from "next/server";
import { getServerSession } from "next-auth";
import { prisma } from "@/lib/prisma";
import crypto from "crypto";

// AES-256-GCM decrypt (matches the encryption in app/api/keys/route.ts
// and the format produced by the dashboard key-creation form).
function decrypt(payload: string): string {
  const raw = process.env.ENCRYPTION_KEY || "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef";
  const buf = Buffer.from(raw, "hex");
  const key = buf.length === 32 ? buf : crypto.createHash("sha256").update(buf).digest();
  if (payload.length < 24 + 32) throw new Error("ciphertext too short");
  const iv = Buffer.from(payload.slice(0, 24), "hex");
  const tag = Buffer.from(payload.slice(24, 56), "hex");
  const ct = Buffer.from(payload.slice(56), "hex");
  const decipher = crypto.createDecipheriv("aes-256-gcm", key, iv);
  decipher.setAuthTag(tag);
  const pt = Buffer.concat([decipher.update(ct), decipher.final()]);
  return pt.toString("utf8");
}

export async function POST(req: Request) {
  try {
    const body = await req.json();
    let provider = body.provider;
    let api_key = body.api_key;

    if (body.virtualKey) {
      const session = await getServerSession();
      if (!session || !session.user || !session.user.email) {
        return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
      }

      const user = await prisma.user.findUnique({ where: { email: session.user.email } });
      const apiKeyRec = await prisma.apiKey.findFirst({
        where: { virtualKey: body.virtualKey, userId: user?.id }
      });
      
      if (!apiKeyRec) {
        return NextResponse.json({ error: "Key not found" }, { status: 404 });
      }
      provider = apiKeyRec.provider;
      api_key = decrypt(apiKeyRec.realKeyEnc);
    }

    const proxyUrl = process.env.PROXY_URL || "http://localhost:8080";
    console.log("Sending to proxy:", `${proxyUrl}/v1/providers/models`, { provider });
    
    const res = await fetch(`${proxyUrl}/v1/providers/models`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ provider, api_key }),
    });
    
    if (!res.ok) {
      const errorText = await res.text();
      console.error("Proxy returned error:", res.status, errorText);
      return NextResponse.json({ error: errorText }, { status: res.status });
    }
    
    const data = await res.json();
    return NextResponse.json(data);
  } catch (error) {
    return NextResponse.json({ error: "Failed to fetch models" }, { status: 500 });
  }
}
