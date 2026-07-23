import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useSites } from "@/features/sites/sites";
import { useMe } from "@/features/auth/auth";
import { can } from "@/lib/api";
import { cn } from "./ui";

interface Command {
  id: string;
  label: string;
  hint?: string;
  run: () => void;
}

// CommandPalette is the ⌘K launcher. It merges a static set of navigation and
// action commands with dynamic ones (jump to a specific site), filters as you
// type, and runs on Enter. It is the Topbar's "⌘K" placeholder made real.
export function CommandPalette() {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [sel, setSel] = useState(0);
  const navigate = useNavigate();
  const inputRef = useRef<HTMLInputElement>(null);
  const { data: sites } = useSites();
  const { data: me } = useMe();

  // Global ⌘K / Ctrl+K toggle.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setOpen((o) => !o);
      }
      if (e.key === "Escape") setOpen(false);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  useEffect(() => {
    if (open) {
      setQuery("");
      setSel(0);
      setTimeout(() => inputRef.current?.focus(), 0);
    }
  }, [open]);

  const commands = useMemo<Command[]>(() => {
    const go = (to: string) => () => {
      navigate(to);
      setOpen(false);
    };
    const nav: Command[] = [
      { id: "dashboard", label: "Go to Dashboard", run: go("/") },
      { id: "sites", label: "Go to Websites", run: go("/sites") },
      { id: "databases", label: "Go to Databases", run: go("/databases") },
      { id: "dns", label: "Go to DNS", run: go("/dns") },
      { id: "ssl", label: "Go to SSL certificates", run: go("/ssl") },
      { id: "audit", label: "Go to Audit log", run: go("/audit") },
      // Mirrors the sidebar: offered only to whoever may actually read them.
      ...(can(me, "terminal.recordings.read")
        ? [{ id: "recordings", label: "Go to Session recordings", run: go("/recordings") }]
        : []),
      ...(can(me, "docker.read")
        ? [
            { id: "docker", label: "Go to Docker", run: go("/docker") },
            { id: "apps", label: "Go to one-click Apps", run: go("/apps") },
          ]
        : []),
      ...(can(me, "mail.read") ? [{ id: "mail", label: "Go to Mail", run: go("/mail") }] : []),
      ...(can(me, "monitor.read")
        ? [{ id: "monitor", label: "Go to Monitoring", run: go("/monitor") }]
        : []),
      { id: "modules", label: "Go to Modules", run: go("/modules") },
      { id: "users", label: "Go to Users", run: go("/users") },
      { id: "new-site", label: "Create a website", hint: "action", run: go("/sites?new=1") },
    ];
    const siteJumps: Command[] = (sites ?? []).map((s) => ({
      id: `site-${s.uid}`,
      label: s.primary_domain,
      hint: "website",
      run: go(`/sites/${s.uid}`),
    }));
    return [...nav, ...siteJumps];
  }, [navigate, sites, me]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return commands.slice(0, 12);
    return commands.filter((c) => c.label.toLowerCase().includes(q)).slice(0, 12);
  }, [commands, query]);

  useEffect(() => setSel(0), [query]);

  if (!open) return null;

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setSel((s) => Math.min(s + 1, filtered.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setSel((s) => Math.max(s - 1, 0));
    } else if (e.key === "Enter") {
      e.preventDefault();
      filtered[sel]?.run();
    }
  };

  return (
    <div className="fixed inset-0 z-[70] grid place-items-start justify-center bg-black/40 p-4 pt-[15vh]" onClick={() => setOpen(false)}>
      <div
        className="w-full max-w-lg overflow-hidden rounded-xl border border-border bg-panel shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <input
          ref={inputRef}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={onKeyDown}
          placeholder="Search pages, sites, actions…"
          className="h-12 w-full border-b border-border bg-transparent px-4 text-sm text-fg placeholder:text-muted focus:outline-none"
        />
        <ul className="max-h-80 overflow-auto py-1">
          {filtered.length === 0 && <li className="px-4 py-6 text-center text-sm text-muted">No matches.</li>}
          {filtered.map((c, i) => (
            <li key={c.id}>
              <button
                onMouseEnter={() => setSel(i)}
                onClick={c.run}
                className={cn(
                  "flex w-full items-center justify-between px-4 py-2.5 text-left text-sm",
                  i === sel ? "bg-brand/15 text-fg" : "text-muted",
                )}
              >
                <span>{c.label}</span>
                {c.hint && <span className="text-xs text-muted">{c.hint}</span>}
              </button>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}
