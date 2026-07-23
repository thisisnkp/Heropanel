import { useCallback, useEffect, useRef, useState } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { SearchAddon } from "@xterm/addon-search";
import { WebLinksAddon } from "@xterm/addon-web-links";
import "@xterm/xterm/css/xterm.css";
import { useTheme } from "@/stores/theme";
import { toast } from "@/stores/toast";
import { ContextMenu, type MenuItems, useContextMenu } from "./ContextMenu";
import { cn } from "./ui";

// WebTerminal is the xterm.js front end for a site's PTY session.
//
// Wire shape (mirrors internal/httpapi/terminal_handlers.go): terminal bytes are
// **binary** frames in both directions — PTY output is arbitrary bytes and a read
// can land mid-UTF-8-sequence, so it must not be squeezed through a JSON string.
// Control messages (resize out, exit/error in) are JSON **text** frames. xterm
// buffers partial escape and UTF-8 sequences itself, so frame boundaries are safe.
//
// xterm styles elements through the CSSOM (`el.style.x = …`), not by parsing
// style attributes, so it runs under the app's strict `default-src 'self'` CSP
// without needing `'unsafe-inline'`. Its stylesheet is bundled by Vite as a
// same-origin <link>.

export type TerminalStatus =
  | { state: "connecting" }
  | { state: "open" }
  | { state: "closed"; code?: number; message?: string };

// cssVar reads one of the app's design tokens (stored as an "R G B" triple) and
// returns it as a CSS color, so the terminal inherits the panel's palette
// instead of maintaining a second one.
function cssVar(name: string, fallback: string): string {
  const raw = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  return raw ? `rgb(${raw})` : fallback;
}

function buildTheme() {
  const fg = cssVar("--fg", "#e2e8f0");
  const bg = cssVar("--panel", "#11161f");
  return {
    background: bg,
    foreground: fg,
    cursor: fg,
    cursorAccent: bg,
    selectionBackground: cssVar("--brand", "#818cf8") + "59", // ~35% alpha
  };
}

