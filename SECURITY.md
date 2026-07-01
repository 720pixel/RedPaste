# Security policy

## Reporting a vulnerability

Please report security issues privately. Email **redx7r@gmail.com** with a
description, reproduction steps, and impact. Don't open a public issue for
anything exploitable.

We'll acknowledge within a few days and keep you posted on the fix. Coordinated
disclosure is appreciated; give us a reasonable window before going public.

## What RedPaste protects

RedPaste is zero-knowledge: content is encrypted in the browser before upload,
and the decryption key stays in the URL fragment, which never reaches the server.
A database dump or a malicious operator sees only ciphertext and coarse metadata
(size, timestamps, expiry, view policy).

## What it can't protect against

Be honest with yourself about these:

- Anyone with the full share link can read (or edit) the paste.
- A compromised frontend deployment could steal *future* keys; the served
  JavaScript is trusted at encrypt/decrypt time.
- We can't recover lost keys or forgotten passphrases.
- We can't stop a recipient from screenshotting or copying revealed content.
- We can't guarantee deletion from a recipient's device or from old backups.

## Scope

In scope: the server, the crypto format, capability handling, the API, and the
frontend. Out of scope: the private automation API (not part of the public repo),
social-engineering, and DoS that requires the documented edge protections to be
absent.
