// Theme controller. /theme-init.js sets the initial theme before first paint to
// avoid a flash; this module handles changes and persistence after that.

export type ThemePref = "system" | "light" | "dark";

const KEY = "redpaste-theme";

export function getThemePref(): ThemePref {
  const v = localStorage.getItem(KEY);
  return v === "light" || v === "dark" || v === "system" ? v : "system";
}

function systemDark(): boolean {
  return window.matchMedia?.("(prefers-color-scheme: dark)").matches ?? false;
}

export function resolvedDark(pref: ThemePref): boolean {
  return pref === "dark" || (pref === "system" && systemDark());
}

export function applyTheme(pref: ThemePref): void {
  const dark = resolvedDark(pref);
  const root = document.documentElement;
  root.setAttribute("data-theme", dark ? "dark" : "light");
  root.style.colorScheme = dark ? "dark" : "light";
}

export function setThemePref(pref: ThemePref): void {
  localStorage.setItem(KEY, pref);
  applyTheme(pref);
}

// Follow OS light/dark changes while the pref is "system".
export function watchSystemTheme(onChange: () => void): () => void {
  const mq = window.matchMedia("(prefers-color-scheme: dark)");
  const handler = () => {
    if (getThemePref() === "system") onChange();
  };
  mq.addEventListener("change", handler);
  return () => mq.removeEventListener("change", handler);
}
