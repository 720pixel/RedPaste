import { useMemo, useRef, useState } from "react";
import { Copy, Share2 } from "lucide-react";
import { buildDiff, type Precision, type Row, type Seg } from "./model";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { Label } from "@/components/ui/label";
import { toast } from "@/lib/toast";

type Layout = "split" | "unified";

async function copyText(text: string, label: string) {
  try {
    await navigator.clipboard.writeText(text);
    toast(`${label} copied`, "success");
  } catch {
    toast("Copy failed. Select and copy manually", "error");
  }
}

const lineCount = (s: string) => (s === "" ? 0 : s.split("\n").length);

interface UnifiedLine {
  kind: "eq" | "del" | "add";
  leftNum?: number;
  rightNum?: number;
  segs: Seg[];
}

function toUnifiedLines(rows: Row[]): UnifiedLine[] {
  const out: UnifiedLine[] = [];
  for (const row of rows) {
    if (row.kind === "eq") {
      out.push({ kind: "eq", leftNum: row.left?.num, rightNum: row.right?.num, segs: row.left?.segs ?? [] });
    } else if (row.kind === "change") {
      out.push({ kind: "del", leftNum: row.left?.num, segs: row.left?.segs ?? [] });
      out.push({ kind: "add", rightNum: row.right?.num, segs: row.right?.segs ?? [] });
    } else if (row.kind === "del") {
      out.push({ kind: "del", leftNum: row.left?.num, segs: row.left?.segs ?? [] });
    } else {
      out.push({ kind: "add", rightNum: row.right?.num, segs: row.right?.segs ?? [] });
    }
  }
  return out;
}

type Collapsible<T> = { kind: "item"; item: T } | { kind: "gap"; count: number };

// hides long stretches of unchanged context, keeping a few lines on each side
// of a change so the reader still has orientation.
function collapseContext<T>(items: T[], isContext: (t: T) => boolean, context: number): Collapsible<T>[] {
  const out: Collapsible<T>[] = [];
  let i = 0;
  while (i < items.length) {
    if (!isContext(items[i])) {
      out.push({ kind: "item", item: items[i] });
      i++;
      continue;
    }
    let j = i;
    while (j < items.length && isContext(items[j])) j++;
    const runLen = j - i;
    if (runLen <= context * 2) {
      for (let k = i; k < j; k++) out.push({ kind: "item", item: items[k] });
    } else {
      for (let k = i; k < i + context; k++) out.push({ kind: "item", item: items[k] });
      out.push({ kind: "gap", count: runLen - context * 2 });
      for (let k = j - context; k < j; k++) out.push({ kind: "item", item: items[k] });
    }
    i = j;
  }
  return out;
}

function GapRow({ count, spanCols }: { count: number; spanCols: boolean }) {
  return (
    <div className={(spanCols ? "col-span-2 " : "") + "border-y border-border bg-muted/40 px-3 py-1 text-center text-xs text-muted-foreground"}>
      ⋯ {count} unchanged line{count === 1 ? "" : "s"} ⋯
    </div>
  );
}

function Segs({ segs, markClass, wrap }: { segs: Seg[]; markClass: string; wrap: boolean }) {
  return (
    <span className={"px-2 py-0.5 " + (wrap ? "whitespace-pre-wrap break-all" : "whitespace-pre")}>
      {segs.map((s, i) =>
        s.changed ? (
          <mark key={i} className={markClass}>
            {s.text}
          </mark>
        ) : (
          <span key={i}>{s.text}</span>
        ),
      )}
    </span>
  );
}

