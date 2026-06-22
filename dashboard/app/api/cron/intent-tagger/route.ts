import { NextRequest, NextResponse } from "next/server";
import { prisma } from "@/lib/prisma";

// We use dynamic import for @xenova/transformers to avoid
// breaking the Vercel build if it's too large, or we just rely
// on the Node.js environment to run it.
export const maxDuration = 300; // 5 minutes max duration

// Persist the classifier on globalThis so a hot cron re-run doesn't
// re-download + re-init the model. Loading takes 2-10s the first time
// (cold download) and the model is ~80MB, so re-loading every tick
// (when the user hits the cron manually or via a schedule) was the
// main reason the cron exceeded its 5-min budget on a batch of 50.
type Classifier = (text: string, labels: string[]) => Promise<{
  sequence: string;
  labels: string[];
  scores: number[];
}>;
declare global {
  // eslint-disable-next-line no-var
  var __intent_classifier__: Promise<Classifier> | undefined;
}

async function getClassifier(): Promise<Classifier> {
  if (globalThis.__intent_classifier__) {
    return globalThis.__intent_classifier__;
  }
  const { pipeline } = await import("@xenova/transformers");
  globalThis.__intent_classifier__ = pipeline(
    "zero-shot-classification",
    "Xenova/mobilebert-uncased-mnli",
  ) as Promise<Classifier>;
  return globalThis.__intent_classifier__;
}

export async function GET(req: NextRequest) {
  const authHeader = req.headers.get("authorization");
  if (authHeader !== `Bearer ${process.env.CRON_SECRET}`) {
    return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
  }

  try {
    // 1. Fetch 50 untagged requests.
    const untaggedLogs = await prisma.requestLog.findMany({
      where: { intentTag: null },
      take: 50,
      select: { id: true, originalPayload: true },
    });

    if (untaggedLogs.length === 0) {
      return NextResponse.json({ success: true, message: "No untagged logs found." });
    }

    // 2. Load classifier once per process (warm on subsequent calls).
    const classifier = await getClassifier();

    const labels = ["coding", "extraction", "chat", "rag", "reasoning"];

    // 3. Process logs. Build all update payloads in memory and apply
    //    them in a single bulk UPDATE at the end — instead of one
    //    round-trip per row, which on a 50-row batch used to issue
    //    50 separate UPDATEs.
    const updates: { id: string; tag: string }[] = [];

    for (const log of untaggedLogs) {
      try {
        let textToAnalyze = "";

        if (log.originalPayload) {
          try {
            const body = JSON.parse(log.originalPayload);
            if (body.messages && Array.isArray(body.messages)) {
              textToAnalyze = body.messages
                .map((m: any) =>
                  typeof m.content === "string"
                    ? m.content
                    : Array.isArray(m.content)
                    ? m.content.map((c: any) => c?.text ?? "").join(" ")
                    : ""
                )
                .join(" ");
            } else if (body.prompt) {
              textToAnalyze = body.prompt;
            }
          } catch {
            textToAnalyze = log.originalPayload.substring(0, 1000);
          }
        }

        // Clean text and truncate for the small model (max 512 tokens).
        textToAnalyze = textToAnalyze.substring(0, 500).trim();

        if (textToAnalyze.length < 10) {
          updates.push({ id: log.id, tag: "unknown" });
          continue;
        }

        const result = await classifier(textToAnalyze, labels);
        const bestLabel = result.labels[0];
        const bestScore = result.scores[0];
        updates.push({
          id: log.id,
          tag: bestScore > 0.3 ? bestLabel : "unknown",
        });
      } catch (err) {
        console.error(`Failed to classify log ${log.id}:`, err);
        updates.push({ id: log.id, tag: "error" });
      }
    }

    // Apply all updates as a transaction. Prisma's $transaction with
    // an array of plain updateMany-style operations is sequential —
    // we instead loop the small update calls but inside a single
    // transaction so we get atomicity + a single COMMIT.
    if (updates.length > 0) {
      await prisma.$transaction(
        updates.map((u) =>
          prisma.requestLog.update({
            where: { id: u.id },
            data: { intentTag: u.tag },
          })
        )
      );
    }

    return NextResponse.json({ success: true, taggedCount: updates.length });
  } catch (error) {
    console.error("Intent tagger error:", error);
    return NextResponse.json({ error: "Internal Server Error" }, { status: 500 });
  }
}