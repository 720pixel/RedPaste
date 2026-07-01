// The indexable info pages: Security, Privacy, and the 404.

import { Button } from "@/components/ui/button";

export function Security() {
  return (
    <article className="mx-auto max-w-2xl space-y-4">
      <h1 className="text-3xl font-bold">Security</h1>
      <p className="text-muted-foreground">
        RedPaste encrypts paste content on your device before upload. The server
        stores encrypted data and does not receive the decryption secret
        contained in the complete share link. Anyone with that complete link may
        be able to read or edit the paste according to its access type.
      </p>
      <p className="text-muted-foreground">
        RedPaste cannot recover lost keys or passphrases, prevent screenshots, or
        remove copies already saved by recipients.
      </p>
      <h2 className="pt-2 text-xl font-semibold">How it works</h2>
      <ul className="list-disc space-y-1.5 pl-5 text-muted-foreground">
        <li>Content is encrypted with AES-256-GCM using a key derived in your browser.</li>
        <li>
          The key lives only in the URL fragment (after{" "}
          <code className="rounded bg-muted px-1 py-0.5 font-mono text-sm text-foreground">#</code>) and is never sent
          to the server.
        </li>
        <li>Optional passphrases add Argon2id key stretching on top of the link secret.</li>
        <li>Capability tokens for owner and editor access are stored only as keyed hashes.</li>
      </ul>
    </article>
  );
}

export function Privacy() {
  return (
    <article className="mx-auto max-w-2xl space-y-4">
      <h1 className="text-3xl font-bold">Privacy</h1>
      <p className="text-muted-foreground">
        RedPaste is anonymous by design. There are no accounts, no logins, and no
        third-party analytics or trackers on any page. Paste content, titles, and
        language selections are encrypted before they reach the server.
      </p>
      <h2 className="pt-2 text-xl font-semibold">What the server can see</h2>
      <ul className="list-disc space-y-1.5 pl-5 text-muted-foreground">
        <li>Ciphertext size and creation time.</li>
        <li>Expiration and view policy.</li>
        <li>Network metadata required to operate the service, retained briefly.</li>
      </ul>
      <p className="text-muted-foreground">
        The server never receives your plaintext, keys, passphrases, or URL fragments.
      </p>
    </article>
  );
}

export function NotFound() {
  return (
    <article className="mx-auto max-w-2xl space-y-4">
      <h1 className="text-3xl font-bold">Not found</h1>
      <p className="text-muted-foreground">
        This content is unavailable. It may have expired, been deleted, or never existed.
      </p>
      <Button asChild>
        <a href="/">Back to RedPaste</a>
      </Button>
    </article>
  );
}
