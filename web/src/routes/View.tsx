import { useEffect, useState } from "react";
import { Copy, Download } from "lucide-react";
import { getMeta, redeem, ApiError } from "@/api/client";
import { currentFragment } from "@/crypto/fragment";
import { decryptEnvelope, DecryptError, PassphraseRequired, type Envelope } from "@/crypto/envelope";
import { Editor } from "@/editor/Editor";
import { labelFor } from "@/languages/list";
import { DiffWorkspace } from "@/diff/DiffWorkspace";
import type { DiffPayload } from "@/diff/model";
import { useWideLayout } from "@/app/layout";
import { Card } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { toast } from "@/lib/toast";

type State =
  | { phase: "loading" }
  | { phase: "reveal"; needsPass: boolean; consumesView: boolean }
  | { phase: "working" }
  | { phase: "error"; message: string }
  | { phase: "shown"; env: Envelope };

export function View({ slug }: { slug: string }) {
  const [state, setState] = useState<State>({ phase: "loading" });
  const [pass, setPass] = useState("");
  const frag = currentFragment();
  useWideLayout(state.phase === "shown" && state.env.type === "diff");

  useEffect(() => {
    let cancelled = false;
    (async () => {
      if (!frag || frag.kind !== "A") {
        setState({ phase: "error", message: "This link is missing its decryption key." });
        return;
      }
      try {
        const meta = await getMeta(slug);
        if (cancelled) return;
        // A passphrase or a one-time/limited policy means we stop and ask first.
        if (meta.passphraseProtected || meta.requiresExplicitReveal) {
          setState({ phase: "reveal", needsPass: meta.passphraseProtected, consumesView: meta.requiresExplicitReveal });
        } else {
          await doReveal("");
        }
      } catch (e) {
        if (!cancelled) setState({ phase: "error", message: unavailable(e) });
      }
    })();
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [slug]);

  async function doReveal(passphrase: string) {
    if (!frag) return;
    setState({ phase: "working" });
    try {
      const r = await redeem(slug);
      const env = await decryptEnvelope(frag.secret, r.ciphertext, r.crypto, passphrase || undefined);
      setState({ phase: "shown", env });
    } catch (e) {
      if (e instanceof PassphraseRequired) {
        setState({ phase: "reveal", needsPass: true, consumesView: false });
        toast("This paste needs a passphrase", "info");
      } else if (e instanceof DecryptError) {
        setState({ phase: "error", message: e.message });
      } else {
        setState({ phase: "error", message: unavailable(e) });
      }
    }
  }

  if (state.phase === "loading" || state.phase === "working") {
    return (
      <div className="py-20 text-center text-muted-foreground">
        {state.phase === "working" ? "Decrypting…" : "Loading…"}
      </div>
    );
  }
  if (state.phase === "error") {
    return (
      <div className="mx-auto max-w-2xl space-y-4 py-16 text-center">
        <h1 className="text-3xl font-bold">Unavailable</h1>
        <p className="text-muted-foreground">{state.message}</p>
        <Button asChild>
          <a href="/">Create a new paste</a>
        </Button>
      </div>
    );
  }
  if (state.phase === "reveal") {
    return (
      <Card className="mx-auto max-w-md space-y-4 p-6">
        <h1 className="text-2xl font-bold">{state.needsPass ? "Passphrase required" : "Reveal paste"}</h1>
        <p className="text-muted-foreground">
          {state.consumesView
            ? "This paste is one-time or view-limited. Revealing it may consume an allowed view."
            : "This paste is protected by a passphrase."}
        </p>
        {state.needsPass && (
          <Input
            type="password"
            placeholder="Passphrase"
            value={pass}
            autoFocus
            onChange={(e) => setPass((e.target as HTMLInputElement).value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") void doReveal(pass);
            }}
          />
        )}
        <Button className="w-full" disabled={state.needsPass && pass === ""} onClick={() => void doReveal(pass)}>
          {state.needsPass ? "Decrypt" : "Reveal paste"}
        </Button>
      </Card>
    );
  }

  if (state.env.type === "diff") return <SharedDiff env={state.env} />;
  return <Shown env={state.env} />;
}

function SharedDiff({ env }: { env: Envelope }) {
  let payload: DiffPayload | null = null;
  try {
    payload = JSON.parse(env.content);
  } catch {
    payload = null;
  }

  if (!payload) {
    return (
      <div className="mx-auto max-w-2xl space-y-4 py-16 text-center">
        <h1 className="text-3xl font-bold">Unavailable</h1>
        <p className="text-muted-foreground">This comparison couldn't be read.</p>
      </div>
    );
  }

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-xl font-bold">{env.title || "Shared comparison"}</h1>
        <Button variant="outline" size="sm" asChild>
          <a href="/diff">Open diff checker</a>
        </Button>
      </div>
      <DiffWorkspace leftName={payload.a.name} rightName={payload.b.name} leftText={payload.a.content} rightText={payload.b.content} />
    </div>
  );
}

function Shown({ env }: { env: Envelope }) {
  // Wrap on by default when viewing so long lines don't force horizontal scroll.
  const [wrap, setWrap] = useState(env.editor?.wrap ?? true);
  const [lines, setLines] = useState(env.content === "" ? 1 : env.content.split("\n").length);
  function download() {
    const blob = new Blob([env.content], { type: "text/plain" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = (env.title || "paste") + ".txt";
    a.click();
    URL.revokeObjectURL(url);
  }
  async function copyAll() {
    try {
      await navigator.clipboard.writeText(env.content);
      toast("Copied", "success");
    } catch {
      toast("Copy failed", "error");
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          {env.title && <h1 className="text-2xl font-bold">{env.title}</h1>}
          <Badge variant="outline">{labelFor(env.language)}</Badge>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" onClick={() => setWrap(!wrap)}>
            {wrap ? "No wrap" : "Wrap"}
          </Button>
          <Button variant="outline" size="sm" onClick={copyAll}>
            <Copy /> Copy all
          </Button>
          <Button variant="outline" size="sm" onClick={download}>
            <Download /> Download
          </Button>
        </div>
      </div>
      <Card className="overflow-hidden">
        <Editor value={env.content} language={env.language} readOnly wrap={wrap} onLineCount={setLines} />
        <div className="flex justify-between border-t border-border px-4 py-2 text-xs text-muted-foreground">
          <span>
            {lines} {lines === 1 ? "line" : "lines"} · {env.content.length} chars
          </span>
          <span>{labelFor(env.language)}</span>
        </div>
      </Card>
    </div>
  );
}

function unavailable(e: unknown): string {
  if (e instanceof ApiError && e.status === 410) {
    return "This paste has expired, been deleted, or already been viewed.";
  }
  return "This paste is unavailable.";
}
