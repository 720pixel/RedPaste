import { useRef, useState } from "react";
import { Eye, EyeOff, Loader2 } from "lucide-react";
import { detectLanguage } from "@/editor/detect";
import { LANGUAGES } from "@/languages/list";
import { encryptEnvelope, newOwnerToken, type Envelope } from "@/crypto/envelope";
import { buildFragment } from "@/crypto/fragment";
import { createPaste, ApiError } from "@/api/client";
import { MAX_PASTE_BYTES, approxMaxLines, contentByteLength, formatBytes } from "@/lib/limits";
import { SuccessDialog, type CreatedLinks } from "./SuccessDialog";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { Select, SelectTrigger, SelectValue, SelectContent, SelectItem } from "@/components/ui/select";
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

export function Create() {
  const [title, setTitle] = useState("");
  const [content, setContent] = useState("");
  const [language, setLanguage] = useState("plaintext");
  const [autoLang, setAutoLang] = useState(true);
  const detectTimer = useRef<number | undefined>(undefined);
  const lineCount = content === "" ? 1 : content.split("\n").length;
  const contentBytes = contentByteLength(content);
  const overLimit = contentBytes > MAX_PASTE_BYTES;
  const [expiry, setExpiry] = useState(604800);
  const [burn, setBurn] = useState(false);
  const [maxViews, setMaxViews] = useState<string>("");
  const [wrap, setWrap] = useState(false);
  const [passphrase, setPassphrase] = useState("");
  const [showPass, setShowPass] = useState(false);
  const [busy, setBusy] = useState(false);
  const [created, setCreated] = useState<CreatedLinks | null>(null);

  async function submit() {
    if (content === "" || busy) return;
    if (contentByteLength(content) > MAX_PASTE_BYTES) {
      toast(`Paste is too large (max ${formatBytes(MAX_PASTE_BYTES)})`, "error");
      return;
    }
    setBusy(true);
    try {
      const env: Envelope = {
        version: 1,
        type: "static",
        title: title || undefined,
        content,
        language,
        editor: { wrap, tabSize: 4 },
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
      });

      const share = (res.shortUrlBase ?? res.urlBase) + buildFragment("A", linkSecret);
      const canonical = res.urlBase + buildFragment("A", linkSecret);
      const owner = res.urlBase.replace("/p/", "/manage/") + buildFragment("O", ownerToken);
      setCreated({ share, canonical, owner, expiresAt: res.expiresAt, burn, maxViews: mv });
    } catch (e) {
      const msg = e instanceof ApiError ? `Couldn't create paste (${e.code})` : "Encryption failed";
      toast(msg, "error");
    } finally {
      setBusy(false);
    }
  }

  function reset() {
    setCreated(null);
    setContent("");
    setTitle("");
    setPassphrase("");
  }

  const onKeyDown = (e: React.KeyboardEvent) => {
    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
      e.preventDefault();
      void submit();
    }
  };

  // Auto-detect after typing settles so keystrokes stay instant.
  const onContentChange = (v: string) => {
    setContent(v);
    if (autoLang) {
      window.clearTimeout(detectTimer.current);
      detectTimer.current = window.setTimeout(() => setLanguage(detectLanguage(v).id), 350);
    }
  };

  return (
    <div className="space-y-4" onKeyDown={onKeyDown}>
      <Input
        type="text"
        placeholder="Optional title"
        value={title}
        maxLength={200}
        className="text-base"
        onChange={(e) => setTitle((e.target as HTMLInputElement).value)}
      />

      <div className="space-y-1.5">
        <Textarea
          spellCheck={false}
          autoCapitalize="off"
          autoComplete="off"
          autoCorrect="off"
          autoFocus
          value={content}
          placeholder="Paste or type here. Encrypted in your browser before it ever leaves."
          className={
            "min-h-[58vh] w-full resize-y rounded-lg border bg-card px-5 py-4 font-mono text-sm leading-relaxed outline-none focus-visible:ring-2 focus-visible:ring-ring" +
            (wrap ? "" : " whitespace-pre")
          }
          onChange={(e) => onContentChange((e.target as HTMLTextAreaElement).value)}
        />
        <div className="flex justify-between px-1 text-xs text-muted-foreground">
          <span className={overLimit ? "text-destructive" : ""}>
            {lineCount} {lineCount === 1 ? "line" : "lines"} · {content.length} chars ·{" "}
            {formatBytes(contentBytes)}
            {overLimit
              ? ` (over the ${formatBytes(MAX_PASTE_BYTES)} limit)`
              : ` of ${formatBytes(MAX_PASTE_BYTES)} (~${approxMaxLines().toLocaleString()} lines max)`}
          </span>
          <span>
            {autoLang ? "auto · " : ""}
            {language}
          </span>
        </div>
      </div>

      <div className="grid gap-4 rounded-lg border border-border bg-card p-5 sm:grid-cols-2">
        <div className="space-y-1.5">
          <Label>Syntax</Label>
          <Select
            value={autoLang ? "auto" : language}
            onValueChange={(v) => {
              if (v === "auto") {
                setAutoLang(true);
                setLanguage(detectLanguage(content).id);
              } else {
                setAutoLang(false);
                setLanguage(v);
              }
            }}
          >
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="auto">
                Auto-detect{autoLang && language !== "plaintext" ? ` · ${language}` : ""}
              </SelectItem>
              {LANGUAGES.map((l) => (
                <SelectItem key={l.id} value={l.id}>
                  {l.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

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
          <Label htmlFor="max-views">Max views</Label>
          <Input
            id="max-views"
            type="number"
            min={1}
            placeholder="unlimited"
            value={maxViews}
            onChange={(e) => setMaxViews((e.target as HTMLInputElement).value)}
          />
        </div>

        <div className="flex items-end gap-6">
          <div className="flex items-center gap-2">
            <Switch id="burn" checked={burn} onCheckedChange={setBurn} />
            <Label htmlFor="burn">Burn after reading</Label>
          </div>
          <div className="flex items-center gap-2">
            <Switch id="wrap" checked={wrap} onCheckedChange={setWrap} />
            <Label htmlFor="wrap">Wrap lines</Label>
          </div>
        </div>

        <div className="space-y-1.5 sm:col-span-2">
          <Label htmlFor="passphrase">Passphrase (optional)</Label>
          <div className="flex gap-2">
            <Input
              id="passphrase"
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

      <div className="sticky bottom-4 flex items-center justify-between gap-4 rounded-lg border border-border bg-card/90 px-4 py-3 backdrop-blur">
        <span className="text-xs text-muted-foreground">Encrypted in your browser · ⌘/Ctrl + Enter</span>
        <Button disabled={busy || content === "" || overLimit} onClick={() => void submit()}>
          {busy && <Loader2 className="animate-spin" />}
          {busy ? "Encrypting…" : "Create encrypted paste"}
        </Button>
      </div>

      <SuccessDialog links={created} onClose={reset} />
    </div>
  );
}
