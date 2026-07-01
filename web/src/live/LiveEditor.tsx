import { useEffect, useRef } from "react";
import type * as Y from "yjs";
import type { Awareness } from "y-protocols/awareness";
import { EditorState, Compartment, type Extension } from "@codemirror/state";
import { EditorView, keymap, lineNumbers, highlightActiveLine } from "@codemirror/view";
import { defaultKeymap, history, historyKeymap, indentWithTab } from "@codemirror/commands";
import { searchKeymap } from "@codemirror/search";
import { syntaxHighlighting, defaultHighlightStyle, bracketMatching, indentOnInput } from "@codemirror/language";
import { oneDark } from "@codemirror/theme-one-dark";
import { yCollab } from "y-codemirror.next";
import { loadLanguage } from "../editor/languages";

interface Props {
  ytext: Y.Text;
  awareness: Awareness;
  language: string;
  wrap: boolean;
  onLineCount?: (n: number) => void;
}

const isDark = () => document.documentElement.getAttribute("data-theme") === "dark";

export function LiveEditor({ ytext, awareness, language, wrap, onLineCount }: Props) {
  const host = useRef<HTMLDivElement>(null);
  const view = useRef<EditorView | null>(null);
  const langComp = useRef(new Compartment());
  const themeComp = useRef(new Compartment());
  const wrapComp = useRef(new Compartment());

  useEffect(() => {
    if (!host.current) return;
    const base: Extension[] = [
      lineNumbers(),
      history(),
      bracketMatching(),
      indentOnInput(),
      highlightActiveLine(),
      syntaxHighlighting(defaultHighlightStyle, { fallback: true }),
      keymap.of([...defaultKeymap, ...historyKeymap, ...searchKeymap, indentWithTab]),
      yCollab(ytext, awareness),
      EditorView.updateListener.of((u) => {
        if (u.docChanged) onLineCount?.(u.state.doc.lines);
      }),
      themeComp.current.of(isDark() ? oneDark : []),
      langComp.current.of([]),
      wrapComp.current.of(wrap ? EditorView.lineWrapping : []),
    ];
    const v = new EditorView({ state: EditorState.create({ doc: ytext.toString(), extensions: base }), parent: host.current });
    view.current = v;
    onLineCount?.(v.state.doc.lines);

    const obs = new MutationObserver(() => v.dispatch({ effects: themeComp.current.reconfigure(isDark() ? oneDark : []) }));
    obs.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
    return () => {
      obs.disconnect();
      v.destroy();
      view.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    let cancelled = false;
    loadLanguage(language).then((ext) => {
      if (!cancelled) view.current?.dispatch({ effects: langComp.current.reconfigure(ext) });
    });
    return () => {
      cancelled = true;
    };
  }, [language]);

  useEffect(() => {
    view.current?.dispatch({ effects: wrapComp.current.reconfigure(wrap ? EditorView.lineWrapping : []) });
  }, [wrap]);

  return <div className="cm-host cm-host-big" ref={host} />;
}
