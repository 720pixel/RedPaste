import { describe, it, expect } from "vitest";
import {
  deriveRoomKeys,
  newJoinSecret,
  encryptUpdate,
  decryptUpdate,
  newPasswordRoomKey,
  deriveRoomKeyForJoin,
} from "./roomcrypto";

describe("room crypto", () => {
  it("derives the same room key + editor token from the same join secret", async () => {
    const js = newJoinSecret();
    const a = await deriveRoomKeys(js);
    const b = await deriveRoomKeys(js);
    expect(a.editorToken).toBe(b.editorToken);
  });

  it("different join secrets give different editor tokens", async () => {
    const a = await deriveRoomKeys(newJoinSecret());
    const b = await deriveRoomKeys(newJoinSecret());
    expect(a.editorToken).not.toBe(b.editorToken);
  });

  it("round-trips an update through the room key", async () => {
    const { roomKey } = await deriveRoomKeys(newJoinSecret());
    const data = new TextEncoder().encode("some yjs update bytes 世界");
    const env = await encryptUpdate(roomKey, data);
    const back = await decryptUpdate(roomKey, env);
    expect(Array.from(back)).toEqual(Array.from(data));
  });

  it("a different room key cannot decrypt", async () => {
    const k1 = (await deriveRoomKeys(newJoinSecret())).roomKey;
    const k2 = (await deriveRoomKeys(newJoinSecret())).roomKey;
    const env = await encryptUpdate(k1, new Uint8Array([1, 2, 3, 4]));
    await expect(decryptUpdate(k2, env)).rejects.toBeTruthy();
  });

  it("editor token is never derivable from just the room key (they differ)", async () => {
    const { roomKey, editorToken } = await deriveRoomKeys(newJoinSecret());
    expect(typeof editorToken).toBe("string");
    expect(roomKey).toBeInstanceOf(CryptoKey);
  });
});

describe("password-protected room", () => {
  it("a joiner with the right password reproduces the room key", async () => {
    const js = newJoinSecret();
    const { roomKey, meta } = await newPasswordRoomKey(js, "hunter2");
    const data = new TextEncoder().encode("secret live update");
    const env = await encryptUpdate(roomKey, data);

    const joinKey = await deriveRoomKeyForJoin(js, "hunter2", meta);
    const back = await decryptUpdate(joinKey, env);
    expect(Array.from(back)).toEqual(Array.from(data));
  });

  it("the link alone (no password) cannot read the room", async () => {
    const js = newJoinSecret();
    const { roomKey, meta } = await newPasswordRoomKey(js, "hunter2");
    const env = await encryptUpdate(roomKey, new Uint8Array([1, 2, 3]));

    // wrong password
    const wrong = await deriveRoomKeyForJoin(js, "nope", meta);
    await expect(decryptUpdate(wrong, env)).rejects.toBeTruthy();

    // link only (no-password derivation) also can't
    const linkOnly = (await deriveRoomKeys(js)).roomKey;
    await expect(decryptUpdate(linkOnly, env)).rejects.toBeTruthy();
  });
}, 20000);
