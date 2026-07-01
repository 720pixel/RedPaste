import { useEffect, useRef, useState } from "react";
import { EditorState, Compartment, type Extension } from "@codemirror/state";
import { EditorView, keymap, lineNumbers, highlightActiveLine, highlightActiveLineGutter } from "@codemirror/view";
import { defaultKeymap, history, historyKeymap, indentWithTab } from "@codemirror/commands";
import { searchKeymap, highlightSelectionMatches } from "@codemirror/search";
import { syntaxHighlighting, defaultHighlightStyle, bracketMatching, foldGutter, indentOnInput } from "@codemirror/language";
import { oneDark } from "@codemirror/theme-one-dark";
import { loadLanguage } from "./languages";

export interface EditorProps {
  value: string;
  language: string;
  readOnly?: boolean;
  wrap?: boolean;
  big?: boolean; // tall min-height for the editing/live surface
  onChange?: (v: string) => void;
  onLineCount?: (n: number) => void;
}

const isDark = () => document.documentElement.getAttribute("data-theme") === "dark";

export function CodeMirror({ value, language, readOnly, wrap, big, onChange, onLineCount }: EditorProps) {
  const host = useRef<HTMLDivElement>(null);
  const view = useRef<EditorView | null>(null);
  const langComp = useRef(new Compartment());
  const themeComp = useRef(new Compartment());
  const wrapComp = useRef(new Compartment());
  const [ready, setReady] = useState(false);

  useEffect(() => {
    if (!host.current) return;

    const base: Extension[] = [
      lineNumbers(),
      highlightSelectionMatches(),
      syntaxHighlighting(defaultHighlightStyle, { fallback: true }),
      keymap.of([...defaultKeymap, ...historyKeymap, ...searchKeymap, indentWithTab]),
      // Editor-only chrome; a read-only viewer stays clean and calm.
      ...(readOnly ? [] : [foldGutter(), history(), bracketMatching(), indentOnInput(), highlightActiveLine(), highlightActiveLineGutter()]),
      EditorView.updateListener.of((u) => {
        if (u.docChanged) {
          onChange?.(u.state.doc.toString());
          onLineCount?.(u.state.doc.lines);
        }
      }),
      EditorView.editable.of(!readOnly),
      EditorState.readOnly.of(!!readOnly),
      themeComp.current.of(isDark() ? oneDark : []),
      langComp.current.of([]),
      wrapComp.current.of(wrap ? EditorView.lineWrapping : []),
    ];

    const state = EditorState.create({ doc: value, extensions: base });
    const v = new EditorView({ state, parent: host.current });
    view.current = v;
    onLineCount?.(state.doc.lines);
    if (!readOnly) v.focus();
    setReady(true);

    const obs = new MutationObserver(() =>
      v.dispatch({ effects: themeComp.current.reconfigure(isDark() ? oneDark : []) }),
    );
    obs.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });

    return () => {
      obs.disconnect();
      v.destroy();
      view.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    if (!ready) return;
    let cancelled = false;
    loadLanguage(language).then((ext) => {
      if (!cancelled) view.current?.dispatch({ effects: langComp.current.reconfigure(ext) });
    });
    return () => {
      cancelled = true;
    };
  }, [language, ready]);

  useEffect(() => {
    const v = view.current;
    if (!v || !ready) return;
    if (v.state.doc.toString() !== value) {
      v.dispatch({ changes: { from: 0, to: v.state.doc.length, insert: value } });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [value, ready]);

  useEffect(() => {
    if (!ready) return;
    view.current?.dispatch({ effects: wrapComp.current.reconfigure(wrap ? EditorView.lineWrapping : []) });
  }, [wrap, ready]);

  return <div className={"cm-host" + (big ? " cm-host-big" : "")} ref={host} />;
}