export function WebTerminal({
  uid,
  cwd = "",
  endpoint,
  wsQuery,
  readOnly = false,
  onStatus,
  reconnectKey = 0,
  fontSize = 13,
}: {
  uid: string;
  cwd?: string;
  /**
   * API path to upgrade against, when this is not a site terminal. A container
   * shell speaks the identical wire protocol — the same binary frames and the
   * same JSON control frames — so it reuses this component rather than growing a
   * second emulator that would drift in its key handling and resize behaviour.
   */
  endpoint?: string;
  /**
   * Extra query parameters merged into the upgrade URL. A log follow carries its
   * `tail` here, kept separate from `endpoint` so the component still owns cols/
   * rows and there is never a second "?" in the URL.
   */
  wsQuery?: Record<string, string>;
  /**
   * A one-way stream (a log follow) has nothing to type into. readOnly stops
   * keystrokes being forwarded, so the pane is a live viewer rather than a shell.
   */
  readOnly?: boolean;
  onStatus?: (s: TerminalStatus) => void;
  /** Change this to force a fresh session (the Reconnect button). */
  reconnectKey?: number;
  fontSize?: number;
}) {
  const host = useRef<HTMLDivElement | null>(null);
  const term = useRef<Terminal | null>(null);
  const fit = useRef<FitAddon | null>(null);
  const searcher = useRef<SearchAddon | null>(null);
  const refit = useRef<() => void>(() => {});
  const themeVersion = useTheme((s) => s.theme);
  const statusRef = useRef(onStatus);
  statusRef.current = onStatus;

  const [searchOpen, setSearchOpen] = useState(false);
  const [query, setQuery] = useState("");
  const searchInput = useRef<HTMLInputElement | null>(null);
  const menu = useContextMenu<null>();

  // Copy and paste are explicit here because a terminal cannot use the usual
  // shortcuts: Ctrl+C is SIGINT and has to stay that way. Ctrl+Shift+C/V are the
  // terminal convention. Plain Ctrl+V still works — xterm's hidden textarea
  // receives the browser's native paste event — so the clipboard API below is
  // only the path for the shifted shortcut and the menu.
  const copySelection = useCallback(async () => {
    const t = term.current;
    const text = t?.getSelection();
    if (!text) {
      toast.error("Nothing selected", "Drag to select text in the terminal first.");
      return;
    }
    try {
      await navigator.clipboard.writeText(text);
      toast.success("Copied to clipboard");
    } catch {
      toast.error("Could not access the clipboard", "Your browser blocked it; the selection is still highlighted.");
    }
  }, []);

  const pasteFromClipboard = useCallback(async () => {
    const t = term.current;
    if (!t) return;
    try {
      const text = await navigator.clipboard.readText();
      if (text) t.paste(text);
    } catch {
      toast.error("Could not read the clipboard", "Your browser blocked it — Ctrl/⌘-V pastes directly.");
    }
  }, []);

  useEffect(() => {
    if (!host.current) return;

    const t = new Terminal({
      fontSize,
      fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
      cursorBlink: true,
      // A generous scrollback: the whole point of a terminal is reading output
      // that has already gone past.
      scrollback: 5000,
      theme: buildTheme(),
      allowProposedApi: true,
    });
    const f = new FitAddon();
    const s = new SearchAddon();
    t.loadAddon(f);
    t.loadAddon(s);
    t.loadAddon(new WebLinksAddon());
    t.open(host.current);
    f.fit();
    term.current = t;
    fit.current = f;
    searcher.current = s;

    const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
    const params = new URLSearchParams({
      cols: String(t.cols),
      rows: String(t.rows),
    });
    if (cwd) params.set("cwd", cwd);
    for (const [k, v] of Object.entries(wsQuery ?? {})) params.set(k, v);
    const path = endpoint ?? `/sites/${uid}/terminal`;
    const ws = new WebSocket(`${proto}//${window.location.host}/api/v1${path}?${params}`);
    ws.binaryType = "arraybuffer";

    const encoder = new TextEncoder();
    statusRef.current?.({ state: "connecting" });

    ws.onopen = () => {
      statusRef.current?.({ state: "open" });
      t.focus();
    };

    ws.onmessage = (ev) => {
      if (typeof ev.data === "string") {
        // JSON control frame.
        try {
          const c = JSON.parse(ev.data) as { type: string; exit_code?: number; message?: string };
          if (c.type === "exit") {
            statusRef.current?.({ state: "closed", code: c.exit_code });
            t.write(`\r\n\x1b[90m[session ended${c.exit_code ? ` — exit ${c.exit_code}` : ""}]\x1b[0m\r\n`);
          } else if (c.type === "error") {
            statusRef.current?.({ state: "closed", message: c.message });
            t.write(`\r\n\x1b[31m[${c.message ?? "session failed"}]\x1b[0m\r\n`);
          }
        } catch {
          /* ignore malformed control frames */
        }
        return;
      }
      t.write(new Uint8Array(ev.data as ArrayBuffer));
    };

    ws.onclose = (ev) => {
      statusRef.current?.({
        state: "closed",
        message: ev.code === 1000 ? undefined : ev.reason || undefined,
      });
    };
    ws.onerror = () => statusRef.current?.({ state: "closed", message: "Connection failed." });

    // Shortcuts the *panel* owns rather than the shell. Returning false stops
    // xterm from also forwarding the keystroke to the PTY, which is what keeps
    // Ctrl+Shift+C from reaching the shell as a control character.
    t.attachCustomKeyEventHandler((e) => {
      if (e.type !== "keydown") return true;
      const mod = e.ctrlKey || e.metaKey;
      if (mod && e.shiftKey) {
        switch (e.key.toLowerCase()) {
          case "c":
            void copySelection();
            return false;
          case "v":
            void pasteFromClipboard();
            return false;
          case "f":
            setSearchOpen(true);
            // Focus after the input exists.
            setTimeout(() => searchInput.current?.focus(), 0);
            return false;
        }
      }
      // Shift+Insert is the other long-standing paste binding on Linux.
      if (e.shiftKey && e.key === "Insert") {
        void pasteFromClipboard();
        return false;
      }
      return true;
    });

    // Keystrokes → PTY, as binary. A read-only viewer (a log follow) forwards
    // nothing: there is no process on the other end to receive input.
    const dataSub = readOnly
      ? { dispose() {} }
      : t.onData((d) => {
          if (ws.readyState === WebSocket.OPEN) ws.send(encoder.encode(d));
        });

    // Window size → PTY, as a JSON control frame. Debounced via rAF so a drag
    // does not flood the socket with one resize per pixel.
    let raf = 0;
    const sendResize = () => {
      cancelAnimationFrame(raf);
      raf = requestAnimationFrame(() => {
        try {
          f.fit();
        } catch {
          return; // the container can be zero-sized mid-layout
        }
        if (ws.readyState === WebSocket.OPEN) {
          ws.send(JSON.stringify({ type: "resize", cols: t.cols, rows: t.rows }));
        }
      });
    };
    refit.current = sendResize;
    const ro = new ResizeObserver(sendResize);
    ro.observe(host.current);
    window.addEventListener("resize", sendResize);

    return () => {
      window.removeEventListener("resize", sendResize);
      cancelAnimationFrame(raf);
      ro.disconnect();
      dataSub.dispose();
      // Closing the socket is what tells the broker to kill the shell.
      ws.close();
      t.dispose();
      term.current = null;
      fit.current = null;
      searcher.current = null;
      refit.current = () => {};
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [uid, cwd, endpoint, readOnly, reconnectKey]);

  // Follow the app's light/dark toggle without rebuilding the session.
  useEffect(() => {
    if (term.current) term.current.options.theme = buildTheme();
  }, [themeVersion]);

  // A font-size change alters how many columns fit, so the PTY has to be told —
  // otherwise the shell keeps line-wrapping at the old width.
  useEffect(() => {
    if (!term.current) return;
    term.current.options.fontSize = fontSize;
    refit.current();
  }, [fontSize]);

  const find = (dir: "next" | "prev") => {
    if (!query || !searcher.current) return;
    const found =
      dir === "next"
        ? searcher.current.findNext(query, { incremental: false })
        : searcher.current.findPrevious(query, { incremental: false });
    if (!found) toast.error("No matches", `“${query}” is not in the scrollback.`);
  };

  const closeSearch = () => {
    setSearchOpen(false);
    searcher.current?.clearDecorations();
    term.current?.focus();
  };

  const menuItems = (): MenuItems => [
    { label: "Copy", shortcut: "Ctrl+Shift+C", onSelect: () => void copySelection() },
    { label: "Paste", shortcut: "Ctrl+Shift+V", onSelect: () => void pasteFromClipboard() },
    { label: "Select all", separatorBefore: true, onSelect: () => term.current?.selectAll() },
    {
      label: "Find…",
      shortcut: "Ctrl+Shift+F",
      onSelect: () => {
        setSearchOpen(true);
        setTimeout(() => searchInput.current?.focus(), 0);
      },
    },
    {
      label: "Clear scrollback",
      separatorBefore: true,
      onSelect: () => term.current?.clear(),
    },
  ];

  return (
    <div className="flex h-full w-full flex-col overflow-hidden rounded-lg border border-border bg-panel">
      {searchOpen && (
        <form
          className="flex items-center gap-2 border-b border-border px-2 py-1.5"
          onSubmit={(e) => {
            e.preventDefault();
            find("next");
          }}
        >
          <input
            ref={searchInput}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Escape") {
                e.preventDefault();
                closeSearch();
              }
            }}
            placeholder="Find in scrollback…"
            className="h-7 flex-1 rounded border border-border bg-surface px-2 text-xs text-fg outline-none focus:border-brand"
          />
          <TermButton onClick={() => find("prev")} label="Previous match">
            ↑
          </TermButton>
          <TermButton onClick={() => find("next")} label="Next match">
            ↓
          </TermButton>
          <TermButton onClick={closeSearch} label="Close find">
            ✕
          </TermButton>
        </form>
      )}
      <div ref={host} className="min-h-0 flex-1 p-2" onContextMenu={(e) => menu.open(e, null)} />
      {menu.menu && <ContextMenu x={menu.menu.x} y={menu.menu.y} items={menuItems()} onClose={menu.close} />}
    </div>
  );
}

function TermButton({
  children,
  label,
  onClick,
}: {
  children: React.ReactNode;
  label: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      title={label}
      aria-label={label}
      onClick={onClick}
      className={cn("grid h-7 w-7 place-items-center rounded border border-border text-xs text-muted hover:bg-border/50 hover:text-fg")}
    >
      {children}
    </button>
  );
}
