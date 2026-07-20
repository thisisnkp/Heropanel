import { useEffect, useRef, useState } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import { useTheme } from "@/stores/theme";
import { Button, cn } from "./ui";
import type { Cast } from "@/features/recordings/recordings";

// Replays a recorded session into a real xterm.js terminal.
//
// The panel already ships xterm for the live terminal, and a recording is just
// the same byte stream with timestamps — so playback writes those bytes back
// into a Terminal instead of pulling in a second player library. That keeps the
// bundle honest and guarantees playback renders exactly as the live session did:
// same emulator, same escape-sequence handling.
//
// Input events are drawn too, dimmed and inline, because a transcript that shows
// only output leaves you guessing what was typed at a prompt that echoes
// nothing. Input recorded while the terminal was not echoing was already
// replaced with a marker server-side; it never reached this file.

const SPEEDS = [0.5, 1, 2, 4, 8];

function cssVar(name: string, fallback: string): string {
  const raw = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  return raw ? `rgb(${raw})` : fallback;
}

export function CastPlayer({ cast }: { cast: Cast }) {
  const host = useRef<HTMLDivElement | null>(null);
  const term = useRef<Terminal | null>(null);
  const fit = useRef<FitAddon | null>(null);
  const timer = useRef<number | null>(null);
  const cursor = useRef(0); // index of the next event to apply
  const themeVersion = useTheme((s) => s.theme);

  const [playing, setPlaying] = useState(true);
  const [speed, setSpeed] = useState(1);
  const [elapsed, setElapsed] = useState(0);

  // Build the terminal at the size the session was recorded at, so the replay
  // wraps where the original did.
  useEffect(() => {
    if (!host.current) return;
    const t = new Terminal({
      fontSize: 13,
      fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
      cursorBlink: false,
      disableStdin: true, // a recording is not interactive
      scrollback: 5000,
      cols: cast.header.width,
      rows: cast.header.height,
      theme: {
        background: cssVar("--panel", "#11161f"),
        foreground: cssVar("--fg", "#e2e8f0"),
      },
      allowProposedApi: true,
    });
    const f = new FitAddon();
    t.loadAddon(f);
    t.open(host.current);
    try {
      f.fit();
    } catch {
      /* zero-sized container mid-layout */
    }
    term.current = t;
    fit.current = f;

    const ro = new ResizeObserver(() => {
      try {
        f.fit();
      } catch {
        /* ignore */
      }
    });
    ro.observe(host.current);

    return () => {
      ro.disconnect();
      t.dispose();
      term.current = null;
      fit.current = null;
    };
  }, [cast]);

  useEffect(() => {
    if (term.current) term.current.options.theme = { background: cssVar("--panel", "#11161f"), foreground: cssVar("--fg", "#e2e8f0") };
  }, [themeVersion]);

  // Apply one event to the terminal.
  const apply = (i: number) => {
    const t = term.current;
    const e = cast.events[i];
    if (!t || !e) return;
    switch (e.kind) {
      case "o":
        t.write(e.data);
        break;
      case "i":
        // Dimmed, so it reads as "this was typed" rather than as program output.
        t.write(`\x1b[2m${e.data}\x1b[0m`);
        break;
      case "r": {
        const [c, r] = e.data.split("x").map((n) => parseInt(n, 10));
        if (c > 0 && r > 0) t.resize(c, r);
        break;
      }
    }
  };

  // The playback loop. Each step schedules the next event at its own recorded
  // gap, so pauses in the original session are reproduced rather than collapsed —
  // watching someone think is part of reading a session.
  useEffect(() => {
    if (!playing) {
      if (timer.current) window.clearTimeout(timer.current);
      return;
    }
    let cancelled = false;

    const step = () => {
      if (cancelled) return;
      const i = cursor.current;
      if (i >= cast.events.length) {
        setPlaying(false);
        return;
      }
      apply(i);
      cursor.current = i + 1;
      setElapsed(cast.events[i].time);

      const next = cast.events[i + 1];
      if (!next) {
        setPlaying(false);
        return;
      }
      // Long idle gaps are capped: nobody wants to sit through a two-minute
      // pause, and the seek bar still shows where the time went.
      const gap = Math.min(next.time - cast.events[i].time, 2);
      timer.current = window.setTimeout(step, Math.max(0, (gap * 1000) / speed));
    };

    step();
    return () => {
      cancelled = true;
      if (timer.current) window.clearTimeout(timer.current);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [playing, speed, cast]);

  // Seeking replays from the start rather than trying to reverse the terminal:
  // a terminal's state is the accumulation of every escape sequence before it,
  // so "rewind" only has one correct meaning.
  const seek = (to: number) => {
    const t = term.current;
    if (!t) return;
    setPlaying(false);
    t.reset();
    let i = 0;
    while (i < cast.events.length && cast.events[i].time <= to) {
      apply(i);
      i++;
    }
    cursor.current = i;
    setElapsed(to);
  };

  const restart = () => {
    seek(0);
    cursor.current = 0;
    setElapsed(0);
    setPlaying(true);
  };

  const finished = cursor.current >= cast.events.length;

  return (
    <div className="flex h-full flex-col gap-2">
      <div ref={host} className="min-h-0 flex-1 overflow-hidden rounded-lg border border-border bg-panel p-2" />

      <div className="flex flex-wrap items-center gap-3">
        <Button
          className="h-9 w-24 px-3"
          onClick={() => (finished ? restart() : setPlaying((p) => !p))}
        >
          {finished ? "Replay" : playing ? "Pause" : "Play"}
        </Button>

        <input
          type="range"
          min={0}
          max={Math.max(cast.duration, 0.001)}
          step={0.01}
          value={elapsed}
          onChange={(e) => seek(Number(e.target.value))}
          aria-label="Seek"
          className="h-1.5 min-w-[8rem] flex-1 cursor-pointer appearance-none rounded-full bg-border accent-brand"
        />

        <span className="w-24 text-right text-xs tabular-nums text-muted">
          {elapsed.toFixed(1)}s / {cast.duration.toFixed(1)}s
        </span>

        <div className="flex items-center gap-1">
          {SPEEDS.map((s) => (
            <button
              key={s}
              onClick={() => setSpeed(s)}
              className={cn(
                "rounded border border-border px-2 py-1 text-xs",
                speed === s ? "bg-brand text-brand-fg" : "text-muted hover:bg-border/50 hover:text-fg",
              )}
            >
              {s}×
            </button>
          ))}
        </div>
      </div>

      <p className="text-xs text-muted">
        Dimmed text is what was typed. Input entered while the terminal had echo off — a password prompt — was replaced
        with <code className="font-mono">[redacted]</code> before the recording was written, so it was never stored.
      </p>
    </div>
  );
}
