import { useEffect, useRef, useState } from "react";
import { ArrowLeft, Eye, EyeOff, Loader2, Pencil, Plus, X } from "lucide-react";
import { loadFromLink, LoadError } from "@/diff/load";
import { DiffWorkspace } from "@/diff/DiffWorkspace";
import type { DiffPayload } from "@/diff/model";
import { encryptEnvelope, newOwnerToken, PassphraseRequired, type Envelope } from "@/crypto/envelope";
import { buildFragment } from "@/crypto/fragment";
import { createPaste, ApiError } from "@/api/client";
import { useWideLayout } from "@/app/layout";
import { Card } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { Select, SelectTrigger, SelectValue, SelectContent, SelectItem } from "@/components/ui/select";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog";
import { toast } from "@/lib/toast";

const EXPIRY = [
  { label: "10 minutes", secs: 600 },
  { label: "1 hour", secs: 3600 },
  { label: "1 day", secs: 86400 },
  { label: "7 days", secs: 604800 },
  { label: "30 days", secs: 2592000 },
  { label: "90 days", secs: 7776000 },
  { label: "6 months (180 days)", secs: 15552000 },
];

interface Version {
  id: string;
  name: string;
  content: string;
}

interface CreatedLink {
  share: string;
  canonical: string;
  owner: string;
  expiresAt: string;
  burn: boolean;
  maxViews: number | null;
}

const lineCount = (s: string) => (s === "" ? 0 : s.split("\n").length);

export function Diff() {
  const [versions, setVersions] = useState<Version[]>([
    { id: "v1", name: "Version 1", content: "" },
    { id: "v2", name: "Version 2", content: "" },
  ]);
  const nextNum = useRef(3);
  const [slotA, setSlotA] = useState("v1");
  const [slotB, setSlotB] = useState("v2");
  const [compared, setCompared] = useState(false);
  const [sharing, setSharing] = useState(false);
  const [created, setCreated] = useState<CreatedLink | null>(null);

  useWideLayout(compared);

  const versionA = versions.find((v) => v.id === slotA) ?? versions[0];
  const versionB = versions.find((v) => v.id === slotB) ?? versions[1];

  function updateContent(id: string, content: string) {
    setVersions((vs) => vs.map((v) => (v.id === id ? { ...v, content } : v)));
  }

  function renameVersion(id: string, name: string) {
    setVersions((vs) => vs.map((v) => (v.id === id && name.trim() ? { ...v, name: name.trim() } : v)));
  }

  function addVersion() {
    const v: Version = { id: crypto.randomUUID(), name: `Version ${nextNum.current++}`, content: "" };
    setVersions((vs) => [...vs, v]);
    setSlotB(v.id);
  }

  function removeVersion(id: string) {
    if (versions.length <= 2) return;
    const next = versions.filter((v) => v.id !== id);
    setVersions(next);
    if (slotA === id) setSlotA(next[0].id);
    if (slotB === id) setSlotB(next[next.length - 1].id);
  }

  function swap() {
    setSlotA(slotB);
    setSlotB(slotA);
  }

  if (compared) {
    return (
      <div className="space-y-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <Button variant="outline" size="sm" onClick={() => setCompared(false)}>
            <ArrowLeft /> Edit
          </Button>
          {versions.length > 2 && (
            <div className="flex items-center gap-2 text-sm">
              <VersionPicker versions={versions} value={slotA} onChange={setSlotA} />
              <span className="text-muted-foreground">vs</span>
              <VersionPicker versions={versions} value={slotB} onChange={setSlotB} />
            </div>
          )}
          <Button variant="outline" size="sm" onClick={swap}>
            Swap
          </Button>
        </div>

        <DiffWorkspace
          leftName={versionA.name}
          rightName={versionB.name}
          leftText={versionA.content}
          rightText={versionB.content}
          onShare={() => setSharing(true)}
        />

        <ShareDialog
          open={sharing}
          pair={{ a: versionA, b: versionB }}
          onClose={() => setSharing(false)}
          onCreated={(links) => {
            setSharing(false);
            setCreated(links);
          }}
        />
        <ShareSuccessDialog links={created} onClose={() => setCreated(null)} />
      </div>
    );
  }

  return (
    <div className="space-y-5">
      <div className="space-y-2">
        <h1 className="text-3xl font-bold">Diff checker</h1>
        <p className="text-muted-foreground">
          Paste text in each pane, load an existing paste by its share link, or keep more versions around to switch
          between. Everything is compared in your browser.
        </p>
      </div>

      {versions.length > 2 && (
        <p className="text-sm text-muted-foreground">
          {versions.length} versions saved · pick which two to compare from each pane's dropdown.
        </p>
      )}

      <div className="grid gap-4 md:grid-cols-2">
        <VersionPane
          version={versionA}
          versions={versions}
          onSelect={setSlotA}
          onRename={renameVersion}
          onChange={(v) => updateContent(versionA.id, v)}
          onRemove={removeVersion}
        />
        <VersionPane
          version={versionB}
          versions={versions}
          onSelect={setSlotB}
          onRename={renameVersion}
          onChange={(v) => updateContent(versionB.id, v)}
          onRemove={removeVersion}
        />
      </div>

      <div className="flex flex-wrap gap-2">
        <Button variant="outline" onClick={swap}>
          Swap
        </Button>
        <Button disabled={versionA.content === "" && versionB.content === ""} onClick={() => setCompared(true)}>
          Compare
        </Button>
        <Button variant="ghost" onClick={addVersion}>
          <Plus /> Add version
        </Button>
      </div>
    </div>
  );
}

