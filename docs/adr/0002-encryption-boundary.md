# ADR 0002: Where encryption happens

**Status:** accepted

## Context

The whole premise is that the server can't read your pastes. That only holds if
encryption happens in the browser and the key never leaves it.

## Decision

- The browser encrypts with **AES-256-GCM**. The content key is derived with
  **HKDF-SHA-256** from a high-entropy link secret.
- The link secret lives in the **URL fragment** (`#...`), which browsers never
  send to the server.
- Optional **passphrases** add an **Argon2id** step (in a Web Worker), mixed into
  the key derivation. The passphrase is never transmitted.
- Capability tokens (owner/editor) are random secrets; the server stores only
  `HMAC-SHA-256(pepper, token)` and compares in constant time.
- Every encrypted object carries a version so we can evolve the format without
  silently reinterpreting old data.

## Consequences

- Lost key or passphrase = unrecoverable. That's the trade for zero-knowledge.
- A compromised frontend could capture future keys; the served JS is trusted at
  encrypt/decrypt time. Documented honestly in SECURITY.md.
- The server never needs to know the language, title, or content; all of that
  rides inside the encrypted envelope.
