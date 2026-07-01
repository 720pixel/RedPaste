import { Suspense, lazy, useEffect, useState } from "react";
import { useRoute, useLinkInterception } from "./router";
import { getThemePref, setThemePref, applyTheme, watchSystemTheme, type ThemePref } from "./theme";
import { WideLayoutContext } from "./layout";
import { ThemeToggle } from "@/components/ThemeToggle";
import { Toaster } from "@/components/ui/sonner";
import { Button } from "@/components/ui/button";
import { Create } from "@/routes/Create";

const View = lazy(() => import("@/routes/View").then((m) => ({ default: m.View })));
const Resolve = lazy(() => import("@/routes/Resolve").then((m) => ({ default: m.Resolve })));
const Manage = lazy(() => import("@/routes/Manage").then((m) => ({ default: m.Manage })));
const Diff = lazy(() => import("@/routes/Diff").then((m) => ({ default: m.Diff })));
const LiveStart = lazy(() => import("@/routes/Live").then((m) => ({ default: m.LiveStart })));
const LiveRoom = lazy(() => import("@/routes/Live").then((m) => ({ default: m.LiveRoom })));
const Security = lazy(() => import("@/routes/static").then((m) => ({ default: m.Security })));
const Privacy = lazy(() => import("@/routes/static").then((m) => ({ default: m.Privacy })));
const NotFound = lazy(() => import("@/routes/static").then((m) => ({ default: m.NotFound })));

const SECRET_PREFIXES = ["/p/", "/live/", "/s/", "/manage/"];
const isSecret = (path: string) => SECRET_PREFIXES.some((p) => path.startsWith(p));

function setRobotsMeta(noindex: boolean) {
  let tag = document.querySelector('meta[name="robots"]') as HTMLMetaElement | null;
  if (noindex) {
    if (!tag) {
      tag = document.createElement("meta");
      tag.name = "robots";
      document.head.appendChild(tag);
    }
    tag.content = "noindex,nofollow,noarchive,nosnippet,noimageindex";
  } else if (tag) {
    tag.remove();
  }
}

function segment(path: string, prefix: string): string {
  if (!path.startsWith(prefix)) return "";
  return decodeURIComponent(path.slice(prefix.length).split("/")[0] ?? "");
}

export function App() {
  const path = useRoute();
  useLinkInterception();
  const [pref, setPref] = useState<ThemePref>(getThemePref());
  const [wide, setWide] = useState(false);

  useEffect(() => watchSystemTheme(() => applyTheme("system")), []);
  useEffect(() => setRobotsMeta(isSecret(path)), [path]);
  useEffect(() => setWide(false), [path]);

  const changeTheme = (p: ThemePref) => {
    setPref(p);
    setThemePref(p);
  };

  return (
    <div className="flex min-h-dvh flex-col">
      <header className="sticky top-0 z-40 border-b border-border bg-background/80 backdrop-blur supports-[backdrop-filter]:bg-background/70">
        <div className="mx-auto flex h-14 max-w-[1120px] items-center justify-between px-5">
          <a href="/" aria-label="RedPaste home" className="flex items-center gap-2.5">
            <svg viewBox="0 0 32 32" className="size-7">
              <rect width="32" height="32" rx="8" className="fill-primary" />
              <path d="M21 3.5 L28.5 11 H23 a2 2 0 0 1-2-2 Z" fill="#000" opacity="0.22" />
              <rect x="9" y="12" width="14" height="2.6" rx="1.3" fill="#fff" />
              <rect x="9" y="17.4" width="11" height="2.6" rx="1.3" fill="#fff" />
              <rect x="9" y="22.8" width="7" height="2.6" rx="1.3" fill="#fff" opacity="0.75" />
            </svg>
            <span className="font-display text-[17px] font-bold uppercase tracking-[0.05em]">
              Red<span className="text-primary">Paste</span>
            </span>
          </a>
          <nav className="flex items-center gap-2">
            <Button variant="ghost" asChild><a href="/live">Live</a></Button>
            <Button variant="ghost" asChild><a href="/diff">Diff</a></Button>
            <Button variant="ghost" asChild className="max-sm:hidden"><a href="/security">Security</a></Button>
            <ThemeToggle pref={pref} onChange={changeTheme} />
          </nav>
        </div>
      </header>

      <main className={wide ? "w-full flex-1 px-4 py-8 sm:px-6" : "mx-auto w-full max-w-[1120px] flex-1 px-5 py-8"}>
        <WideLayoutContext.Provider value={setWide}>
          <Suspense fallback={<div className="py-20 text-center text-muted-foreground">Loading…</div>}>
            <Route path={path} />
          </Suspense>
        </WideLayoutContext.Provider>
      </main>

      <footer className="border-t border-border py-6 text-center text-[13px] text-muted-foreground">
        <div className="flex flex-wrap justify-center gap-4">
          <a href="/security" className="hover:text-foreground">Security</a>
          <a href="/privacy" className="hover:text-foreground">Privacy</a>
          <a href="https://github.com/720pixel/RedPaste" target="_blank" rel="noreferrer noopener" className="hover:text-foreground">
            GitHub
          </a>
          <span>Encrypted in your browser · No accounts · No trackers</span>
        </div>
      </footer>

      <Toaster />
    </div>
  );
}

function Route({ path }: { path: string }) {
  if (path === "/") return <Create />;
  if (path === "/live") return <LiveStart />;
  if (path === "/diff") return <Diff />;
  if (path === "/security") return <Security />;
  if (path === "/privacy") return <Privacy />;

  const p = segment(path, "/p/");
  if (p) return <View slug={p} />;
  const s = segment(path, "/s/");
  if (s) return <Resolve code={s} />;
  const m = segment(path, "/manage/");
  if (m) return <Manage slug={m} />;
  const live = segment(path, "/live/");
  if (live) return <LiveRoom slug={live} />;

  return <NotFound />;
}
