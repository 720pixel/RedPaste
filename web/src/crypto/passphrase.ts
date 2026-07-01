// Argon2id passphrase stretching. Runs entirely in the browser via WASM; the
// passphrase never leaves the page. Params are bounded so a hostile page can't
// ask the browser to burn gigabytes of memory.

import { argon2id } from "hash-wasm";
import { fromB64url, utf8Encode } from "./base64";

export interface Argon2Params {
  mem: number; // memory in KiB
  iter: number;
  par: number; // parallelism
}

// Calibrated defaults (RFC 9106 second recommended option: 64 MiB, t=3).
export const DEFAULT_PARAMS: Argon2Params = { mem: 65536, iter: 3, par: 1 };

// Guard rails: reject anything outside this on decrypt so a malicious paste
// can't exhaust the viewer's memory.
const MIN = { mem: 8192, iter: 1, par: 1 };
const MAX = { mem: 262144, iter: 10, par: 4 };

export function paramsInBounds(p: Argon2Params): boolean {
  return (
    p.mem >= MIN.mem && p.mem <= MAX.mem &&
    p.iter >= MIN.iter && p.iter <= MAX.iter &&
    p.par >= MIN.par && p.par <= MAX.par
  );
}

// Derive the 32-byte passphrase key that gets mixed into the content key.
export async function deriveKPass(
  passphrase: string,
  saltB64: string,
  params: Argon2Params,
): Promise<Uint8Array> {
  const hash = await argon2id({
    password: utf8Encode(passphrase),
    salt: fromB64url(saltB64),
    parallelism: params.par,
    iterations: params.iter,
    memorySize: params.mem,
    hashLength: 32,
    outputType: "binary",
  });
  return hash;
}