function SplitCells({ row, wrap }: { row: Row; wrap: boolean }) {
  const changed = row.kind !== "eq";
  const leftTint = row.kind === "del" || row.kind === "change" ? "bg-destructive/10" : "";
  const rightTint = row.kind === "add" || row.kind === "change" ? "bg-success/10" : "";
  return (
    <>
      <div className={"flex " + leftTint} data-change={changed ? "true" : undefined}>
        <span className="w-10 shrink-0 select-none px-2 py-0.5 text-right text-muted-foreground">{row.left?.num ?? ""}</span>
        <Segs segs={row.left?.segs ?? []} markClass="bg-destructive/30 text-foreground" wrap={wrap} />
      </div>
      <div className={"flex border-l border-border " + rightTint}>
        <span className="w-10 shrink-0 select-none px-2 py-0.5 text-right text-muted-foreground">{row.right?.num ?? ""}</span>
        <Segs segs={row.right?.segs ?? []} markClass="bg-success/30 text-foreground" wrap={wrap} />
      </div>
    </>
  );
}

function UnifiedRow({ line, wrap }: { line: UnifiedLine; wrap: boolean }) {
  const tint = line.kind === "del" ? "bg-destructive/10" : line.kind === "add" ? "bg-success/10" : "";
  const markClass = line.kind === "del" ? "bg-destructive/30 text-foreground" : "bg-success/30 text-foreground";
  return (
    <div className={"flex " + tint} data-change={line.kind !== "eq" ? "true" : undefined}>
      <span className="w-10 shrink-0 select-none px-2 py-0.5 text-right text-muted-foreground">{line.leftNum ?? ""}</span>
      <span className="w-10 shrink-0 select-none border-r border-border px-2 py-0.5 text-right text-muted-foreground">
        {line.rightNum ?? ""}
      </span>
      <Segs segs={line.segs} markClass={markClass} wrap={wrap} />
    </div>
  );
}

export function DiffWorkspace({
  leftName,
  rightName,
  leftText,
  rightText,
  onShare,
}: {
  leftName: string;
  rightName: string;
  leftText: string;
  rightText: string;
  onShare?: () => void;
}) {
  const [layout, setLayout] = useState<Layout>("split");
  const [precision, setPrecision] = useState<Precision>("smart");
  const [ignoreWhitespace, setIgnoreWhitespace] = useState(false);
  const [hideUnchanged, setHideUnchanged] = useState(false);
  const [wrap, setWrap] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);

  const result = useMemo(
    () => buildDiff(leftText, rightText, { precision, ignoreWhitespace }),
    [leftText, rightText, precision, ignoreWhitespace],
  );

  const identical = result.added === 0 && result.removed === 0;

  function jumpToFirstChange() {
    scrollRef.current?.querySelector('[data-change="true"]')?.scrollIntoView({ block: "center", behavior: "smooth" });
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2 rounded-lg border border-border bg-card p-2 text-sm">
        <SegmentedControl
          value={layout}
          onChange={setLayout}
          options={[
            { value: "split", label: "Split" },
            { value: "unified", label: "Unified" },
          ]}
        />
        <SegmentedControl
          value={precision}
          onChange={setPrecision}
          options={[
            { value: "smart", label: "Smart" },
            { value: "word", label: "Word" },
            { value: "char", label: "Char" },
          ]}
        />
        <ToggleField id="diff-ignore-ws" checked={ignoreWhitespace} onCheckedChange={setIgnoreWhitespace} label="Ignore whitespace" />
        <ToggleField id="diff-hide-unchanged" checked={hideUnchanged} onCheckedChange={setHideUnchanged} label="Hide unchanged" />
        <ToggleField id="diff-wrap" checked={wrap} onCheckedChange={setWrap} label="Wrap lines" />
        <Button variant="ghost" size="sm" onClick={jumpToFirstChange} disabled={identical}>
          Go to first change
        </Button>
        {onShare && (
          <Button size="sm" className="ml-auto" onClick={onShare}>
            <Share2 /> Share comparison
          </Button>
        )}
      </div>

      <div className="overflow-hidden rounded-lg border border-border">
        <div className="grid grid-cols-2 divide-x divide-border border-b border-border text-sm">
          <SideHeader name={leftName} stat={`− ${result.removed} removal${result.removed === 1 ? "" : "s"}`} statClass="text-destructive" lines={lineCount(leftText)} text={leftText} />
          <SideHeader name={rightName} stat={`+ ${result.added} addition${result.added === 1 ? "" : "s"}`} statClass="text-success" lines={lineCount(rightText)} text={rightText} />
        </div>
        {identical && <div className="px-4 py-3 text-sm text-muted-foreground">Identical</div>}
        <div ref={scrollRef} className="h-[calc(100vh-320px)] min-h-[360px] overflow-auto">
          {layout === "split" ? (
            <SplitBody rows={result.rows} hideUnchanged={hideUnchanged} wrap={wrap} />
          ) : (
            <UnifiedBody rows={result.rows} hideUnchanged={hideUnchanged} wrap={wrap} />
          )}
        </div>
      </div>
    </div>
  );
}

