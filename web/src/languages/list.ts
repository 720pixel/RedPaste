// The language menu. In Phase 3 these gain real CodeMirror grammars loaded on
// demand; for now it's just the identifier we store (encrypted) with the paste.

export interface Language {
  id: string;
  label: string;
}

export const LANGUAGES: Language[] = [
  { id: "plaintext", label: "Plain text" },
  { id: "markdown", label: "Markdown" },
  { id: "json", label: "JSON" },
  { id: "yaml", label: "YAML" },
  { id: "toml", label: "TOML" },
  { id: "ini", label: "INI" },
  { id: "xml", label: "XML" },
  { id: "html", label: "HTML" },
  { id: "css", label: "CSS" },
  { id: "scss", label: "SCSS" },
  { id: "javascript", label: "JavaScript" },
  { id: "typescript", label: "TypeScript" },
  { id: "jsx", label: "JSX" },
  { id: "tsx", label: "TSX" },
  { id: "python", label: "Python" },
  { id: "go", label: "Go" },
  { id: "rust", label: "Rust" },
  { id: "c", label: "C" },
  { id: "cpp", label: "C++" },
  { id: "csharp", label: "C#" },
  { id: "java", label: "Java" },
  { id: "kotlin", label: "Kotlin" },
  { id: "swift", label: "Swift" },
  { id: "php", label: "PHP" },
  { id: "ruby", label: "Ruby" },
  { id: "bash", label: "Bash" },
  { id: "powershell", label: "PowerShell" },
  { id: "sql", label: "SQL" },
  { id: "dockerfile", label: "Dockerfile" },
  { id: "nginx", label: "Nginx" },
  { id: "diff", label: "Diff" },
];

export function labelFor(id: string): string {
  return LANGUAGES.find((l) => l.id === id)?.label ?? id;
}
