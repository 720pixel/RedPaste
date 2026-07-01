// Cheap, deterministic language guessing. Runs locally; text never leaves the
// browser for detection. Returns a best guess plus a rough confidence; low
// confidence falls back to plain text.

export interface Guess {
  id: string;
  confidence: number;
}

export function detectLanguage(text: string): Guess {
  const t = text.trim();
  if (t === "") return { id: "plaintext", confidence: 1 };

  // shebang wins outright
  const first = t.split("\n", 1)[0];
  if (first.startsWith("#!")) {
    if (/python/.test(first)) return { id: "python", confidence: 0.95 };
    if (/node/.test(first)) return { id: "javascript", confidence: 0.9 };
    if (/\b(bash|sh|zsh)\b/.test(first)) return { id: "bash", confidence: 0.9 };
  }

  // valid JSON
  if ((t.startsWith("{") || t.startsWith("[")) && tryJSON(t)) {
    return { id: "json", confidence: 0.9 };
  }

  const rules: { id: string; re: RegExp; w: number }[] = [
    { id: "html", re: /<!doctype html|<html[\s>]/i, w: 0.9 },
    { id: "php", re: /<\?php/, w: 0.95 },
    { id: "go", re: /\bpackage\s+\w+|\bfunc\s+\w*\s*\(|:=/, w: 0.75 },
    { id: "rust", re: /\bfn\s+\w+\s*\(|\blet\s+mut\b|::</, w: 0.75 },
    { id: "python", re: /^\s*(def|class)\s+\w+|^\s*import\s+\w+|print\(/m, w: 0.7 },
    { id: "typescript", re: /\binterface\s+\w+|\b:\s*(string|number|boolean)\b|\bimport\s+type\b/, w: 0.7 },
    { id: "javascript", re: /\b(const|let|var)\b|=>|\bfunction\b/, w: 0.55 },
    { id: "java", re: /\bpublic\s+(class|static)\b|System\.out\.print/, w: 0.75 },
    { id: "cpp", re: /#include\s*<|std::/, w: 0.75 },
    { id: "c", re: /#include\s*<\w+\.h>/, w: 0.7 },
    { id: "sql", re: /\bSELECT\b[\s\S]*\bFROM\b|\bINSERT\s+INTO\b/i, w: 0.75 },
    { id: "dockerfile", re: /^FROM\s+\S+/m, w: 0.7 },
    { id: "yaml", re: /^\s*[\w-]+:\s/m, w: 0.4 },
    { id: "css", re: /[.#]?[\w-]+\s*\{[^}]*:[^}]*;/, w: 0.6 },
    { id: "markdown", re: /^#{1,6}\s|\[.+\]\(.+\)/m, w: 0.5 },
    { id: "xml", re: /^<\?xml|<\/[a-z]+>/i, w: 0.6 },
  ];

  let best: Guess = { id: "plaintext", confidence: 0 };
  for (const r of rules) {
    if (r.re.test(t) && r.w > best.confidence) best = { id: r.id, confidence: r.w };
  }
  return best.confidence >= 0.5 ? best : { id: "plaintext", confidence: best.confidence };
}

function tryJSON(s: string): boolean {
  try {
    JSON.parse(s);
    return true;
  } catch {
    return false;
  }
}
