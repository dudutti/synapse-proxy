import { NextRequest, NextResponse } from "next/server";
import { prisma } from "@/lib/prisma";

// We use dynamic import for @xenova/transformers to avoid 
// breaking the Vercel build if it's too large, or we just rely
// on the Node.js environment to run it.
export const maxDuration = 300; // 5 minutes max duration

export async function GET(req: NextRequest) {
  const authHeader = req.headers.get("authorization");
  if (authHeader !== `Bearer ${process.env.CRON_SECRET}`) {
    return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
  }

  try {
    // 1. Fetch 50 untagged requests
    const untaggedLogs = await prisma.requestLog.findMany({
      where: { intentTag: null },
      take: 50,
      select: { id: true, originalPayload: true }
    });

    if (untaggedLogs.length === 0) {
      return NextResponse.json({ success: true, message: "No untagged logs found." });
    }

    // 2. Import pipeline dynamically
    const { pipeline } = await import("@xenova/transformers");
    
    // Use a small zero-shot classification model, or a feature extraction model.
    // For extreme speed, we use a TinyBERT or similar. Here we use the default
    // zero-shot model (Xenova/mobilebert-uncased-mnli)
    const classifier = await pipeline(
      "zero-shot-classification", 
      "Xenova/mobilebert-uncased-mnli"
    );

    const labels = ["coding", "extraction", "chat", "rag", "reasoning"];

    let taggedCount = 0;

    // 3. Process logs
    for (const log of untaggedLogs) {
      try {
        let textToAnalyze = "";
        
        // Try to parse the original payload to extract the prompt
        if (log.originalPayload) {
          try {
            const body = JSON.parse(log.originalPayload);
            if (body.messages && Array.isArray(body.messages)) {
              textToAnalyze = body.messages.map((m: any) => m.content).join(" ");
            } else if (body.prompt) {
              textToAnalyze = body.prompt;
            }
          } catch (e) {
            textToAnalyze = log.originalPayload.substring(0, 1000);
          }
        }

        // Clean text and truncate for the small model (max 512 tokens)
        textToAnalyze = textToAnalyze.substring(0, 500).trim();

        if (textToAnalyze.length < 10) {
          // Too short to classify properly
          await prisma.requestLog.update({
            where: { id: log.id },
            data: { intentTag: "unknown" }
          });
          continue;
        }

        const result = await classifier(textToAnalyze, labels);
        
        // result is { sequence, labels: [...], scores: [...] }
        const bestLabel = result.labels[0];
        const bestScore = result.scores[0];

        // If confidence is too low, mark as unknown
        const assignedTag = bestScore > 0.3 ? bestLabel : "unknown";

        await prisma.requestLog.update({
          where: { id: log.id },
          data: { intentTag: assignedTag }
        });

        taggedCount++;
      } catch (err) {
        console.error(`Failed to classify log ${log.id}:`, err);
        // Fallback so we don't get stuck in a loop
        await prisma.requestLog.update({
          where: { id: log.id },
          data: { intentTag: "error" }
        });
      }
    }

    return NextResponse.json({ success: true, taggedCount });
  } catch (error) {
    console.error("Intent tagger error:", error);
    return NextResponse.json({ error: "Internal Server Error" }, { status: 500 });
  }
}
