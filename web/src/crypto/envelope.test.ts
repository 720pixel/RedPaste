import { describe, it, expect } from "vitest";
import { encryptEnvelope, decryptEnvelope, DecryptError, type Envelope } from "./envelope";
import { toB64url, fromB64url } from "./base64";

const sample: Envelope = {
  version: 1,
  type: "static",
  title: "test",
  content: "line one\nlíne two 世界 🌍\nline three",
  language: "go",
  editor: { wrap: false, tabSize: 4 },
  createdAtClient: "2026-07-01T00:00:00Z",
};

describe("base64url", () => {
  it("round-trips arbitrary bytes", () => {
    const b = new Uint8Array([0, 1, 2, 250, 251, 255, 62, 63]);
    expect(Array.from(fromB64url(toB64url(b)))).toEqual(Array.from(b));
  });
  it("has no padding", () => {
    expect(toB64url(new Uint8Array([1, 2, 3, 4, 5]))).not.toContain("=");
  });
});

describe("envelope", () => {
  it("encrypts then decrypts back to the original", async () => {
    const { linkSecret, ciphertext, meta } = await encryptEnvelope(sample);
    const back = await decryptEnvelope(linkSecret, ciphertext, meta);
    expect(back.content).toBe(sample.content);
    expect(back.language).toBe("go");
  });

  it("uses a fresh nonce each time", async () => {
    const a = await encryptEnvelope(sample);
    const b = await encryptEnvelope(sample);
    expect(a.meta.nonce).not.toBe(b.meta.nonce);
    expect(a.linkSecret).not.toBe(b.linkSecret);
  });

  it("fails on the wrong link secret", async () => {
    const { ciphertext, meta } = await encryptEnvelope(sample);
    const wrong = toB64url(crypto.getRandomValues(new Uint8Array(16)));
    await expect(decryptEnvelope(wrong, ciphertext, meta)).rejects.toBeInstanceOf(DecryptError);
  });

  it("rejects an unknown crypto version", async () => {
    const { linkSecret, ciphertext, meta } = await encryptEnvelope(sample);
    await expect(decryptEnvelope(linkSecret, ciphertext, { ...meta, v: 99 })).rejects.toBeInstanceOf(DecryptError);
  });
});

describe("passphrase", () => {
  it("round-trips with the correct passphrase", async () => {
    const { linkSecret, ciphertext, meta } = await encryptEnvelope(sample, "correct horse");
    expect(meta.pass.on).toBe(true);
    expect(meta.pass.salt).toBeTruthy();
    const back = await decryptEnvelope(linkSecret, ciphertext, meta, "correct horse");
    expect(back.content).toBe(sample.content);
  });

  it("needs both link secret and passphrase", async () => {
    const { linkSecret, ciphertext, meta } = await encryptEnvelope(sample, "s3cret");
    // right link, wrong passphrase
    await expect(decryptEnvelope(linkSecret, ciphertext, meta, "wrong")).rejects.toBeInstanceOf(DecryptError);
    // right link, missing passphrase
    await expect(decryptEnvelope(linkSecret, ciphertext, meta)).rejects.toBeInstanceOf(DecryptError);
  });
}, 20000);
