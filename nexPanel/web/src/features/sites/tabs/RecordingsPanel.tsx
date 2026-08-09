import { ApiRequestError } from "@/lib/api";
import { Alert, EmptyState, Spinner } from "@/components/ui";
import { useRecordings } from "@/features/recordings/recordings";
import { RecordingsTable } from "@/features/recordings/RecordingsTable";

// RecordingsPanel lists this site's recorded sessions, in context, next to the
// terminal that produced them.
//
// It is only a convenience view: because it sits inside the Terminal tab it is
// reachable only by someone who also holds `terminal.use`. The cross-site
// /recordings page is the one an auditor uses, and that is gated on
// `terminal.recordings.read` alone.
export function RecordingsPanel({ uid }: { uid: string }) {
  const { data, isLoading, error } = useRecordings(uid);

  if (isLoading) {
    return (
      <div className="flex items-center gap-2 text-muted">
        <Spinner /> Loading recordings…
      </div>
    );
  }
  if (error) {
    return (
      <Alert>
        {error instanceof ApiRequestError && error.status === 403
          ? "You do not have permission to view recorded sessions."
          : "Could not load recorded sessions."}
      </Alert>
    );
  }

  const recordings = data ?? [];
  if (recordings.length === 0) {
    return (
      <EmptyState
        title="No recorded sessions"
        hint="Sessions are recorded automatically once a terminal is opened, and kept for the configured retention period."
      />
    );
  }
  return <RecordingsTable recordings={recordings} />;
}