function VersionPicker({ versions, value, onChange }: { versions: Version[]; value: string; onChange: (id: string) => void }) {
  return (
    <Select value={value} onValueChange={onChange}>
      <SelectTrigger className="h-8 w-auto max-w-[150px] gap-1 px-2 text-sm">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {versions.map((v) => (
          <SelectItem key={v.id} value={v.id}>
            {v.name}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

function VersionPane({
  version,
  versions,
  onSelect,
  onRename,
  onChange,
  onRemove,
}: {
  version: Version;
  versions: Version[];
  onSelect: (id: string) => void;
  onRename: (id: string, name: string) => void;
  onChange: (v: string) => void;
  onRemove: (id: string) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(version.name);
  const [linkOpen, setLinkOpen] = useState(false);
  const [link, setLink] = useState("");
  const [pass, setPass] = useState("");
  const [needPass, setNeedPass] = useState(false);
  const [busy, setBusy] = useState(false);

  function startEdit() {
    setDraft(version.name);
    setEditing(true);
  }
  function commitEdit() {
    onRename(version.id, draft);
    setEditing(false);
  }

  async function load() {
    setBusy(true);
    try {
      const content = await loadFromLink(link, needPass ? pass : undefined);
      onChange(content);
      setLinkOpen(false);
      setLink("");
      setPass("");
      setNeedPass(false);
    } catch (e) {
      if (e instanceof PassphraseRequired) {
        setNeedPass(true);
        toast("This paste needs a passphrase", "info");
      } else {
        toast(e instanceof LoadError ? e.message : "Couldn't load that link", "error");
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card className="flex flex-col p-4">
      <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-1">
          {editing ? (
            <Input
              autoFocus
              value={draft}
              onChange={(e) => setDraft((e.target as HTMLInputElement).value)}
              onBlur={commitEdit}
              onKeyDown={(e) => {
                if (e.key === "Enter") commitEdit();
                if (e.key === "Escape") setEditing(false);
              }}
              className="h-7 w-36 text-sm font-medium"
            />
          ) : versions.length > 2 ? (
            <VersionPicker versions={versions} value={version.id} onChange={onSelect} />
          ) : (
            <span className="truncate font-medium">{version.name}</span>
          )}
          <Button
            variant="ghost"
            size="icon"
            className="size-6 shrink-0 text-muted-foreground hover:text-foreground"
            onClick={startEdit}
            aria-label={`Rename ${version.name}`}
          >
            <Pencil className="size-3.5" />
          </Button>
          {versions.length > 2 && (
            <Button
              variant="ghost"
              size="icon"
              className="size-6 shrink-0 text-muted-foreground hover:text-destructive"
              onClick={() => onRemove(version.id)}
              aria-label={`Remove ${version.name}`}
            >
              <X className="size-3.5" />
            </Button>
          )}
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <span className="text-xs text-muted-foreground">{lineCount(version.content)} lines</span>
          <Button variant="outline" size="sm" onClick={() => setLinkOpen(!linkOpen)}>
            Load link
          </Button>
        </div>
      </div>
      {linkOpen && (
        <div className="mb-2 space-y-2">
          <div className="flex gap-2">
            <Input
              placeholder="https://…/s/xxxx#A…"
              value={link}
              onChange={(e) => setLink((e.target as HTMLInputElement).value)}
            />
            <Button disabled={busy || link === ""} onClick={() => void load()}>
              {busy ? "…" : "Load"}
            </Button>
          </div>
          {needPass && (
            <Input
              type="password"
              placeholder="Passphrase"
              value={pass}
              onChange={(e) => setPass((e.target as HTMLInputElement).value)}
            />
          )}
        </div>
      )}
      <Textarea
        spellCheck={false}
        value={version.content}
        onChange={(e) => onChange((e.target as HTMLTextAreaElement).value)}
        placeholder="Paste text…"
        className="min-h-[40vh] resize-y font-mono text-sm"
      />
    </Card>
  );
}

function ShareDialog({
  open,
  pair,
  onClose,
  onCreated,
}: {
  open: boolean;
  pair: { a: Version; b: Version };
  onClose: () => void;
  onCreated: (links: CreatedLink) => void;
}) {
  const [expiry, setExpiry] = useState(604800);
  const [burn, setBurn] = useState(false);
  const [maxViews, setMaxViews] = useState("");
  const [passphrase, setPassphrase] = useState("");
  const [showPass, setShowPass] = useState(false);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (open) {
      setExpiry(604800);
      setBurn(false);
      setMaxViews("");
      setPassphrase("");
      setShowPass(false);
    }
  }, [open]);

  async function submit() {
    if (busy) return;
    setBusy(true);
    try {
      const payload: DiffPayload = {
        a: { name: pair.a.name, content: pair.a.content },
        b: { name: pair.b.name, content: pair.b.content },
      };
      const env: Envelope = {
        version: 1,
        type: "diff",
        title: `${pair.a.name} vs ${pair.b.name}`,
        content: JSON.stringify(payload),
        language: "diff",
        editor: { wrap: false, tabSize: 4 },
        createdAtClient: new Date().toISOString(),
      };
      const { linkSecret, ciphertext, meta } = await encryptEnvelope(env, passphrase || undefined);
      const ownerToken = newOwnerToken();
      const mv = maxViews.trim() === "" ? null : Math.max(1, parseInt(maxViews, 10) || 1);

      const res = await createPaste({
        ciphertext,
        meta,
        expiresInSeconds: expiry,
        burnAfterRead: burn,
        maxViews: mv,
        ownerToken,
        shortLink: true,
        kind: "diff",
      });

      const share = (res.shortUrlBase ?? res.urlBase) + buildFragment("A", linkSecret);
      const canonical = res.urlBase + buildFragment("A", linkSecret);
      const owner = res.urlBase.replace("/p/", "/manage/") + buildFragment("O", ownerToken);
      onCreated({ share, canonical, owner, expiresAt: res.expiresAt, burn, maxViews: mv });
    } catch (e) {
      const msg = e instanceof ApiError ? `Couldn't create link (${e.code})` : "Encryption failed";
      toast(msg, "error");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Share this comparison</DialogTitle>
          <DialogDescription>
            Creates one encrypted paste holding both versions ({pair.a.name} vs {pair.b.name}). Anyone with the link
            sees the same diff view, split/unified toggle included. The decryption key stays in the link.
          </DialogDescription>
        </DialogHeader>

        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label>Expires</Label>
            <Select value={String(expiry)} onValueChange={(v) => setExpiry(parseInt(v, 10))}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {EXPIRY.map((x) => (
                  <SelectItem key={x.secs} value={String(x.secs)}>
                    {x.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="diff-max-views">Max views</Label>
            <Input
              id="diff-max-views"
              type="number"
              min={1}
              placeholder="unlimited"
              value={maxViews}
              onChange={(e) => setMaxViews((e.target as HTMLInputElement).value)}
            />
          </div>
          <div className="flex items-center gap-2">
            <Switch id="diff-burn" checked={burn} onCheckedChange={setBurn} />
            <Label htmlFor="diff-burn">Burn after reading</Label>
          </div>
          <div className="space-y-1.5 sm:col-span-2">
            <Label htmlFor="diff-passphrase">Passphrase (optional)</Label>
            <div className="flex gap-2">
              <Input
                id="diff-passphrase"
                type={showPass ? "text" : "password"}
                autoComplete="new-password"
                placeholder="Extra protection on top of the link"
                value={passphrase}
                onChange={(e) => setPassphrase((e.target as HTMLInputElement).value)}
              />
              <Button
                type="button"
                variant="outline"
                size="icon"
                onClick={() => setShowPass(!showPass)}
                aria-label={showPass ? "Hide passphrase" : "Show passphrase"}
              >
                {showPass ? <EyeOff /> : <Eye />}
              </Button>
            </div>
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button disabled={busy} onClick={() => void submit()}>
            {busy && <Loader2 className="animate-spin" />}
            {busy ? "Encrypting…" : "Create link"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function ShareSuccessDialog({ links, onClose }: { links: CreatedLink | null; onClose: () => void }) {
  const [showOwner, setShowOwner] = useState(false);
  if (!links) return null;

  const expires = new Date(links.expiresAt).toLocaleString();
  const policy = [
    links.burn ? "burns after first read" : null,
    links.maxViews ? `${links.maxViews} view${links.maxViews > 1 ? "s" : ""} max` : null,
    `expires ${expires}`,
  ]
    .filter(Boolean)
    .join(" · ");

  async function copy(text: string, label: string) {
    try {
      await navigator.clipboard.writeText(text);
      toast(`${label} copied`, "success");
    } catch {
      toast("Copy failed. Select and copy manually", "error");
    }
  }

  return (
    <Dialog open={!!links} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Link created</DialogTitle>
          <DialogDescription>
            Share this link. The decryption key is in it, so anyone with the full link can view the comparison.
          </DialogDescription>
        </DialogHeader>

        <div className="flex gap-2">
          <Input
            readOnly
            className="font-mono text-sm"
            value={links.share}
            onClick={(e) => (e.target as HTMLInputElement).select()}
          />
          <Button onClick={() => void copy(links.share, "Link")}>Copy</Button>
        </div>

        <p className="text-sm text-muted-foreground">{policy}</p>

        <div className="rounded-lg border border-border p-3">
          <Button
            variant="ghost"
            className="h-auto w-full justify-start p-0 hover:bg-transparent"
            aria-expanded={showOwner}
            onClick={() => setShowOwner(!showOwner)}
          >
            Private owner controls
          </Button>
          {showOwner && (
            <div className="mt-3 space-y-2">
              <p className="text-sm text-destructive">
                <strong>Keep this private.</strong> It lets you view status and delete the paste. Don't share it.
              </p>
              <div className="flex gap-2">
                <Input
                  readOnly
                  className="font-mono text-sm"
                  value={links.owner}
                  onClick={(e) => (e.target as HTMLInputElement).select()}
                />
                <Button variant="outline" onClick={() => void copy(links.owner, "Owner link")}>
                  Copy
                </Button>
              </div>
            </div>
          )}
        </div>

        <DialogFooter>
          <Button onClick={onClose}>Done</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
