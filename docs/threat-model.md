# RedPaste threat model

A working document, updated as the design evolves. The point is to be explicit
about what RedPaste defends and, just as importantly, what it doesn't.

## Assets worth protecting

- Paste and live-document plaintext
- Encryption keys and passphrases
- Owner / editor capability tokens
- The server pepper, database credentials, TURN credentials
- Availability and the expiration guarantee

## Who we're worried about

| Adversary | What they can do | What stops them |
|---|---|---|
| Passive network observer | See TLS-wrapped traffic | TLS; content is already ciphertext |
| Malicious operator / DB thief | Read the entire database | Only ciphertext + coarse metadata is stored |
| Malicious recipient | Read/copy anything they have the link for | Out of scope: the link *is* the grant |
| Link-preview / search crawler | Fetch secret URLs | noindex headers, no view consumption on GET/HEAD |
| Bot spammer / storage abuser | Flood creation | Rate limits, body caps, storage quotas |
| Malicious live participant | Replay or flood updates | Nonce discipline, per-editor rate/size caps |
| XSS attacker | Steal fragment keys | Strict CSP, never inject content as HTML |
| Compromised dependency | Ship malicious JS | Pinned deps, SRI-style embedding, minimal footprint |

## Threats we actively design against

- Nonce reuse in AES-GCM → fresh random nonce per static encryption; structured
  session-id||counter nonces for live updates.
- One-time redemption races → the redeem path is a single atomic transaction;
  two racing requests can't both consume the last view.
- Key leakage via logs/referrers → fragments never sent to the server; logs
  redact slugs, auth headers, query strings; `Referrer-Policy: no-referrer`.
- Search indexing of paste URLs → noindex headers + meta, no sitemap entries.
- Preview bots burning one-time pastes → reveal requires an explicit POST.
- Compact-code enumeration → high-entropy codes, generic 410 on miss.

## Assumptions

- The browser and device are trusted at encrypt/decrypt time.
- Whoever holds a valid key may read (and copy) the content.
- A compromised frontend deployment can steal *future* keys.
- TLS termination is configured correctly.
- The crypto primitives behave per spec.

## Known non-goals

We do not claim to prevent screenshots, recover lost keys, guarantee deletion
from recipient devices or old backups, or stop application-layer DoS without the
documented edge protections.
