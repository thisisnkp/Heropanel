import { lazy, Suspense, useEffect, useState } from "react";
import { Badge, Button, Card, Spinner, cn } from "@/components/ui";
import { can } from "@/lib/api";
import { useMe } from "@/features/auth/auth";
import type { TerminalStatus } from "@/components/WebTerminal";
import { RecordingsPanel } from "./RecordingsPanel";

// xterm.js is only needed once someone actually opens a shell, so it lives in
// its own chunk — the site workspace does not pay for it on every page load.
const WebTerminal = lazy(() =>
  import("@/components/WebTerminal").then((m) => ({ default: m.WebTerminal })),
);

// TerminalTab gives an operator an interactive shell on the site, running as the
// site's own Linux user. The session is opened lazily (on click) rather than on
// tab mount: opening a shell is an audited, privileged act, and it should be a
// deliberate one — not a side effect of clicking through tabs.
const MIN_FONT = 9;
const MAX_FONT = 24;
const FONT_KEY = "hp.terminal.fontSize";

export function TerminalTab({ uid, systemUser }: { uid: string; systemUser: string }) {
  const { data: me } = useMe();
  const canViewRecordings = can(me, "terminal.recordings.read");
  const [view, setView] = useState<"session" | "recordings">("session");
  const [started, setStarted] = useState(false);
  const [reconnectKey, setReconnectKey] = useState(0);
  const [status, setStatus] = useState<TerminalStatus>({ state: "connecting" });
  const [fullscreen, setFullscreen] = useState(false);
  // Font size is remembered: whoever needed 16px once needs it every time, and
  // re-zooming on every visit is the kind of small friction that adds up.
  const [fontSize, setFontSize] = useState(() => {
    const saved = Number(localStorage.getItem(FONT_KEY));
    return saved >= MIN_FONT && saved <= MAX_FONT ? saved : 13;
  });

  const closed = status.state === "closed";
  const zoom = (delta: number) =>
    setFontSize((f) => {
      const next = Math.min(MAX_FONT, Math.max(MIN_FONT, f + delta));
      localStorage.setItem(FONT_KEY, String(next));
      return next;
    });

  // Esc leaves fullscreen. Without it the only way out is a button that the
  // expanded terminal has just covered the rest of the page to show.
  useEffect(() => {
    if (!fullscreen) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setFullscreen(false);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [fullscreen]);

  // Recordings are a separate view rather than a separate tab: they are about
  // this terminal, and burying them elsewhere makes "what happened in that
  // session" a thing you have to already know where to look for.
  if (view === "recordings") {
    return (
      <div className="space-y-3">
        <Card className="flex flex-wrap items-center justify-between gap-3 p-3">
          <span className="text-sm font-medium text-fg">Recorded sessions</span>
          <Button variant="ghost" className="h-9 px-3" onClick={() => setView("session")}>
            Back to terminal
          </Button>
        </Card>
        <RecordingsPanel uid={uid} />
      </div>
    );
  }

  return (
    <div className={cn("space-y-3", fullscreen && "fixed inset-0 z-40 flex flex-col bg-surface p-4")}>
      <Card className="flex flex-wrap items-center justify-between gap-3 p-3">
        <div className="flex items-center gap-2 text-sm">
          <span className="text-muted">Shell as</span>
          <code className="rounded bg-surface px-1.5 py-0.5 font-mono text-xs text-fg">{systemUser}</code>
          {started && (
            <Badge>
              {status.state === "open" ? "connected" : status.state === "connecting" ? "connecting…" : "disconnected"}
            </Badge>
          )}
        </div>
        <div className="flex items-center gap-2">
          {canViewRecordings && !fullscreen && (
            <Button variant="ghost" className="h-9 px-3" onClick={() => setView("recordings")}>
              Recordings
            </Button>
          )}
          {started && (
            <>
              <div className="flex items-center gap-1">
                <Button
                  variant="ghost"
                  className="h-9 w-9 px-0"
                  aria-label="Decrease font size"
                  disabled={fontSize <= MIN_FONT}
                  onClick={() => zoom(-1)}
                >
                  A−
                </Button>
                <span className="w-8 text-center text-xs tabular-nums text-muted">{fontSize}px</span>
                <Button
                  variant="ghost"
                  className="h-9 w-9 px-0"
                  aria-label="Increase font size"
                  disabled={fontSize >= MAX_FONT}
                  onClick={() => zoom(1)}
                >
                  A+
                </Button>
              </div>
              <Button variant="ghost" className="h-9 px-3" onClick={() => setFullscreen((v) => !v)}>
                {fullscreen ? "Exit fullscreen" : "Fullscreen"}
              </Button>
            </>
          )}
          {started && (
            <Button
              variant="ghost"
              className="h-9 px-3"
              onClick={() => {
                setStatus({ state: "connecting" });
                setReconnectKey((k) => k + 1);
              }}
            >
              {closed ? "Reconnect" : "Restart session"}
            </Button>
          )}
          {started && (
            <Button
              variant="ghost"
              className="h-9 px-3"
              onClick={() => {
                setFullscreen(false);
                setStarted(false);
              }}
            >
              Close
            </Button>
          )}
        </div>
      </Card>

      {!started ? (
        <Card className="p-8">
          <div className="mx-auto max-w-md space-y-3 text-center">
            <p className="text-sm font-medium text-fg">Open a terminal on this site</p>
            <p className="text-sm text-muted">
              You get a login shell as <code className="font-mono text-xs text-fg">{systemUser}</code>, starting in the
              site's home directory. It runs with that account's privileges — not root — and the session is recorded in
              the audit log.
            </p>
            <p className="text-xs text-muted">Closing this tab or the panel ends the session and its processes.</p>
            {/* Said before the session starts, not buried in a settings page.
                Recording someone's work without telling them is not a thing this
                panel should do quietly. */}
            <p className="rounded-lg border border-border bg-surface p-3 text-left text-xs text-muted">
              <span className="font-medium text-fg">This session is recorded.</span> Terminal output and keystrokes are
              saved so an administrator can replay what was done. Anything typed while the terminal has echo off — a{" "}
              <code className="font-mono">sudo</code> or database password prompt — is replaced with{" "}
              <code className="font-mono">[redacted]</code> before it is written, so passwords are never stored.
            </p>
            <Button onClick={() => setStarted(true)}>Start session</Button>
          </div>
        </Card>
      ) : (
        <>
          <div className={cn(fullscreen ? "min-h-0 flex-1" : "h-[65vh]")}>
            <Suspense
              fallback={
                <div className="flex h-full items-center justify-center rounded-lg border border-border text-muted">
                  <Spinner /> <span className="ml-2">Loading terminal…</span>
                </div>
              }
            >
              <WebTerminal uid={uid} onStatus={setStatus} reconnectKey={reconnectKey} fontSize={fontSize} />
            </Suspense>
          </div>
          {closed && (
            <p className="text-xs text-muted">
              {status.message ?? `Session ended${status.code ? ` with exit code ${status.code}` : ""}.`} Use Reconnect to
              start a new one.
            </p>
          )}
          <p className="text-xs text-muted">
            <span className="font-medium text-fg">Shortcuts:</span> right-click for actions · Ctrl/⌘-Shift-C copy ·
            Ctrl/⌘-Shift-V paste (plain Ctrl/⌘-V works too) · Ctrl/⌘-Shift-F find · Esc leaves fullscreen. Ctrl-C is
            left alone — it still sends SIGINT to the shell.
          </p>
        </>
      )}
    </div>
  );
}
