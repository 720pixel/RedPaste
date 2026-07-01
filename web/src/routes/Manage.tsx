import type { ReactNode } from "react";
import { useEffect, useState } from "react";
import { getStatus, deletePaste, type OwnerStatus } from "@/api/client";
import { currentFragment } from "@/crypto/fragment";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { toast } from "@/lib/toast";

type State =
  | { phase: "loading" }
  | { phase: "error"; message: string }
  | { phase: "ready"; status: OwnerStatus }
  | { phase: "deleted" };

export function Manage({ slug }: { slug: string }) {
  const [state, setState] = useState<State>({ phase: "loading" });
  const [confirm, setConfirm] = useState(false);
  const frag = currentFragment();
  const token = frag?.kind === "O" ? frag.secret : null;

  useEffect(() => {
    let cancelled = false;
    (async () => {
      if (!token) {
        setState({ phase: "error", message: "This owner link is missing its capability." });
        return;
      }
      try {
        const status = await getStatus(slug, token);
        if (!cancelled) setState({ phase: "ready", status });
      } catch {
        if (!cancelled)
          setState({ phase: "error", message: "Couldn't load status. The owner link may be wrong or the paste is gone." });
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [slug, token]);

  async function doDelete() {
    if (!token) return;
    try {
      await deletePaste(slug, token);
      setConfirm(false);
      setState({ phase: "deleted" });
      toast("Paste deleted", "success");
    } catch {
      toast("Delete failed", "error");
    }
  }

  if (state.phase === "loading") return <div className="py-20 text-center text-muted-foreground">Loading…</div>;
  if (state.phase === "error") {
    return (
      <div className="mx-auto max-w-2xl space-y-4 py-16 text-center">
        <h1 className="text-3xl font-bold">Owner controls</h1>
        <p className="text-muted-foreground">{state.message}</p>
        <Button asChild>
          <a href="/">Back to RedPaste</a>
        </Button>
      </div>
    );
  }
  if (state.phase === "deleted") {
    return (
      <div className="mx-auto max-w-2xl space-y-4 py-16 text-center">
        <h1 className="text-3xl font-bold">Deleted</h1>
        <p className="text-muted-foreground">This paste has been revoked and will be purged.</p>
        <Button asChild>
          <a href="/">Create a new paste</a>
        </Button>
      </div>
    );
  }

  const s = state.status;
  const stateVariant = s.state === "active" ? "success" : "destructive";
  return (
    <div className="mx-auto max-w-2xl space-y-5">
      <h1 className="text-3xl font-bold">Owner controls</h1>
      <Card>
        <CardContent className="divide-y divide-border p-0">
          <Row label="Status" value={<Badge variant={stateVariant}>{s.state}</Badge>} />
          <Row label="Created" value={new Date(s.createdAt).toLocaleString()} />
          <Row label="Expires" value={new Date(s.expiresAt).toLocaleString()} />
          <Row label="Burn after read" value={s.burnAfterRead ? "yes" : "no"} />
          <Row label="View limit" value={s.maxViews == null ? "unlimited" : String(s.maxViews)} />
          <Row label="Successful views" value={String(s.successfulViews)} />
          <Row label="Last viewed" value={s.lastViewedAt ? new Date(s.lastViewedAt).toLocaleString() : "never"} />
        </CardContent>
      </Card>
      <Button variant="destructive" onClick={() => setConfirm(true)}>
        Delete paste
      </Button>

      <Dialog open={confirm} onOpenChange={setConfirm}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete this paste?</DialogTitle>
            <DialogDescription>
              This revokes the paste immediately. Anyone holding the share link will no longer be able to read it. This
              can't be undone.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirm(false)}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={doDelete}>
              Delete permanently
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function Row({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="flex items-center justify-between px-5 py-3">
      <span className="text-sm text-muted-foreground">{label}</span>
      <span className="text-sm">{value}</span>
    </div>
  );
}
