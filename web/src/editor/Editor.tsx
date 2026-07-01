import { Suspense, lazy } from "react";
import type { EditorProps } from "./CodeMirror";

// CodeMirror is heavy, so load it on demand. While it loads we show the content
// immediately in a matching <pre> so the paste is readable with no wait; the
// highlighter then swaps in seamlessly.
const CodeMirror = lazy(() => import("./CodeMirror").then((m) => ({ default: m.CodeMirror })));

export function Editor(props: EditorProps) {
  return (
    <Suspense fallback={<InstantCode value={props.value} wrap={props.wrap} />}>
      <CodeMirror {...props} />
    </Suspense>
  );
}

function InstantCode({ value, wrap }: { value: string; wrap?: boolean }) {
  const lines = value === "" ? 1 : value.split("\n").length;
  return (
    <div className="flex">
      <div aria-hidden className="select-none border-r border-border py-2 pl-3.5 pr-3 text-right font-mono text-[13.5px] leading-[1.6] text-muted-foreground/70">
        {Array.from({ length: lines }, (_, i) => (
          <div key={i}>{i + 1}</div>
        ))}
      </div>
      <pre className={"code-instant flex-1" + (wrap ? " wrap" : "")}>{value}</pre>
    </div>
  );
}
