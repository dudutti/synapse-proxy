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
      // Try to decrypt the real key. If it fails (typically because the
      // stored ciphertext was encrypted with a different ENCRYPTION_KEY
      // than the one currently in .env), fall back to a static model
      // list for the provider so the user can still see the dropdown
      // and re-key their account without the page crashing in a loop.
      try {
        api_key = decrypt(apiKeyRec.realKeyEnc);
      } catch (e) {
        console.warn(
          `[api/models] decrypt failed for virtualKey ${apiKeyRec.virtualKey.slice(0, 16)}... ` +
          `(provider=${apiKeyRec.provider}). Falling back to a static model list. ` +
          `The user should re-key their API key in Settings to fix this.`,
        );
        return NextResponse.json(staticModelsFor(apiKeyRec.provider));
      }
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

// Static model lists used as a fallback when the stored real key cannot
// be decrypted (typically because ENCRYPTION_KEY in .env was rotated
// after the key was created). The user can re-key their account from
// the Settings page; in the meantime we still want the Add/Edit Key
// form's model dropdown to render so the user is not blocked.
function staticModelsFor(provider: string) {
  const p = provider.toLowerCase();
  if (p === "openai") {
    return { models: [
      { id: "gpt-4o", name: "GPT-4o" },
      { id: "gpt-4o-mini", name: "GPT-4o mini" },
      { id: "gpt-4-turbo", name: "GPT-4 Turbo" },
      { id: "o1-preview", name: "o1 Preview" },
      { id: "o1-mini", name: "o1 mini" },
    ]};
  }
  if (p === "minimax" || p === "MiniMax") {
    return { models: [
      { id: "minimax-m2.7", name: "MiniMax-M3" },
      { id: "minimax-m2", name: "MiniMax M2" },
    ]};
  }
  if (p === "anthropic") {
    return { models: [
      { id: "claude-3-5-sonnet-20241022", name: "Claude 3.5 Sonnet" },
      { id: "claude-3-5-haiku-20241022", name: "Claude 3.5 Haiku" },
      { id: "claude-3-opus-20240229", name: "Claude 3 Opus" },
    ]};
  }
  if (p === "google" || p === "gemini") {
    return { models: [
      { id: "gemini-1.5-pro", name: "Gemini 1.5 Pro" },
      { id: "gemini-1.5-flash", name: "Gemini 1.5 Flash" },
    ]};
  }
  return { models: [] };
}
