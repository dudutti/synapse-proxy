"use client";

import { useState } from "react";
import { Code, ExternalLink, Copy, Check, FileCode2, Download } from "lucide-react";
import { toast } from "sonner";

// Detects ```language\n...\n``` blocks in assistant text and renders them
// as syntax-highlighted code (plain text, no Shiki dependency) with a
// preview pane for HTML. Falls back to plain text when no blocks found.
interface ParsedBlock {
  kind: "code" | "html" | "text";
  language: string;
  content: string;
}

export function parseBlocks(content: string): ParsedBlock[] {
  if (!content) return [];
  const regex = /```(\w+)?\n?([\s\S]*?)```/g;
  const blocks: ParsedBlock[] = [];
  let lastIndex = 0;
  let match: RegExpExecArray | null;

  while ((match = regex.exec(content)) !== null) {
    if (match.index > lastIndex) {
      const text = content.slice(lastIndex, match.index);
      if (text.trim()) blocks.push({ kind: "text", language: "", content: text });
    }
    const lang = (match[1] || "").toLowerCase();
    const code = match[2].replace(/\n$/, "");
    const kind: "code" | "html" =
      lang === "html" || lang === "htm" ? "html" : "code";
    blocks.push({ kind, language: lang, content: code });
    lastIndex = regex.lastIndex;
  }
  if (lastIndex < content.length) {
    const text = content.slice(lastIndex);
    if (text.trim()) blocks.push({ kind: "text", language: "", content: text });
  }
  return blocks.length > 0 ? blocks : [{ kind: "text", language: "", content }];
}

export function ArtifactRenderer({ content }: { content: string }) {
  const blocks = parseBlocks(content);
  const hasArtifacts = blocks.some((b) => b.kind === "html" || (b.kind === "code" && b.content.length > 20));

  if (!hasArtifacts) {
    return (
      <div className="whitespace-pre-wrap text-sm leading-relaxed">{content}</div>
    );
  }

  return (
    <div className="space-y-3">
      {blocks.map((block, idx) => {
        if (block.kind === "text") {
          return (
            <div key={idx} className="whitespace-pre-wrap text-sm leading-relaxed">
              {block.content}
            </div>
          );
        }
        if (block.kind === "html") {
          return <HtmlArtifact key={idx} content={block.content} />;
        }
        return <CodeArtifact key={idx} content={block.content} language={block.language} />;
      })}
    </div>
  );
}

