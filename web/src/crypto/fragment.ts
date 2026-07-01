// URL fragment carries the secret. Prefix says what kind:
//   A = static access secret, O = owner capability, L = live join secret.
// We read window.location.hash directly and never let it near the network.

export type FragmentKind = "A" | "O" | "L";

export interface Fragment {
  kind: FragmentKind;
  secret: string; // base64url, still encoded
}

export function parseFragment(hash: string): Fragment | null {
  const h = hash.startsWith("#") ? hash.slice(1) : hash;
  if (h.length < 2) return null;
  const kind = h[0] as FragmentKind;
  if (kind !== "A" && kind !== "O" && kind !== "L") return null;
  const secret = h.slice(1);
  if (!/^[A-Za-z0-9_-]+$/.test(secret)) return null;
  return { kind, secret };
}

export function buildFragment(kind: FragmentKind, secret: string): string {
  return "#" + kind + secret;
}

export function currentFragment(): Fragment | null {
  return parseFragment(window.location.hash);
}
