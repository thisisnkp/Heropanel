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
  // The search runs on the server across ALL history, so a query is not limited
  // to the loaded page. Reset paging whenever the query changes.
  const { data, isLoading, error, isFetching } = useAllRecordings(offset, q);
  const searching = q.trim() !== "";

  const recordings = useMemo(() => data ?? [], [data]);

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
          onChange={(e) => {
            setQ(e.target.value);
            setOffset(0);
          }}
          placeholder="Search all history by person, site, Linux user, or IP"
          className="max-w-sm"
        />
        {isFetching && <Spinner />}
      </div>

      {isLoading ? (
        <Spinner />
      ) : recordings.length === 0 ? (
        <EmptyState
          title={searching ? "No matching sessions" : offset > 0 ? "No older sessions" : "No recorded sessions"}
          hint={
            searching
              ? "The search covers all recorded history — no session matches that query."
              : "Sessions are recorded automatically once a terminal is opened, and kept for the configured retention period."
          }
        />
      ) : (
        <RecordingsTable recordings={recordings} showSite />
      )}

      {(offset > 0 || atPageLimit) && (
        <div className="flex items-center justify-between">
          <span className="text-xs text-muted">
            {searching ? "Search results" : `Showing sessions ${offset + 1}–${offset + recordings.length}`}
            {searching ? " across all history." : ""}
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
