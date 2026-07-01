import { useEffect, useState } from "react";
import { resolveShort } from "@/api/client";
import { Button } from "@/components/ui/button";

// /s/{code} resolver. We ask the server for the internal path, then replace the
// URL keeping the fragment intact; the secret after '#' never hits the server.
export function Resolve({ code }: { code: string }) {
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const path = await resolveShort(code);
        if (cancelled) return;
        window.location.replace(path + window.location.hash);
      } catch {
        if (!cancelled) setFailed(true);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [code]);

  if (failed) {
    return (
      <div className="mx-auto max-w-2xl space-y-4 py-16 text-center">
        <h1 className="text-3xl font-bold">Unavailable</h1>
        <p className="text-muted-foreground">This link has expired, been deleted, or never existed.</p>
        <Button asChild>
          <a href="/">Create a new paste</a>
        </Button>
      </div>
    );
  }
  return <div className="py-20 text-center text-muted-foreground">Opening…</div>;
}
