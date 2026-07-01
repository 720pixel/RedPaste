// Live-room key material, all derived in the browser from the join secret that
// rides in the URL fragment. K_room (encrypts updates) never touches the server;
// only the derived editor capability is sent, and only as a hash the server keeps.

import { toB64url, fromB64url } from "../crypto/base64";

const enc = new TextEncoder();
const ROOM_KEY_INFO = "redpaste/live/v1/room-key";
const EDITOR_CAP_INFO = "redpaste/live/v1/editor-capability";

function buf(u: Uint8Array): ArrayBuffer {
  return u.slice().buffer;
}

function randomBytes(n: number): Uint8Array {
  const b = new Uint8Array(n);
  crypto.getRandomValues(b);
  return b;
}

export interface RoomKeys {
  roomKey: CryptoKey; // AES-256-GCM
  editorToken: string; // base64url of raw T_editor bytes
}

async function hkdf(ikm: Uint8Array, info: string): Promise<ArrayBuffer> {
  const key = await crypto.subtle.importKey("raw", buf(ikm), "HKDF", false, ["deriveBits"]);
  return crypto.subtle.deriveBits(
    { name: "HKDF", hash: "SHA-256", salt: new Uint8Array(0), info: enc.encode(info) },
    key,
    256,
  );
}

// Optional room password. When on, K_room also folds in an Argon2id passphrase,
// so holding the link alone is not enough to read the room.
export interface LivePassMeta {
  on: boolean;
  salt?: string; // base64url argon2 salt
  mem?: number;
  iter?: number;
  par?: number;
}

function concat(a: Uint8Array, b: Uint8Array): Uint8Array {
  const out = new Uint8Array(a.length + b.length);
  out.set(a, 0);
  out.set(b, a.length);
  return out;
}

async function importRoomKey(ikm: Uint8Array): Promise<CryptoKey> {
  const roomRaw = await hkdf(ikm, ROOM_KEY_INFO);
  return crypto.subtle.importKey("raw", roomRaw, { name: "AES-GCM", length: 256 }, false, ["encrypt", "decrypt"]);
}

// The editor capability comes from the join secret only (it is what the server
// authenticates), independent of any password.
export async function deriveEditorToken(joinSecretB64: string): Promise<string> {
  const editorRaw = await hkdf(fromB64url(joinSecretB64), EDITOR_CAP_INFO);
  return toB64url(new Uint8Array(editorRaw));
}

// No-password rooms: both keys straight from the join secret.
export async function deriveRoomKeys(joinSecretB64: string): Promise<RoomKeys> {
  const joinSecret = fromB64url(joinSecretB64);
  const roomKey = await importRoomKey(joinSecret);
  return { roomKey, editorToken: await deriveEditorToken(joinSecretB64) };
}

// Creating a password-protected room: generate the argon params and the room key.
export async function newPasswordRoomKey(joinSecretB64: string, passphrase: string): Promise<{ roomKey: CryptoKey; meta: LivePassMeta }> {
  const { deriveKPass, DEFAULT_PARAMS } = await import("../crypto/passphrase");
  const salt = toB64url(randomBytes(16));
  const kpass = await deriveKPass(passphrase, salt, DEFAULT_PARAMS);
  const roomKey = await importRoomKey(concat(fromB64url(joinSecretB64), kpass));
  return { roomKey, meta: { on: true, salt, mem: DEFAULT_PARAMS.mem, iter: DEFAULT_PARAMS.iter, par: DEFAULT_PARAMS.par } };
}

// Joining a password room: reproduce the room key from the link + the password.
export async function deriveRoomKeyForJoin(joinSecretB64: string, passphrase: string, meta: LivePassMeta): Promise<CryptoKey> {
  const { deriveKPass, paramsInBounds } = await import("../crypto/passphrase");
  const params = { mem: meta.mem ?? 0, iter: meta.iter ?? 0, par: meta.par ?? 0 };
  if (!meta.salt || !paramsInBounds(params)) throw new Error("bad room password parameters");
  const kpass = await deriveKPass(passphrase, meta.salt, params);
  return importRoomKey(concat(fromB64url(joinSecretB64), kpass));
}

export function newJoinSecret(): string {
  return toB64url(randomBytes(24)); // 192-bit
}
export function newOwnerToken(): string {
  return toB64url(randomBytes(24));
}

// envelope = [version=1][nonce:12][ciphertext]; base64url for transport. The
// server stores/relays this opaquely and can't read it.
export async function encryptUpdate(key: CryptoKey, data: Uint8Array): Promise<string> {
  const nonce = randomBytes(12);
  const ct = new Uint8Array(await crypto.subtle.encrypt({ name: "AES-GCM", iv: buf(nonce) }, key, buf(data)));
  const env = new Uint8Array(1 + 12 + ct.length);
  env[0] = 1;
  env.set(nonce, 1);
  env.set(ct, 13);
  return toB64url(env);
}

export async function decryptUpdate(key: CryptoKey, envB64: string): Promise<Uint8Array> {
  const env = fromB64url(envB64);
  if (env.length < 14 || env[0] !== 1) throw new Error("bad update envelope");
  const nonce = env.subarray(1, 13);
  const ct = env.subarray(13);
  const pt = await crypto.subtle.decrypt({ name: "AES-GCM", iv: buf(nonce) }, key, buf(ct));
  return new Uint8Array(pt);
}
