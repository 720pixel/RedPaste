// Thin wrapper over the browser API. Only ciphertext and opaque metadata cross
// this boundary.

import type { CryptoMeta } from "../crypto/envelope";
import { toB64url, fromB64url } from "../crypto/base64";

export interface CreatePayload {
  ciphertext: Uint8Array;
  meta: CryptoMeta;
  expiresInSeconds: number;
  burnAfterRead: boolean;
  maxViews: number | null;
  ownerToken: string;
  shortLink: boolean;
  kind?: string;
}

export interface CreateResult {
  slug: string;
  urlBase: string;
  shortUrlBase: string | null;
  expiresAt: string;
}

export interface PasteMeta {
  slug: string;
  available: boolean;
  requiresExplicitReveal: boolean;
  passphraseProtected: boolean;
  expiresAt: string;
  crypto: CryptoMeta;
}

export interface Revealed {
  ciphertext: Uint8Array;
  crypto: CryptoMeta;
  expiresAt: string;
}

export class ApiError extends Error {
  constructor(public status: number, public code: string) {
    super(code);
  }
}

async function readError(res: Response): Promise<never> {
  let code = "error";
  try {
    const body = await res.json();
    if (body?.error) code = body.error;
  } catch {
    /* ignore */
  }
  throw new ApiError(res.status, code);
}

export async function createPaste(p: CreatePayload): Promise<CreateResult> {
  const res = await fetch("/api/v1/pastes", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      ciphertext: toB64url(p.ciphertext),
      cryptoMetadata: p.meta,
      policy: {
        expiresInSeconds: p.expiresInSeconds,
        burnAfterRead: p.burnAfterRead,
        maxViews: p.maxViews,
      },
      ownerToken: p.ownerToken,
      shortLink: p.shortLink,
      kind: p.kind,
    }),
  });
  if (!res.ok) await readError(res);
  return res.json();
}

export async function getMeta(slug: string): Promise<PasteMeta> {
  const res = await fetch(`/api/v1/pastes/${encodeURIComponent(slug)}`);
  if (!res.ok) await readError(res);
  return res.json();
}

export async function redeem(slug: string): Promise<Revealed> {
  const res = await fetch(`/api/v1/pastes/${encodeURIComponent(slug)}/redeem`, { method: "POST" });
  if (!res.ok) await readError(res);
  const body = await res.json();
  return { ciphertext: fromB64url(body.ciphertext), crypto: body.crypto, expiresAt: body.expiresAt };
}

export async function resolveShort(code: string): Promise<string> {
  const res = await fetch(`/api/v1/shortlinks/${encodeURIComponent(code)}`);
  if (!res.ok) await readError(res);
  const body = await res.json();
  return body.path as string;
}

export interface OwnerStatus {
  slug: string;
  createdAt: string;
  expiresAt: string;
  burnAfterRead: boolean;
  maxViews: number | null;
  successfulViews: number;
  lastViewedAt: string | null;
  state: string;
}

export async function getStatus(slug: string, ownerToken: string): Promise<OwnerStatus> {
  const res = await fetch(`/api/v1/pastes/${encodeURIComponent(slug)}/status`, {
    headers: { Authorization: `Bearer ${ownerToken}` },
  });
  if (!res.ok) await readError(res);
  return res.json();
}

export async function deletePaste(slug: string, ownerToken: string): Promise<void> {
  const res = await fetch(`/api/v1/pastes/${encodeURIComponent(slug)}`, {
    method: "DELETE",
    headers: { Authorization: `Bearer ${ownerToken}` },
  });
  if (!res.ok && res.status !== 204) await readError(res);
}
