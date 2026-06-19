import { NextResponse } from "next/server";
import { prisma } from "@/lib/prisma";

export async function POST(req: Request) {
  try {
    const { email } = await req.json();

    if (!email || !email.includes("@")) {
      return NextResponse.json({ error: "Invalid email" }, { status: 400 });
    }

    const existing = await prisma.prospect.findUnique({
      where: { email }
    });

    if (existing) {
      return NextResponse.json({ message: "Already on the waitlist" }, { status: 200 });
    }

    await prisma.prospect.create({
      data: { email }
    });

    return NextResponse.json({ message: "Added to waitlist" }, { status: 201 });
  } catch (error) {
    return NextResponse.json({ error: "Failed to join waitlist" }, { status: 500 });
  }
}