function SideHeader({
  name,
  stat,
  statClass,
  lines,
  text,
}: {
  name: string;
  stat: string;
  statClass: string;
  lines: number;
  text: string;
}) {
  return (
    <div className="flex items-center justify-between gap-3 px-3 py-2">
      <div className="flex min-w-0 items-center gap-2">
        <span className="truncate font-medium">{name}</span>
        <span className={"shrink-0 text-xs " + statClass}>{stat}</span>
      </div>
      <div className="flex shrink-0 items-center gap-3">
        <span className="text-xs text-muted-foreground">{lines} lines</span>
        <Button variant="ghost" size="sm" onClick={() => void copyText(text, name)}>
          <Copy className="size-3.5" /> Copy
        </Button>
      </div>
    </div>
  );
}

function SplitBody({ rows, hideUnchanged, wrap }: { rows: Row[]; hideUnchanged: boolean; wrap: boolean }) {
  const display = hideUnchanged ? collapseContext(rows, (r) => r.kind === "eq", 3) : rows.map((item) => ({ kind: "item", item }) as const);
  return (
    <div className={"grid font-mono text-sm " + (wrap ? "w-full grid-cols-2" : "w-max min-w-full grid-cols-[max-content_max-content]")}>
      {display.map((d, i) =>
        d.kind === "gap" ? <GapRow key={i} count={d.count} spanCols /> : <SplitCells key={i} row={d.item} wrap={wrap} />,
      )}
    </div>
  );
}

function UnifiedBody({ rows, hideUnchanged, wrap }: { rows: Row[]; hideUnchanged: boolean; wrap: boolean }) {
  const lines = useMemo(() => toUnifiedLines(rows), [rows]);
  const display = hideUnchanged ? collapseContext(lines, (l) => l.kind === "eq", 3) : lines.map((item) => ({ kind: "item", item }) as const);
  return (
    <div className={"font-mono text-sm " + (wrap ? "w-full" : "w-max min-w-full")}>
      {display.map((d, i) =>
        d.kind === "gap" ? <GapRow key={i} count={d.count} spanCols={false} /> : <UnifiedRow key={i} line={d.item} wrap={wrap} />,
      )}
    </div>
  );
}

function SegmentedControl<T extends string>({
  value,
  onChange,
  options,
}: {
  value: T;
  onChange: (v: T) => void;
  options: { value: T; label: string }[];
}) {
  return (
    <div className="flex overflow-hidden rounded-md border border-border">
      {options.map((o, i) => (
        <button
          key={o.value}
          type="button"
          className={
            "px-2.5 py-1 text-xs font-medium transition-colors " +
            (i > 0 ? "border-l border-border " : "") +
            (value === o.value ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:bg-accent hover:text-accent-foreground")
          }
          onClick={() => onChange(o.value)}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

function ToggleField({
  id,
  checked,
  onCheckedChange,
  label,
}: {
  id: string;
  checked: boolean;
  onCheckedChange: (v: boolean) => void;
  label: string;
}) {
  return (
    <div className="flex items-center gap-1.5">
      <Switch id={id} checked={checked} onCheckedChange={onCheckedChange} className="scale-90" />
      <Label htmlFor={id} className="text-xs font-normal text-muted-foreground">
        {label}
      </Label>
    </div>
  );
}
