// Lazy language loaders for CodeMirror. Each grammar is a separate dynamic
// import so a paste only pulls the one parser it needs; nothing else ships in
// the initial bundle.

import type { Extension } from "@codemirror/state";

type Loader = () => Promise<Extension>;

async function legacy(mod: string, name: string): Promise<Extension> {
  const { StreamLanguage } = await import("@codemirror/language");
  const m = (await import(`@codemirror/legacy-modes/mode/${mod}`)) as Record<string, never>;
  return StreamLanguage.define(m[name]);
}

const loaders: Record<string, Loader> = {
  plaintext: async () => [],
  markdown: async () => (await import("@codemirror/lang-markdown")).markdown(),
  json: async () => (await import("@codemirror/lang-json")).json(),
  yaml: async () => (await import("@codemirror/lang-yaml")).yaml(),
  xml: async () => (await import("@codemirror/lang-xml")).xml(),
  html: async () => (await import("@codemirror/lang-html")).html(),
  css: async () => (await import("@codemirror/lang-css")).css(),
  javascript: async () => (await import("@codemirror/lang-javascript")).javascript(),
  jsx: async () => (await import("@codemirror/lang-javascript")).javascript({ jsx: true }),
  typescript: async () => (await import("@codemirror/lang-javascript")).javascript({ typescript: true }),
  tsx: async () => (await import("@codemirror/lang-javascript")).javascript({ jsx: true, typescript: true }),
  python: async () => (await import("@codemirror/lang-python")).python(),
  go: async () => (await import("@codemirror/lang-go")).go(),
  rust: async () => (await import("@codemirror/lang-rust")).rust(),
  c: async () => (await import("@codemirror/lang-cpp")).cpp(),
  cpp: async () => (await import("@codemirror/lang-cpp")).cpp(),
  java: async () => (await import("@codemirror/lang-java")).java(),
  php: async () => (await import("@codemirror/lang-php")).php(),
  sql: async () => (await import("@codemirror/lang-sql")).sql(),

  csharp: () => legacy("clike", "csharp"),
  kotlin: () => legacy("clike", "kotlin"),
  scss: () => legacy("css", "sCSS"),
  swift: () => legacy("swift", "swift"),
  ruby: () => legacy("ruby", "ruby"),
  bash: () => legacy("shell", "shell"),
  powershell: () => legacy("powershell", "powerShell"),
  toml: () => legacy("toml", "toml"),
  ini: () => legacy("properties", "properties"),
  dockerfile: () => legacy("dockerfile", "dockerFile"),
  nginx: () => legacy("nginx", "nginx"),
  diff: () => legacy("diff", "diff"),
};

export async function loadLanguage(id: string): Promise<Extension> {
  const loader = loaders[id] ?? loaders.plaintext;
  try {
    return await loader();
  } catch {
    return [];
  }
}
