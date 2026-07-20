import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, rawFetch, type TerminalRecording } from "@/lib/api";

export type { TerminalRecording };

// Data hooks and the asciicast parser for recorded terminal sessions.
//
// The cast itself is fetched as text rather than JSON: asciicast v2 is
// newline-delimited JSON (a header object, then one array per event), which is
// not a single JSON document.

export function useRecordings(siteUID: string) {
  return useQuery({
    queryKey: ["recordings", "site", siteUID],
    queryFn: () => api.get<TerminalRecording[]>(`/sites/${siteUID}/terminal/recordings`),
  });
}

// PageSize is how much history the cross-site view holds at once. It is also
// the server's cap, and the page says so when it is hit: a filter that quietly
// searches only the newest slice would tell an auditor "no such session" about a
// session that exists.
export const PageSize = 200;

export function useAllRecordings(offset = 0) {
  return useQuery({
    queryKey: ["recordings", "all", offset],
    queryFn: () =>
      api.get<TerminalRecording[]>(`/terminal/recordings?limit=${PageSize}&offset=${offset}`),
  });
}

// Deleting invalidates every recordings query, not just the one on screen: the
// same recording is listed by both the site tab and the cross-site page, and a
// row that reappears when you navigate back looks like the delete failed.
export function useDeleteRecording() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (rid: string) => api.del(`/terminal/recordings/${rid}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["recordings"] }),
  });
}

// ── asciicast v2 ─────────────────────────────────────────────────────────────

export interface CastHeader {
  version: number;
  width: number;
  height: number;
  timestamp?: number;
}

/** One event: seconds since the session started, its kind, and its payload. */
export interface CastEvent {
  time: number;
  kind: "o" | "i" | "r";
  data: string;
}

export interface Cast {
  header: CastHeader;
  events: CastEvent[];
  /** Total length in seconds (the last event's offset). */
  duration: number;
}

export async function fetchCast(rid: string): Promise<Cast> {
  const res = await rawFetch("GET", `/terminal/recordings/${rid}/cast`);
  return parseCast(await res.text());
}

// parseCast reads the newline-delimited format. A malformed line is skipped
// rather than failing the whole recording: a transcript that stops being
// readable because of one bad line is worse than one with a gap, and a
// truncated recording legitimately ends mid-line.
export function parseCast(text: string): Cast {
  const lines = text.split("\n").filter((l) => l.trim() !== "");
  if (lines.length === 0) {
    return { header: { version: 2, width: 80, height: 24 }, events: [], duration: 0 };
  }

  let header: CastHeader = { version: 2, width: 80, height: 24 };
  try {
    const parsed = JSON.parse(lines[0]) as Partial<CastHeader>;
    header = {
      version: parsed.version ?? 2,
      width: parsed.width || 80,
      height: parsed.height || 24,
      timestamp: parsed.timestamp,
    };
  } catch {
    /* no usable header: fall back to a standard-sized terminal */
  }

  const events: CastEvent[] = [];
  for (const line of lines.slice(1)) {
    try {
      const e = JSON.parse(line) as [number, string, string];
      if (!Array.isArray(e) || e.length < 3) continue;
      const kind = e[1];
      if (kind !== "o" && kind !== "i" && kind !== "r") continue;
      events.push({ time: Number(e[0]) || 0, kind, data: String(e[2]) });
    } catch {
      continue;
    }
  }
  return { header, events, duration: events.length ? events[events.length - 1].time : 0 };
}

// parseServerTime reads the panel's stored timestamp layout
// ("2006-01-02 15:04:05", always UTC). Handing that string straight to
// `new Date()` would read it as *local* time, which silently shifts every
// recording's start by the viewer's offset — not something an audit artifact can
// afford to be casually wrong about.
export function parseServerTime(s: string): Date {
  if (!s) return new Date(NaN);
  const iso = s.includes("T") ? s : s.replace(" ", "T");
  return new Date(/[zZ]|[+-]\d\d:?\d\d$/.test(iso) ? iso : `${iso}Z`);
}

/** formatDuration renders milliseconds as m:ss, the shape a player needs. */
export function formatDuration(ms: number): string {
  const total = Math.max(0, Math.round(ms / 1000));
  const m = Math.floor(total / 60);
  const s = total % 60;
  return `${m}:${String(s).padStart(2, "0")}`;
}
