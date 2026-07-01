// REST calls for live rooms. Ciphertext-only, same as the static API.

import { ApiError } from "../api/client";
import { toB64url, fromB64url } from "../crypto/base64";

async function readError(res: Response): Promise<never> {
  let code = "error";
  try {
    const b = await res.json();
    if (b?.error) code = b.error;
  } catch {
    /* ignore */
  }
  throw new ApiError(res.status, code);
}

export interface CreateRoomInput {
  editorToken: string;
  ownerToken: string;
  cryptoMetadata: unknown;
  snapshot: Uint8Array | null; // encrypted envelope bytes
  expiresInSeconds: number;
  shortLink: boolean;
}

export interface CreateRoomResult {
  slug: string;
  urlBase: string;
  shortUrlBase: string | null;
  expiresAt: string;
}

export async function createRoom(input: CreateRoomInput): Promise<CreateRoomResult> {
  const res = await fetch("/api/v1/live", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      editorToken: input.editorToken,
      ownerToken: input.ownerToken,
      cryptoMetadata: input.cryptoMetadata,
      snapshot: input.snapshot ? toB64url(input.snapshot) : null,
      expiresInSeconds: input.expiresInSeconds,
      shortLink: input.shortLink,
    }),
  });
  if (!res.ok) await readError(res);
  return res.json();
}

export interface Bootstrap {
  state: "active" | "frozen";
  snapshot: { seq: number; ciphertext: string } | null;
  updates: { seq: number; ciphertext: string }[];
  expiresAt: string;
  maxParticipants: number;
  crypto?: { v?: number; alg?: string; pass?: import("./roomcrypto").LivePassMeta };
}

export async function bootstrap(slug: string, editorToken: string): Promise<Bootstrap> {
  const res = await fetch(`/api/v1/live/${encodeURIComponent(slug)}/bootstrap`, {
    headers: { Authorization: `Bearer ${editorToken}` },
  });
  if (!res.ok) await readError(res);
  return res.json();
}

export async function saveSnapshot(
  slug: string,
  editorToken: string,
  ciphertext: Uint8Array,
  baseSeq: number,
): Promise<void> {
  const res = await fetch(`/api/v1/live/${encodeURIComponent(slug)}/snapshot`, {
    method: "POST",
    headers: { "Content-Type": "application/json", Authorization: `Bearer ${editorToken}` },
    body: JSON.stringify({ ciphertext: toB64url(ciphertext), baseSeq }),
  });
  if (!res.ok) await readError(res);
}

export async function freezeRoom(slug: string, ownerToken: string): Promise<void> {
  const res = await fetch(`/api/v1/live/${encodeURIComponent(slug)}/freeze`, {
    method: "POST",
    headers: { Authorization: `Bearer ${ownerToken}` },
  });
  if (!res.ok) await readError(res);
}

export async function deleteRoom(slug: string, ownerToken: string): Promise<void> {
  const res = await fetch(`/api/v1/live/${encodeURIComponent(slug)}`, {
    method: "DELETE",
    headers: { Authorization: `Bearer ${ownerToken}` },
  });
  if (!res.ok && res.status !== 204) await readError(res);
}

// re-exported for the provider
export { fromB64url };
