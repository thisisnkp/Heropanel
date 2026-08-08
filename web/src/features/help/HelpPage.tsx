import { useMemo, useState } from "react";
import { Alert, Card, EmptyState, Input, Spinner } from "@/components/ui";
import { useOpenApiSpec } from "./help";
import { countEndpoints, filterEndpoints, groupEndpoints, type ApiEndpoint } from "./openapi";

// HelpPage is the in-app help centre: how to get around, and a live reference of
// the panel's REST API rendered straight from the OpenAPI document hpd serves —
// so the reference always matches the running server rather than a doc that
// drifts. No auth is needed to read it; the API surface is public knowledge, and
// each endpoint shows the permission it actually requires.
export function HelpPage() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold text-fg">Help</h1>
        <p className="text-sm text-muted">Getting around, and a live reference for the panel's API.</p>
      </div>
      <ShortcutsCard />
      <ApiReference />
    </div>
  );
}

const SHORTCUTS: { keys: string[]; label: string }[] = [
  { keys: ["⌘/Ctrl", "K"], label: "Open the command palette" },
  { keys: ["Esc"], label: "Close a dialog or the command palette" },
  { keys: ["Tab"], label: "Reveal “Skip to content” from anywhere" },
];

function ShortcutsCard() {
  return (
    <Card className="overflow-hidden">
      <div className="border-b border-border px-4 py-3">
        <h2 className="text-sm font-semibold text-fg">Keyboard shortcuts</h2>
        <p className="text-xs text-muted">Move around the panel without reaching for the mouse.</p>
      </div>
      <ul className="divide-y divide-border/60">
        {SHORTCUTS.map((s) => (
          <li key={s.label} className="flex items-center justify-between px-4 py-2.5 text-sm">
            <span className="text-fg">{s.label}</span>
            <span className="flex items-center gap-1">
              {s.keys.map((k) => (
                <kbd
                  key={k}
                  className="rounded border border-border bg-surface px-1.5 py-0.5 font-mono text-xs text-muted"
                >
                  {k}
                </kbd>
              ))}
            </span>
          </li>
        ))}
      </ul>
    </Card>
  );
}

const METHOD_TONE: Record<string, string> = {
  GET: "text-success",
  POST: "text-brand",
  PUT: "text-warning",
  PATCH: "text-warning",
  DELETE: "text-danger",
};

function ApiReference() {
  const spec = useOpenApiSpec();
  const [query, setQuery] = useState("");

  const groups = useMemo(() => groupEndpoints(spec.data), [spec.data]);
  const filtered = useMemo(() => filterEndpoints(groups, query), [groups, query]);
  const total = countEndpoints(groups);
  const shown = countEndpoints(filtered);

  return (
    <Card className="overflow-hidden">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border px-4 py-3">
        <div>
          <h2 className="text-sm font-semibold text-fg">API reference</h2>
          <p className="text-xs text-muted">
            {spec.data?.info?.title ?? "HeroPanel"} {spec.data?.info?.version ? `· ${spec.data.info.version}` : ""} · rendered
            live from{" "}
            <a href="/api/v1/openapi.json" className="font-mono text-brand hover:underline" target="_blank" rel="noreferrer">
              /api/v1/openapi.json
            </a>
          </p>
        </div>
        {total > 0 && (
          <span className="text-xs text-muted">
            {query ? `${shown} of ${total}` : total} endpoints
          </span>
        )}
      </div>

      {spec.isLoading ? (
        <div className="p-4">
          <Spinner />
        </div>
      ) : spec.error ? (
        <div className="p-4">
          <Alert>Could not load the API document.</Alert>
        </div>
      ) : total === 0 ? (
        <EmptyState title="No API document" hint="The server did not return an OpenAPI spec." />
      ) : (
        <div className="space-y-2 p-3">
          <Input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Filter by path, summary, permission, method…"
            aria-label="Filter API endpoints"
          />
          {filtered.length === 0 ? (
            <p className="px-1 py-6 text-center text-sm text-muted">No endpoints match “{query}”.</p>
          ) : (
            filtered.map((g) => (
              <details key={g.tag} open={!!query} className="rounded-lg border border-border/60">
                <summary className="cursor-pointer select-none px-3 py-2 text-sm font-medium text-fg">
                  {g.tag}
                  <span className="ml-2 text-xs text-muted">{g.endpoints.length}</span>
                </summary>
                <ul className="divide-y divide-border/40 border-t border-border/40">
                  {g.endpoints.map((e) => (
                    <EndpointRow key={`${e.method} ${e.path}`} e={e} />
                  ))}
                </ul>
              </details>
            ))
          )}
        </div>
      )}
    </Card>
  );
}

function EndpointRow({ e }: { e: ApiEndpoint }) {
  return (
    <li className="flex flex-wrap items-baseline gap-x-3 gap-y-1 px-3 py-2 text-sm">
      <span className={`w-14 shrink-0 font-mono text-xs font-semibold ${METHOD_TONE[e.method] ?? "text-muted"}`}>
        {e.method}
      </span>
      <span className="font-mono text-xs text-fg">{e.path}</span>
      {e.summary && <span className="text-xs text-muted">— {e.summary}</span>}
      {e.deprecated && <span className="text-xs text-warning">deprecated</span>}
      {e.permission && (
        <span className="ml-auto rounded-full border border-border bg-surface px-2 py-0.5 font-mono text-[11px] text-muted">
          {e.permission}
        </span>
      )}
    </li>
  );
}
