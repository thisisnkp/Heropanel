import { useJobs, type TrackedJob } from "@/stores/jobs";
import { Button, StatusBadge, cn } from "./ui";

function Row({ job }: { job: TrackedJob }) {
  const failed = job.status === "failed";
  const done = job.status === "succeeded";
  return (
    <div className="border-b border-border/60 px-4 py-3 last:border-0">
      <div className="mb-2 flex items-center justify-between gap-2">
        <span className="truncate text-sm font-medium text-fg">{job.label}</span>
        <StatusBadge status={job.status} />
      </div>
      <div className="h-1.5 overflow-hidden rounded-full bg-border">
        <div
          className={cn(
            "h-full rounded-full transition-all duration-300",
            failed ? "bg-danger" : done ? "bg-emerald-500" : "bg-brand",
          )}
          style={{ width: `${done ? 100 : Math.max(4, job.progress)}%` }}
        />
      </div>
      <p className="mt-1 text-xs text-muted">{failed ? "failed" : job.step || job.status}</p>
    </div>
  );
}

// JobDrawer is the slide-in panel listing tracked jobs with live progress. It is
// mounted once by the shell and driven entirely by the jobs store.
export function JobDrawer() {
  const { jobs, open, setOpen, clearFinished } = useJobs();
  if (!open) return null;

  const finished = jobs.filter((j) => j.status === "succeeded" || j.status === "failed").length;

  return (
    <>
      <div className="fixed inset-0 z-40 bg-black/20" onClick={() => setOpen(false)} />
      <aside className="fixed right-0 top-0 z-50 flex h-full w-full max-w-sm flex-col border-l border-border bg-panel shadow-xl">
        <div className="flex h-14 shrink-0 items-center justify-between border-b border-border px-4">
          <span className="text-sm font-semibold text-fg">Activity</span>
          <button onClick={() => setOpen(false)} className="text-muted hover:text-fg" aria-label="Close">
            <svg viewBox="0 0 24 24" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round">
              <path d="M18 6L6 18M6 6l12 12" />
            </svg>
          </button>
        </div>
        <div className="flex-1 overflow-auto">
          {jobs.length === 0 ? (
            <p className="px-4 py-10 text-center text-sm text-muted">No background jobs.</p>
          ) : (
            jobs.map((j) => <Row key={j.id} job={j} />)
          )}
        </div>
        {finished > 0 && (
          <div className="shrink-0 border-t border-border p-3">
            <Button variant="ghost" className="w-full" onClick={clearFinished}>
              Clear {finished} finished
            </Button>
          </div>
        )}
      </aside>
    </>
  );
}
