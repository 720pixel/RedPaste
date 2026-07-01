// Mirrors the server's REDPASTE_MAX_PASTE_BYTES default (internal/config/config.go).
// This is a UX hint and early guard only — the server is the source of truth and
// re-enforces the real limit on the encrypted ciphertext size.
import { utf8Encode } from "@/crypto/base64";

export const MAX_PASTE_BYTES = 3 * 1024 * 1024; // 3 MiB

// Average bytes per line assumption for the "~N lines" hint; real content varies.
const ASSUMED_BYTES_PER_LINE = 80;

export function approxMaxLines(): number {
  return Math.floor(MAX_PASTE_BYTES / ASSUMED_BYTES_PER_LINE);
}

export function contentByteLength(content: string): number {
  return utf8Encode(content).length;
}

export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}
