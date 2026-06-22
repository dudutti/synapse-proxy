import { NextResponse } from "next/server";
import { getServerSession } from "next-auth";
import { authOptions } from "@/lib/authOptions";
import { prisma } from "@/lib/prisma";

export const dynamic = "force-dynamic";

// /api/explorer/[id] — full payload of a single RequestLog row.
// Available to SUPERADMIN or the owner of the API key. Returns everything except apiKey.

export async function GET(
  _req: Request,
  { params }: { params: { id: string } }
) {
  const session = await getServerSession(authOptions);
  if (!session?.user) {
    return new NextResponse("Unauthorized", { status: 401 });
  }

  const user = session.user as any;
  const isSuper = user.role === "SUPERADMIN";

  const row = await prisma.requestLog.findUnique({
    where: { id: params.id },
    include: {
      apiKey: {
        select: {
          id: true,
          userId: true,
          provider: true,
          virtualKey: true,
        },
      },
    },
  });

  if (!row) {
    return NextResponse.json({ error: "Not found" }, { status: 404 });
  }

  if (!isSuper && row.apiKey?.userId !== user.id) {
    return new NextResponse("Forbidden", { status: 403 });
  }

  // Don't ship apiKey relation fields beyond what's safe to expose
  // (realKeyEnc / fallbackKeyEnc are credentials — only the dedicated
  // payload route can surface those, and even then with auth checks).
  // responsePayload is the heaviest column (multi-MB SSE buffers); the
  // explorer page lazy-fetches it via /api/telemetry/[id]/payload.
  const {
    apiKey,
    responsePayload: _omitResponse,
    ...rowSafe
  } = row as any;

  return NextResponse.json({
    row: rowSafe,
    hasResponsePayload: Boolean(_omitResponse),
  });
}
