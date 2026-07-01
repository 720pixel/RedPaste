// The zero-knowledge core. Everything here runs in the browser; the link secret
// and content key never leave it. The server only ever sees ciphertext plus the
// public metadata (nonce, salt).
//
// key derivation: random 128-bit link secret -> HKDF-SHA-256 -> 256-bit AES key.
// The link secret rides in the URL fragment; the salt is public.

import { toB64url, fromB64url, utf8Encode, utf8Decode } from "./base64";
// passphrase.ts pulls in the Argon2 WASM; it's imported lazily below so it only
// ships to users who actually use a passphrase.

export const CRYPTO_VERSION = 1;
const HKDF_INFO = "redpaste/static/v1/content";

// what actually gets encrypted: language, title, editor prefs, all of it.
// "diff" pastes hold a JSON-encoded DiffPayload (see routes/Diff.tsx) instead
// of plain text; the viewer branches on `type` to render the right UI.
export interface Envelope {
  version: number;
  type: "static" | "diff";
  title?: string;
  content: string;
  language: string;
  editor: { wrap: boolean; tabSize: number };
  createdAtClient: string;
}

// public crypto params the server stores and hands back. When pass.on is true,
// the content key also depends on an Argon2id passphrase, so the salt/params
// needed to reproduce it live here (never the passphrase itself).
export interface PassMeta {
  on: boolean;
  salt?: string; // base64url argon2 salt
  mem?: number;
  iter?: number;
  par?: number;
}

export interface CryptoMeta {
  v: number;
  alg: "AES-256-GCM";
  kdf: "HKDF-SHA-256";
  nonce: string; // base64url
  salt: string; // base64url, HKDF salt
  pass: PassMeta;
}

export interface EncryptResult {
  linkSecret: string; // base64url, goes in the fragment, never to the server
  ciphertext: Uint8Array;
  meta: CryptoMeta;
}

function randomBytes(n: number): Uint8Array {
  const b = new Uint8Array(n);
  crypto.getRandomValues(b);
  return b;
}

// TS 5.7 types Uint8Array as generic over ArrayBufferLike, which Web Crypto's
// BufferSource won't accept. Copying into a fresh ArrayBuffer-backed view fixes
// the types and costs nothing at our sizes.
function buf(u: Uint8Array): ArrayBuffer {
  return u.slice().buffer;
}

// content key = HKDF(ikm, hkdfSalt). Without a passphrase ikm is just the link
// secret; with one it's linkSecret || Argon2id(passphrase).
async function deriveKey(ikm: Uint8Array, hkdfSalt: Uint8Array): Promise<CryptoKey> {
  const keyMaterial = await crypto.subtle.importKey("raw", buf(ikm), "HKDF", false, ["deriveKey"]);
  return crypto.subtle.deriveKey(
    { name: "HKDF", hash: "SHA-256", salt: buf(hkdfSalt), info: buf(utf8Encode(HKDF_INFO)) },
    keyMaterial,
    { name: "AES-GCM", length: 256 },
    false,
    ["encrypt", "decrypt"],
  );
}

function concat(a: Uint8Array, b: Uint8Array): Uint8Array {
  const out = new Uint8Array(a.length + b.length);
  out.set(a, 0);
  out.set(b, a.length);
  return out;
}

// optional passphrase: when set, we stretch it with Argon2id and fold the result
// into the key material. The passphrase and derived bytes never leave the browser.
export async function encryptEnvelope(env: Envelope, passphrase?: string): Promise<EncryptResult> {
  const linkSecret = randomBytes(16); // 128 bits
  const salt = randomBytes(16);
  const nonce = randomBytes(12); // fresh per encryption, never reused

  let ikm = linkSecret;
  let passMeta: PassMeta = { on: false };
  if (passphrase && passphrase.length > 0) {
    const { deriveKPass, DEFAULT_PARAMS } = await import("./passphrase");
    const argonSalt = randomBytes(16);
    const argonSaltB64 = toB64url(argonSalt);
    const kpass = await deriveKPass(passphrase, argonSaltB64, DEFAULT_PARAMS);
    ikm = concat(linkSecret, kpass);
    passMeta = { on: true, salt: argonSaltB64, mem: DEFAULT_PARAMS.mem, iter: DEFAULT_PARAMS.iter, par: DEFAULT_PARAMS.par };
  }

  const key = await deriveKey(ikm, salt);
  const pt = utf8Encode(JSON.stringify(env));
  const ct = new Uint8Array(await crypto.subtle.encrypt({ name: "AES-GCM", iv: buf(nonce) }, key, buf(pt)));

  return {
    linkSecret: toB64url(linkSecret),
    ciphertext: ct,
    meta: {
      v: CRYPTO_VERSION,
      alg: "AES-256-GCM",
      kdf: "HKDF-SHA-256",
      nonce: toB64url(nonce),
      salt: toB64url(salt),
      pass: passMeta,
    },
  };
}

export class DecryptError extends Error {}
export class PassphraseRequired extends DecryptError {
  constructor() {
    super("passphrase required");
  }
}

export async function decryptEnvelope(
  linkSecretB64: string,
  ciphertext: Uint8Array,
  meta: CryptoMeta,
  passphrase?: string,
): Promise<Envelope> {
  if (meta.v !== CRYPTO_VERSION) {
    throw new DecryptError("unsupported crypto version");
  }

  let ikm = fromB64url(linkSecretB64);
  if (meta.pass.on) {
    if (!passphrase) throw new PassphraseRequired();
    const { deriveKPass, paramsInBounds } = await import("./passphrase");
    const params = { mem: meta.pass.mem ?? 0, iter: meta.pass.iter ?? 0, par: meta.pass.par ?? 0 };
    if (!meta.pass.salt || !paramsInBounds(params)) {
      throw new DecryptError("bad passphrase parameters");
    }
    const kpass = await deriveKPass(passphrase, meta.pass.salt, params);
    ikm = concat(ikm, kpass);
  }

  let env: Envelope;
  try {
    const key = await deriveKey(ikm, fromB64url(meta.salt));
    const pt = await crypto.subtle.decrypt(
      { name: "AES-GCM", iv: buf(fromB64url(meta.nonce)) },
      key,
      buf(ciphertext),
    );
    env = JSON.parse(utf8Decode(new Uint8Array(pt)));
  } catch {
    // wrong key, wrong passphrase, tampered ciphertext, or a swapped row
    throw new DecryptError("could not decrypt: wrong key or passphrase");
  }
  return env;
}

// owner capability: a fat random token. The server stores only HMAC(pepper, token).
export function newOwnerToken(): string {
  return toB64url(randomBytes(24)); // 192 bits
}
