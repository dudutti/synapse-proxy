import { NextResponse } from "next/server";
import { getServerSession } from "next-auth";
import { authOptions } from "@/lib/authOptions";
import { writeFile, mkdir } from "fs/promises";
import path from "path";
import { existsSync } from "fs";

export async function POST(request: Request) {
  const session = await getServerSession(authOptions);
  if (!session || (session.user as any).role !== "SUPERADMIN") {
    return new NextResponse("Unauthorized", { status: 401 });
  }

  try {
    const formData = await request.formData();
    const file = formData.get("file") as File | null;

    if (!file) {
      return new NextResponse("No file uploaded", { status: 400 });
    }

    const bytes = await file.arrayBuffer();
    const buffer = Buffer.from(bytes);

    const ext = path.extname(file.name);
    const safeName = file.name.replace(/[^a-zA-Z0-9.\-_]/g, "").replace(ext, "");
    const uniqueFilename = `${Date.now()}-${safeName}${ext}`;

    const uploadDir = path.join(process.cwd(), "public", "uploads");
    
    if (!existsSync(uploadDir)) {
      await mkdir(uploadDir, { recursive: true });
    }

    const filepath = path.join(uploadDir, uniqueFilename);
    await writeFile(filepath, buffer);

    const fileUrl = `/uploads/${uniqueFilename}`;

    return NextResponse.json({ success: true, url: fileUrl });
  } catch (error) {
    console.error("[ADMIN_UPLOAD_POST]", error);
    return new NextResponse("Internal Error", { status: 500 });
  }
}
