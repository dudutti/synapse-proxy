import { NextResponse } from "next/server";
import { prisma } from "@/lib/prisma";

export async function GET(req: Request) {
  try {
    const url = new URL(req.url);
    const token = url.searchParams.get("token");

    if (!token) {
      return new NextResponse("Missing token", { status: 400 });
    }

    const verificationToken = await prisma.verificationToken.findUnique({
      where: { token },
    });

    if (!verificationToken) {
      return new NextResponse("Invalid or expired token", { status: 400 });
    }

    if (new Date() > verificationToken.expires) {
      return new NextResponse("Token expired", { status: 400 });
    }

    await prisma.user.update({
      where: { email: verificationToken.identifier },
      data: { emailVerified: new Date() },
    });

    await prisma.verificationToken.delete({
      where: { token },
    });

    return NextResponse.redirect(`${process.env.NEXT_PUBLIC_APP_URL || 'http://localhost:3000'}/login?verified=true`);
  } catch (error) {
    console.error("[VERIFY_EMAIL_ERROR]", error);
    return new NextResponse("Internal Error", { status: 500 });
  }
}
