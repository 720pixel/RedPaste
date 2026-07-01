// Pull a paste's plaintext into the diff checker from a full share link. All the
// decryption happens here in the browser; only the ciphertext fetch touches the
// network.

import { getMeta, redeem, resolveShort } from "../api/client";
import { parseFragment } from "../crypto/fragment";
import { decryptEnvelope, PassphraseRequired } from "../crypto/envelope";

export class LoadError extends Error {}

export async function loadFromLink(rawUrl: string, passphrase?: string): Promise<string> {
  let url: URL;
  try {
    url = new URL(rawUrl.trim(), window.location.origin);
  } catch {
    throw new LoadError("That doesn't look like a RedPaste link.");
  }

  const frag = parseFragment(url.hash);
  if (!frag || frag.kind !== "A") {
    throw new LoadError("The link is missing its decryption key (#A…).");
  }

  // resolve /s/{code} to /p/{slug} if needed
  let path = url.pathname;
  const shortMatch = path.match(/^\/s\/([^/]+)$/);
  if (shortMatch) {
    path = await resolveShort(shortMatch[1]).catch(() => {
      throw new LoadError("Short link not found or expired.");
    });
  }
  const slugMatch = path.match(/^\/p\/([^/]+)$/);
  if (!slugMatch) throw new LoadError("Not a paste link.");
  const slug = decodeURIComponent(slugMatch[1]);

  const meta = await getMeta(slug).catch(() => {
    throw new LoadError("Paste unavailable. It may have expired or been deleted.");
  });
  if (meta.passphraseProtected && !passphrase) {
    throw new PassphraseRequired();
  }

  const r = await redeem(slug).catch(() => {
    throw new LoadError("Couldn't fetch the paste.");
  });
  const env = await decryptEnvelope(frag.secret, r.ciphertext, r.crypto, passphrase);
  return env.content;
}
