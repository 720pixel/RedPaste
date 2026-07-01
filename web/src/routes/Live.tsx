import { useEffect, useMemo, useRef, useState } from "react";
import * as Y from "yjs";
import { Awareness } from "y-protocols/awareness";
import { Copy, Loader2, Lock, Snowflake, Trash2, Users } from "lucide-react";
import { currentFragment, buildFragment } from "@/crypto/fragment";
import {
  deriveRoomKeys,
  deriveEditorToken,
  newPasswordRoomKey,
  deriveRoomKeyForJoin,
  newJoinSecret,
  newOwnerToken,
  decryptUpdate,
} from "@/live/roomcrypto";
import { createRoom, bootstrap, freezeRoom, deleteRoom } from "@/live/api";
import { LiveProvider, type ConnStatus } from "@/live/provider";
import { LiveEditor } from "@/live/LiveEditor";
import { LANGUAGES } from "@/languages/list";
import { navigate } from "@/app/router";
import { Card } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Select, SelectTrigger, SelectValue, SelectContent, SelectItem } from "@/components/ui/select";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog";
import { toast } from "@/lib/toast";

// Default lifetime. Users don't get quizzed about it up front; they just start
// editing. It can be surfaced/tuned later without blocking the flow.
const DEFAULT_LIVE_SECS = 86400;

// Entry point at /live: one click and you're editing. A password is optional and
// tucked away, so the default path asks for nothing.
export function LiveStart() {
  const [busy, setBusy] = useState(false);
  const [showPass, setShowPass] = useState(false);
  const [password, setPassword] = useState("");

  async function start() {
    if (busy) return;
    setBusy(true);
    try {
      const joinSecret = newJoinSecret();
      const ownerToken = newOwnerToken();
      const editorToken = await deriveEditorToken(joinSecret);

      let cryptoMetadata: unknown = { v: 1, alg: "AES-256-GCM", pass: { on: false } };
      if (showPass && password) {
        const { meta } = await newPasswordRoomKey(joinSecret, password);
        cryptoMetadata = { v: 1, alg: "AES-256-GCM", pass: meta };
      }

      const res = await createRoom({
        editorToken,
        ownerToken,
        cryptoMetadata,
        snapshot: null,
        expiresInSeconds: DEFAULT_LIVE_SECS,
        shortLink: true,
      });
      const editorPath = new URL(res.urlBase).pathname; // /live/{slug}
      // Stash creator-only bits for this tab: owner capability, short base, and
      // the password (so the creator isn't re-prompted). None of these go in the URL.
      try {
        sessionStorage.setItem("rp-owner-" + editorPath, ownerToken);
        if (res.shortUrlBase) sessionStorage.setItem("rp-short-" + editorPath, res.shortUrlBase);
        if (showPass && password) sessionStorage.setItem("rp-pass-" + editorPath, password);
      } catch {
        /* ignore */
      }
      navigate(editorPath + buildFragment("L", joinSecret));
    } catch {
      toast("Couldn't start the pasteboard", "error");
      setBusy(false);
    }
  }

  return (
    <div className="mx-auto max-w-xl space-y-6 py-16 text-center">
      <h1 className="text-3xl font-bold">Live pasteboard</h1>
      <p className="text-muted-foreground">
        A real-time collaborative editor. Everyone edits together and content is encrypted in your browser before it
        syncs. Start now, share the link whenever you're ready.
      </p>
      <div className="space-y-3">
        <Button size="lg" disabled={busy} onClick={() => void start()}>
          {busy && <Loader2 className="animate-spin" />}
          {busy ? "Starting…" : "Start collaborating"}
        </Button>
        {!showPass ? (
          <div>
            <Button variant="link" size="sm" className="text-muted-foreground" onClick={() => setShowPass(true)}>
              <Lock className="size-3.5" /> Add a password
            </Button>
          </div>
        ) : (
          <div className="mx-auto flex max-w-sm items-center gap-2">
            <Input
              type="password"
              autoFocus
              placeholder="Room password"
              value={password}
              onChange={(e) => setPassword((e.target as HTMLInputElement).value)}
            />
            <Button variant="ghost" size="sm" onClick={() => { setShowPass(false); setPassword(""); }}>
              Remove
            </Button>
          </div>
        )}
      </div>
    </div>
  );
}

