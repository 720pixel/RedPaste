import { useState } from "react";
import { Copy, ChevronRight, ChevronDown } from "lucide-react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { toast } from "@/lib/toast";

export interface CreatedLinks {
  share: string;
  canonical: string;
  owner: string;
  expiresAt: string;
  burn: boolean;
  maxViews: number | null;
}

async function copy(text: string, label: string) {
  try {
    await navigator.clipboard.writeText(text);
    toast(`${label} copied`, "success");
  } catch {
    toast("Copy failed. Select and copy manually", "error");
  }
}

export function SuccessDialog({ links, onClose }: { links: CreatedLinks | null; onClose: () => void }) {
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

  const openPath = new URL(links.share).pathname + new URL(links.share).hash;

  return (
    <Dialog open={!!links} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Paste created</DialogTitle>
          <DialogDescription>
            Share this link. The decryption key is in it, so anyone with the full link can read the paste.
          </DialogDescription>
        </DialogHeader>

        <div className="flex gap-2">
          <Input
            readOnly
            className="font-mono text-sm"
            value={links.share}
            onClick={(e) => (e.target as HTMLInputElement).select()}
          />
          <Button onClick={() => copy(links.share, "Link")}>
            <Copy /> Copy
          </Button>
        </div>

        <p className="text-sm text-muted-foreground">{policy}</p>

        <Button variant="link" className="h-auto justify-start p-0" onClick={() => copy(links.canonical, "Canonical link")}>
          Copy canonical link
        </Button>

        <div className="rounded-lg border border-border p-3">
          <Button
            variant="ghost"
            className="h-auto w-full justify-start p-0 hover:bg-transparent"
            aria-expanded={showOwner}
            onClick={() => setShowOwner(!showOwner)}
          >
            {showOwner ? <ChevronDown /> : <ChevronRight />}
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
                <Button variant="outline" onClick={() => copy(links.owner, "Owner link")}>
                  <Copy /> Copy
                </Button>
              </div>
            </div>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" asChild>
            <a href={openPath} target="_blank" rel="noreferrer noopener">
              Open paste
            </a>
          </Button>
          <Button onClick={onClose}>Create another</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
