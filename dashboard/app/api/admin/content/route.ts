import { NextResponse } from "next/server";
import { getServerSession } from "next-auth";
import { authOptions } from "@/lib/authOptions";
import { prisma } from "@/lib/prisma";
import { translations, Language, TranslationItem } from "@/lib/translations";

export async function GET(request: Request) {
  const session = await getServerSession(authOptions);
  if (!session || (session.user as any).role !== "SUPERADMIN") {
    return new NextResponse("Unauthorized", { status: 401 });
  }

  const { searchParams } = new URL(request.url);
  const pageKey = searchParams.get("pageKey");
  const lang = searchParams.get("lang") as Language;

  if (!pageKey || !lang || (lang !== "fr" && lang !== "en")) {
    return new NextResponse("Bad Request: pageKey and lang are required", { status: 400 });
  }

  try {
    const record = await prisma.landingPageContent.findUnique({
      where: {
        pageKey_lang: {
          pageKey,
          lang,
        },
      },
    });

    if (record) {
      return NextResponse.json(JSON.parse(record.content));
    }

    // Return the default from translations.ts
    const pageDefault = translations[pageKey]?.[lang];
    if (pageDefault) {
      return NextResponse.json(pageDefault);
    }

    // Empty template fallback
    const fallback: TranslationItem = {
      metaTitle: "",
      metaDesc: "",
      heroBadge: "",
      heroTitle: "",
      heroDesc: "",
      backBtn: lang === "fr" ? "Retour au Tableau de Bord" : "Back to Dashboard",
      dashboardBtn: lang === "fr" ? "Tableau de Bord" : "Dashboard",
      sections: [],
      videoTitle: "",
      videoDesc: "",
      videoAlt: "",
    };
    return NextResponse.json(fallback);
  } catch (error) {
    console.error("[ADMIN_CONTENT_GET]", error);
    return new NextResponse("Internal Error", { status: 500 });
  }
}

export async function POST(request: Request) {
  const session = await getServerSession(authOptions);
  if (!session || (session.user as any).role !== "SUPERADMIN") {
    return new NextResponse("Unauthorized", { status: 401 });
  }

  try {
    const body = await request.json();
    const { pageKey, lang, content } = body;

    if (!pageKey || !lang || !content || (lang !== "fr" && lang !== "en")) {
      return new NextResponse("Bad Request: pageKey, lang and content are required", { status: 400 });
    }

    // content should be a valid stringified JSON structure
    const contentString = typeof content === "string" ? content : JSON.stringify(content);

    const record = await prisma.landingPageContent.upsert({
      where: {
        pageKey_lang: {
          pageKey,
          lang,
        },
      },
      update: {
        content: contentString,
      },
      create: {
        pageKey,
        lang,
        content: contentString,
      },
    });

    return NextResponse.json({ success: true, record });
  } catch (error) {
    console.error("[ADMIN_CONTENT_POST]", error);
    return new NextResponse("Internal Error", { status: 500 });
  }
}