// /live/{slug}: editor (fragment #L) or owner controls (fragment #O)
export function LiveRoom({ slug }: { slug: string }) {
  const frag = currentFragment();
  if (frag?.kind === "O") return <LiveOwner slug={slug} ownerToken={frag.secret} />;
  if (frag?.kind === "L") return <LiveEditorRoom slug={slug} joinSecret={frag.secret} />;
  return (
    <div className="mx-auto max-w-2xl space-y-4 py-16 text-center">
      <h1 className="text-3xl font-bold">Live pasteboard</h1>
      <p className="text-muted-foreground">This link is missing its room key.</p>
      <Button asChild>
        <a href="/live">Start a new pasteboard</a>
      </Button>
    </div>
  );
}

const ANIMALS = ["Fox", "Owl", "Wren", "Lynx", "Hare", "Moth", "Crow", "Newt", "Ibis", "Vole"];
function anonUser(id: number) {
  return { name: ANIMALS[id % ANIMALS.length] + "-" + (id % 97), color: `hsl(${(id * 137) % 360} 70% 55%)` };
}

function LiveEditorRoom({ slug, joinSecret }: { slug: string; joinSecret: string }) {
  const [status, setStatus] = useState<ConnStatus>("connecting");
  const [peers, setPeers] = useState(1);
  const [frozen, setFrozen] = useState(false);
  const [gone, setGone] = useState<string | null>(null);
  const [language, setLanguage] = useState("plaintext");
  const [wrap, setWrap] = useState(false);
  const [lines, setLines] = useState(1);
  const [share, setShare] = useState(false);

  const yref = useRef<{ doc: Y.Doc; text: Y.Text; settings: Y.Map<string>; awareness: Awareness; provider: LiveProvider } | null>(null);
  const [ready, setReady] = useState(false);
  const [needPass, setNeedPass] = useState(false);
  const [passwordInput, setPasswordInput] = useState("");
  const [enteredPassword, setEnteredPassword] = useState<string | null>(null);

  useEffect(() => {
    let disposed = false;
    (async () => {
      const editorToken = await deriveEditorToken(joinSecret).catch(() => null);
      if (!editorToken) {
        setGone("This link is invalid.");
        return;
      }
      // Bootstrap first: it tells us whether the room needs a password and gives us
      // the initial state to hand straight to the provider (no double fetch).
      let boot;
      try {
        boot = await bootstrap(slug, editorToken);
      } catch {
        setGone("This room is unavailable.");
        return;
      }
      if (disposed) return;

      const passMeta = boot.crypto?.pass;
      const passOn = !!passMeta?.on;
      const pw = enteredPassword ?? (passOn ? sessionStorage.getItem("rp-pass-" + location.pathname) : null);
      if (passOn && !pw) {
        setNeedPass(true);
        return; // wait for the password, then this effect re-runs
      }

      let roomKey: CryptoKey;
      try {
        roomKey = passOn ? await deriveRoomKeyForJoin(joinSecret, pw!, passMeta!) : (await deriveRoomKeys(joinSecret)).roomKey;
      } catch {
        setGone("Couldn't unlock this room.");
        return;
      }
      // If the room already has content, verify the password by decrypting the snapshot.
      if (passOn && boot.snapshot) {
        try {
          await decryptUpdate(roomKey, boot.snapshot.ciphertext);
        } catch {
          if (!disposed) {
            setNeedPass(true);
            setEnteredPassword(null);
            toast("Wrong password", "error");
          }
          return;
        }
      }
      if (disposed) return;
      setNeedPass(false);

      const doc = new Y.Doc();
      const text = doc.getText("content");
      const settings = doc.getMap<string>("settings");
      const awareness = new Awareness(doc);
      awareness.setLocalStateField("user", anonUser(doc.clientID));
      settings.observe(() => {
        const l = settings.get("language");
        if (l) setLanguage(l);
      });

      const provider = new LiveProvider({
        slug,
        keys: { roomKey, editorToken },
        doc,
        awareness,
        onStatus: setStatus,
        onPeers: setPeers,
        onFrozen: () => setFrozen(true),
        onGone: setGone,
        initial: boot,
      });
      yref.current = { doc, text, settings, awareness, provider };
      setReady(true);
    })();

    return () => {
      disposed = true;
      yref.current?.provider.destroy();
      yref.current?.doc.destroy();
      yref.current = null;
    };
  }, [slug, joinSecret, enteredPassword]);

  const setLang = (l: string) => {
    setLanguage(l);
    yref.current?.settings.set("language", l);
  };

  // The creator holds the owner capability (stashed this tab) plus an optional
  // short link. Recipients who open the shared link have neither.
  const creator = useMemo(() => {
    try {
      const token = sessionStorage.getItem("rp-owner-" + location.pathname);
      const short = sessionStorage.getItem("rp-short-" + location.pathname);
      return {
        ownerToken: token,
        ownerLink: token ? location.origin + location.pathname + buildFragment("O", token) : null,
        shortEditor: short ? short + buildFragment("L", joinSecret) : null,
      };
    } catch {
      return { ownerToken: null, ownerLink: null, shortEditor: null };
    }
  }, [joinSecret]);

  const shareLink = creator.shortEditor ?? location.origin + location.pathname + buildFragment("L", joinSecret);
  const [confirmDelete, setConfirmDelete] = useState(false);

  // Nudge the creator to share, but don't shove a modal in their face on arrival.
  useEffect(() => {
    if (creator.ownerToken && ready) toast("Pasteboard ready. Click Share to invite people.", "success");
  }, [creator.ownerToken, ready]);

  const copy = async (t: string, label: string) => {
    try {
      await navigator.clipboard.writeText(t);
      toast(`${label} copied`, "success");
    } catch {
      toast("Copy failed", "error");
    }
  };
  const doFreeze = async () => {
    if (!creator.ownerToken) return;
    try {
      await freezeRoom(slug, creator.ownerToken);
      setFrozen(true);
      toast("Pasteboard frozen", "success");
    } catch {
      toast("Freeze failed", "error");
    }
  };
  const doDelete = async () => {
    if (!creator.ownerToken) return;
    try {
      await deleteRoom(slug, creator.ownerToken);
      setGone("You deleted this pasteboard.");
    } catch {
      toast("Delete failed", "error");
    }
  };

  if (gone) {
    return (
      <div className="mx-auto max-w-2xl space-y-4 py-16 text-center">
        <h1 className="text-3xl font-bold">Unavailable</h1>
        <p className="text-muted-foreground">{gone}</p>
        <Button asChild>
          <a href="/live">Start a new pasteboard</a>
        </Button>
      </div>
    );
  }

  if (needPass) {
    return (
      <div className="mx-auto max-w-sm py-16">
        <Card className="space-y-4 p-6 text-center">
          <Lock className="mx-auto size-6 text-primary" />
          <div>
            <h1 className="text-xl font-bold">Password required</h1>
            <p className="mt-1 text-sm text-muted-foreground">This pasteboard is protected by a password.</p>
          </div>
          <div className="space-y-2 text-left">
            <Label htmlFor="room-pass">Password</Label>
            <Input
              id="room-pass"
              type="password"
              autoFocus
              value={passwordInput}
              onChange={(e) => setPasswordInput((e.target as HTMLInputElement).value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && passwordInput) setEnteredPassword(passwordInput);
              }}
            />
          </div>
          <Button className="w-full" disabled={!passwordInput} onClick={() => setEnteredPassword(passwordInput)}>
            Unlock
          </Button>
        </Card>
      </div>
    );
  }

  const dotColor =
    status === "connected" ? "bg-success" : status === "reconnecting" ? "bg-yellow-500" : "bg-muted-foreground";
  const statusLabel = status === "connected" ? "Live" : status === "reconnecting" ? "Reconnecting…" : "Connecting…";

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border bg-card px-4 py-2.5">
        <div className="flex items-center gap-3">
          <span className="flex items-center gap-1.5 text-sm" aria-live="polite">
            <span className={"size-2 rounded-full " + dotColor} />
            {statusLabel}
          </span>
          <span className="flex items-center gap-1.5 text-sm text-muted-foreground" title="Participants">
            <Users className="size-4" /> {peers} online
          </span>
          {frozen && <Badge variant="destructive">frozen</Badge>}
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Select value={language} onValueChange={setLang}>
            <SelectTrigger className="h-8 w-36 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {LANGUAGES.map((l) => (
                <SelectItem key={l.id} value={l.id}>
                  {l.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button variant="outline" size="sm" onClick={() => setWrap(!wrap)}>
            {wrap ? "No wrap" : "Wrap"}
          </Button>
          {creator.ownerToken && !frozen && (
            <Button variant="outline" size="sm" onClick={() => void doFreeze()} title="Stop further edits">
              <Snowflake /> Freeze
            </Button>
          )}
          {creator.ownerToken && (
            <Button variant="destructive" size="sm" onClick={() => setConfirmDelete(true)}>
              <Trash2 /> Delete
            </Button>
          )}
          <Button size="sm" onClick={() => setShare(true)}>
            Share
          </Button>
        </div>
      </div>

      <Card className="overflow-hidden">
        {ready && yref.current && (
          <LiveEditor
            ytext={yref.current.text}
            awareness={yref.current.awareness}
            language={language}
            wrap={wrap}
            onLineCount={setLines}
          />
        )}
        <div className="flex justify-between border-t border-border px-4 py-2 text-xs text-muted-foreground">
          <span>
            {lines} {lines === 1 ? "line" : "lines"}
          </span>
          <span>{frozen ? "read-only" : "editing"}</span>
        </div>
      </Card>

      <Dialog open={share} onOpenChange={setShare}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>Share this pasteboard</DialogTitle>
            <DialogDescription>Anyone with this link can join and edit. It carries the room key.</DialogDescription>
          </DialogHeader>
          <div className="flex gap-2">
            <Input
              readOnly
              className="font-mono text-sm"
              value={shareLink}
              onClick={(e) => (e.target as HTMLInputElement).select()}
            />
            <Button onClick={() => copy(shareLink, "Link")}>
              <Copy /> Copy
            </Button>
          </div>
          {creator.ownerLink && (
            <div className="space-y-2 rounded-lg border border-border p-3">
              <p className="text-sm text-destructive">
                <strong>Keep private.</strong> Your owner link can freeze or delete the room.
              </p>
              <div className="flex gap-2">
                <Input
                  readOnly
                  className="font-mono text-sm"
                  value={creator.ownerLink}
                  onClick={(e) => (e.target as HTMLInputElement).select()}
                />
                <Button variant="outline" onClick={() => copy(creator.ownerLink!, "Owner link")}>
                  <Copy /> Copy
                </Button>
              </div>
            </div>
          )}
        </DialogContent>
      </Dialog>

      <Dialog open={confirmDelete} onOpenChange={setConfirmDelete}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete this pasteboard?</DialogTitle>
            <DialogDescription>This revokes the room immediately for everyone. It can't be undone.</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmDelete(false)}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={() => void doDelete()}>
              Delete permanently
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function LiveOwner({ slug, ownerToken }: { slug: string; ownerToken: string }) {
  const [confirm, setConfirm] = useState(false);
  const [done, setDone] = useState<string | null>(null);

  async function freeze() {
    try {
      await freezeRoom(slug, ownerToken);
      setDone("Pasteboard frozen. No more edits are accepted.");
    } catch {
      toast("Freeze failed", "error");
    }
  }
  async function del() {
    try {
      await deleteRoom(slug, ownerToken);
      setConfirm(false);
      setDone("Pasteboard deleted.");
    } catch {
      toast("Delete failed", "error");
    }
  }

  if (done) {
    return (
      <div className="mx-auto max-w-2xl space-y-4 py-16 text-center">
        <h1 className="text-3xl font-bold">Done</h1>
        <p className="text-muted-foreground">{done}</p>
        <Button asChild>
          <a href="/">Back to RedPaste</a>
        </Button>
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-2xl space-y-5">
      <h1 className="text-3xl font-bold">Pasteboard owner controls</h1>
      <p className="text-muted-foreground">
        You hold the owner capability. You can freeze or delete this room, but this link can't read its contents.
      </p>
      <div className="flex gap-2">
        <Button variant="outline" onClick={() => void freeze()}>
          <Snowflake /> Freeze (stop edits)
        </Button>
        <Button variant="destructive" onClick={() => setConfirm(true)}>
          <Trash2 /> Delete pasteboard
        </Button>
      </div>
      <Dialog open={confirm} onOpenChange={setConfirm}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete this pasteboard?</DialogTitle>
            <DialogDescription>This revokes the room immediately for everyone. It can't be undone.</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirm(false)}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={() => void del()}>
              Delete permanently
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
