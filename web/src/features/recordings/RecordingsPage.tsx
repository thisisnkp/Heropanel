import { useMemo, useState } from "react";
import { ApiRequestError } from "@/lib/api";
import { Alert, Button, EmptyState, Input, Spinner } from "@/components/ui";
import { PageSize, useAllRecordings } from "./recordings";
import { RecordingsTable } from "./RecordingsTable";

// RecordingsPage is the cross-site view of recorded terminal sessions.
//
// It exists because the per-site panel lives inside the Terminal tab, which is
// gated on `terminal.use` — so the one role that most needs recordings, an
// auditor with `terminal.recordings.read` and deliberately *no* shell access,
// could not reach a single one. Recordings are their own thing at the top level,
// alongside the audit log, not a sub-view of the power they audit.
export function RecordingsPage() {
  const [offset, setOffset] = useState(0);
  const [q, setQ] = useState("");
  const { data, isLoading, error, isFetching } = useAllRecordings(offset);

  const recordings = useMemo(() => data ?? [], [data]);
  const filtered = useMemo(() => {
    const needle = q.trim().toLowerCase();
    if (!needle) return recordings;
    return recordings.filter((r) =>
      [r.actor_email, r.system_user, r.site_name, r.site_uid, r.actor_ip]
        .some((f) => (f ?? "").toLowerCase().includes(needle)),
    );
  }, [recordings, q]);

  if (error) {
    return (
      <div className="space-y-6">
        <Header />
        <Alert>
          {error instanceof ApiRequestError && error.status === 403
            ? "You do not have permission to view recorded sessions."
            : "Could not load recorded sessions."}
        </Alert>
      </div>
    );
  }

  const atPageLimit = recordings.length === PageSize;

  return (
    <div className="space-y-6">
      <Header />

      <div className="flex flex-wrap items-center gap-3">
        <Input
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="Filter by person, site, Linux user, or IP"
          className="max-w-sm"
        />
        {q && (
          <span className="text-xs text-muted">
            {filtered.length} of {recordings.length} shown
          </span>
        )}
        {isFetching && <Spinner />}
      </div>

      {isLoading ? (
        <Spinner />
      ) : recordings.length === 0 ? (
        <EmptyState
          title={offset > 0 ? "No older sessions" : "No recorded sessions"}
          hint="Sessions are recorded automatically once a terminal is opened, and kept for the configured retention period."
        />
      ) : filtered.length === 0 ? (
        <EmptyState title="Nothing matches that filter" hint="The filter searches the sessions loaded below." />
      ) : (
        <RecordingsTable recordings={filtered} showSite />
      )}

      {(offset > 0 || atPageLimit) && (
        <div className="flex items-center justify-between">
          {/* The filter only searches what is loaded, so say so rather than
              letting an auditor read "no results" as "no such session". */}
          <span className="text-xs text-muted">
            Showing sessions {offset + 1}–{offset + recordings.length}. The filter searches this page only.
          </span>
          <div className="flex gap-2">
            <Button
              variant="ghost"
              disabled={offset === 0}
              onClick={() => setOffset((o) => Math.max(0, o - PageSize))}
            >
              Newer
            </Button>
            <Button variant="ghost" disabled={!atPageLimit} onClick={() => setOffset((o) => o + PageSize)}>
              Older
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}

function Header() {
  return (
    <div>
      <h1 className="text-2xl font-semibold text-fg">Session recordings</h1>
      <p className="text-sm text-muted">
        Transcripts of every terminal session, across all sites — who opened it, as which Linux account, and what was
        done. Kept for the configured retention period, then removed.
      </p>
    </div>
  );
}