function CodeArtifact({ content, language }: { content: string; language: string }) {
  const [copied, setCopied] = useState(false);
  const downloadSnippet = () => {
    const blob = new Blob([content], { type: "text/plain;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `snippet.${language || "txt"}`;
    a.click();
    URL.revokeObjectURL(url);
  };
  return (
    <div className="my-2 rounded-lg border border-white/10 bg-black/40 overflow-hidden">
      <div className="flex items-center justify-between px-3 py-1.5 bg-white/5 border-b border-white/5">
        <div className="flex items-center gap-2 text-[10px] text-zinc-400 uppercase tracking-wider">
          <Code className="w-3 h-3" />
          {language || "code"}
        </div>
        <div className="flex items-center gap-1">
          <button
            type="button"
            onClick={async () => {
              try {
                await navigator.clipboard.writeText(content);
                setCopied(true);
                setTimeout(() => setCopied(false), 1200);
              } catch {}
            }}
            className="text-[10px] text-zinc-400 hover:text-white px-2 py-1 rounded hover:bg-white/5 flex items-center gap-1"
            title="Copy code"
          >
            {copied ? <Check className="w-3 h-3" /> : <Copy className="w-3 h-3" />}
            {copied ? "Copied" : "Copy"}
          </button>
          <button
            type="button"
            onClick={downloadSnippet}
            className="text-[10px] text-zinc-400 hover:text-white px-2 py-1 rounded hover:bg-white/5 flex items-center gap-1"
            title="Download snippet"
          >
            <ExternalLink className="w-3 h-3" />
            Download
          </button>
        </div>
      </div>
      <pre className="px-4 py-3 text-xs font-mono text-zinc-200 overflow-x-auto whitespace-pre-wrap break-all leading-relaxed max-h-96">
        {content}
      </pre>
    </div>
  );
}

function HtmlArtifact({ content }: { content: string }) {
  const [copied, setCopied] = useState(false);
  const [showSource, setShowSource] = useState(false);

  // Sandbox the iframe: scripts run, but no same-origin access (sandbox
  // attribute without allow-same-origin). External network is unrestricted
  // but we don't load user-provided scripts that touch parent context.
  const srcDoc = `<!doctype html><html><head><meta charset="utf-8"><style>body{margin:0;padding:12px;font-family:system-ui,-apple-system,sans-serif;background:#0a0a0a;color:#fff}a{color:#34d399}pre,code{background:#000;color:#10b981;padding:2px 6px;border-radius:4px}</style></head><body>${content}</body></html>`;

  const openInNewTab = () => {
    // Use a Blob URL instead of a data URI: Chrome and Firefox honour
    // `target=_blank` on Blob URLs, but block it on data: URIs (Safari
    // blocks the download too).
    const blob = new Blob([srcDoc], { type: "text/html;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const win = window.open(url, "_blank", "noopener,noreferrer");
    // Free the URL after a few seconds (the new tab has loaded it).
    setTimeout(() => URL.revokeObjectURL(url), 5000);
    if (!win) toast("Popup blocked — allow popups to open artifacts in a new tab");
  };

  const downloadHtml = () => {
    const blob = new Blob([content], { type: "text/html;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `artifact-${Date.now()}.html`;
    a.click();
    URL.revokeObjectURL(url);
  };

  return (
    <div className="my-2 rounded-lg border border-emerald-500/30 bg-black/40 overflow-hidden">
      <div className="flex items-center justify-between px-3 py-1.5 bg-emerald-500/10 border-b border-emerald-500/20">
        <div className="flex items-center gap-2 text-[10px] text-emerald-300 uppercase tracking-wider font-bold">
          <FileCode2 className="w-3 h-3" />
          HTML Artifact (sandboxed)
        </div>
        <div className="flex items-center gap-1">
          <button
            type="button"
            onClick={async () => {
              try {
                await navigator.clipboard.writeText(content);
                setCopied(true);
                setTimeout(() => setCopied(false), 1200);
              } catch {}
            }}
            className="text-[10px] text-emerald-300 hover:text-white px-2 py-1 rounded hover:bg-white/5 flex items-center gap-1"
            title="Copy HTML source"
          >
            {copied ? <Check className="w-3 h-3" /> : <Copy className="w-3 h-3" />}
            {copied ? "Copied" : "Copy"}
          </button>
          <button
            type="button"
            onClick={openInNewTab}
            className="text-[10px] text-emerald-300 hover:text-white px-2 py-1 rounded hover:bg-white/5 flex items-center gap-1"
            title="Open rendered HTML in a new tab"
          >
            <ExternalLink className="w-3 h-3" />
            Open
          </button>
          <button
            type="button"
            onClick={downloadHtml}
            className="text-[10px] text-emerald-300 hover:text-white px-2 py-1 rounded hover:bg-white/5 flex items-center gap-1"
            title="Download .html file"
          >
            <Download className="w-3 h-3" />
            Download
          </button>
          <button
            type="button"
            onClick={() => setShowSource(!showSource)}
            className="text-[10px] text-emerald-300 hover:text-white px-2 py-1 rounded hover:bg-white/5"
            title="Toggle source view"
          >
            {showSource ? "Preview" : "Source"}
          </button>
        </div>
      </div>
      {showSource ? (
        <pre className="px-4 py-3 text-xs font-mono text-zinc-200 overflow-x-auto whitespace-pre-wrap break-all leading-relaxed max-h-96">
          {content}
        </pre>
      ) : (
        <iframe
          title="HTML artifact preview"
          srcDoc={srcDoc}
          sandbox=""
          className="w-full min-h-[280px] bg-[#0a0a0a] border-0"
        />
      )}
    </div>
  );
}
