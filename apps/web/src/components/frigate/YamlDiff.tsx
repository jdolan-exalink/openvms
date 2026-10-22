import { useMemo } from "react";
import { type DiffLine, lineDiff } from "@/lib/frigateSchema";
import { cn } from "@/lib/cn";

/** Collapse unchanged runs while keeping changed lines with a little context. */
function collapse(lines: DiffLine[], context = 2): (DiffLine | { kind: "gap"; count: number })[] {
  const keep = new Set<number>();
  lines.forEach((line, index) => {
    if (line.kind === "same") return;
    for (let current = Math.max(0, index - context); current <= Math.min(lines.length - 1, index + context); current++) {
      keep.add(current);
    }
  });
  const output: (DiffLine | { kind: "gap"; count: number })[] = [];
  let gap = 0;
  lines.forEach((line, index) => {
    if (keep.has(index)) {
      if (gap) output.push({ kind: "gap", count: gap });
      gap = 0;
      output.push(line);
    } else {
      gap++;
    }
  });
  if (gap) output.push({ kind: "gap", count: gap });
  return output;
}

export function YamlDiff({ before, after }: { before: string; after: string }) {
  const rows = useMemo(() => collapse(lineDiff(before, after)), [before, after]);
  if (!rows.some((row) => row.kind !== "gap")) return <p className="text-sm text-muted">El archivo no cambió.</p>;
  return (
    <pre aria-label="Diferencias del YAML" className="max-h-96 overflow-auto rounded-m3-md bg-surface-2 p-3 font-mono text-xs">
      {rows.map((row, index) =>
        row.kind === "gap" ? (
          <div key={index} className="text-muted">… {row.count} líneas sin cambios …</div>
        ) : (
          <div key={index} className={cn("whitespace-pre-wrap", row.kind === "add" && "bg-ok/15 text-ok", row.kind === "del" && "bg-bad/15 text-bad")}>
            {row.kind === "add" ? "+ " : row.kind === "del" ? "- " : "  "}
            {row.text}
          </div>
        ),
      )}
    </pre>
  );
}
