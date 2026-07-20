import { lazy, Suspense, useState } from "react";
import { Link } from "react-router-dom";
import { ApiRequestError, can, type TerminalRecording } from "@/lib/api";
import { Badge, Button, Card, Modal, Spinner } from "@/components/ui";
import { toast } from "@/stores/toast";
import { useMe } from "@/features/auth/auth";
import { fetchCast, formatDuration, parseServerTime, useDeleteRecording, type Cast } from "./recordings";

// The player pulls in xterm; it loads only when a recording is actually opened.
const CastPlayer = lazy(() =>
  import("@/components/CastPlayer").then((m) => ({ default: m.CastPlayer })),
);

export function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  const units = ["KB", "MB", "GB"];
  let n = bytes / 1024;
  let i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i++;
  }
  return `${n.toFixed(n < 10 ? 1 : 0)} ${units[i]}`;
}

// RecordingsTable lists recorded sessions and replays them. It is shared by the
// site's Terminal tab and the cross-site Recordings page, so the two cannot
// drift into showing different things about the same session — including the
// redaction note, which is the part an operator most needs to read the same way
// everywhere.
//
// `showSite` is off inside a site, where the column would repeat the page.
export function RecordingsTable({
  recordings,
  showSite = false,
}: {
  recordings: TerminalRecording[];
  showSite?: boolean;
}) {
  const { data: me } = useMe();
  const canDelete = can(me, "terminal.recordings.delete");
  const remove = useDeleteRecording();

  const [playing, setPlaying] = useState<{ rec: TerminalRecording; cast: Cast } | null>(null);
  const [loadingUID, setLoadingUID] = useState<string | null>(null);
  const [confirmDelete, setConfirmDelete] = useState<TerminalRecording | null>(null);

  const open = async (rec: TerminalRecording) => {
    try {
      setLoadingUID(rec.uid);
      setPlaying({ rec, cast: await fetchCast(rec.uid) });
    } catch (e) {
      toast.error("Could not load the recording", e instanceof ApiRequestError ? e.message : undefined);
    } finally {
      setLoadingUID(null);
    }
  };

  return (
    <div className="space-y-3">
      <Card className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border text-left text-xs text-muted">
              <th className="px-4 py-2 font-medium">Started</th>
              {showSite && <th className="px-4 py-2 font-medium">Site</th>}
              <th className="px-4 py-2 font-medium">Opened by</th>
              <th className="px-4 py-2 font-medium">Ran as</th>
              <th className="px-4 py-2 font-medium">Length</th>
              <th className="px-4 py-2 font-medium">Size</th>
              <th className="px-4 py-2" />
            </tr>
          </thead>
          <tbody>
            {recordings.map((rec) => (
              <tr key={rec.uid} className="border-b border-border/60 last:border-0 hover:bg-surface/60">
                <td className="whitespace-nowrap px-4 py-2.5 text-fg">
                  {parseServerTime(rec.started_at).toLocaleString()}
                </td>
                {showSite && (
                  <td className="px-4 py-2.5">
                    {rec.site_uid ? (
                      <Link to={`/sites/${rec.site_uid}`} className="text-brand hover:underline">
                        {rec.site_name || rec.site_uid}
                      </Link>
                    ) : (
                      // The site is gone but the transcript is still inside its
                      // retention window — which is exactly when it matters.
                      <span className="text-muted" title="This site has since been deleted">
                        deleted site
                      </span>
                    )}
                  </td>
                )}
                <td className="px-4 py-2.5">
                  <span className="text-fg">{rec.actor_email || `user #${rec.actor_user_id}`}</span>
                  {rec.actor_ip && <span className="ml-2 text-xs text-muted">{rec.actor_ip}</span>}
                </td>
                <td className="px-4 py-2.5">
                  <code className="rounded bg-surface px-1.5 py-0.5 font-mono text-xs text-fg">{rec.system_user}</code>
                </td>
                <td className="whitespace-nowrap px-4 py-2.5 text-muted">
                  {formatDuration(rec.duration_ms)}
                  {rec.truncated && (
                    <span className="ml-2" title="The recording hit its size limit and is incomplete">
                      <Badge>truncated</Badge>
                    </span>
                  )}
                </td>
                <td className="whitespace-nowrap px-4 py-2.5 text-muted">{formatSize(rec.size_bytes)}</td>
                <td className="px-4 py-2.5">
                  <div className="flex items-center justify-end gap-2">
                    <Button
                      variant="ghost"
                      className="h-8 px-3"
                      loading={loadingUID === rec.uid}
                      onClick={() => void open(rec)}
                    >
                      Replay
                    </Button>
                    <a
                      href={`/api/v1/terminal/recordings/${rec.uid}/cast`}
                      className="rounded-lg border border-border px-3 py-1.5 text-xs text-muted hover:bg-border/50 hover:text-fg"
                      title="Download as asciicast (playable by asciinema)"
                    >
                      Download
                    </a>
                    {canDelete && (
                      <Button variant="danger" className="h-8 px-3" onClick={() => setConfirmDelete(rec)}>
                        Delete
                      </Button>
                    )}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </Card>

      <p className="text-xs text-muted">
        Recordings capture terminal output and keystrokes. Anything typed while the terminal had echo off — a{" "}
        <code className="font-mono">sudo</code> or database password prompt — is replaced with{" "}
        <code className="font-mono">[redacted]</code> before it is written, so passwords are never stored. Viewing a
        recording is itself recorded in the audit log.
      </p>

      {playing && (
        <Modal
          title={`Session — ${playing.rec.actor_email || "unknown"} as ${playing.rec.system_user}`}
          wide
          onClose={() => setPlaying(null)}
        >
          <div className="h-[70vh]">
            <Suspense
              fallback={
                <div className="flex h-full items-center justify-center text-muted">
                  <Spinner /> <span className="ml-2">Loading player…</span>
                </div>
              }
            >
              <CastPlayer cast={playing.cast} />
            </Suspense>
          </div>
        </Modal>
      )}

      {confirmDelete && (
        <Modal title="Delete this recording?" onClose={() => setConfirmDelete(null)}>
          <p className="text-sm text-muted">
            This permanently removes the transcript of a privileged session. Deleting an audit artifact is itself
            recorded in the audit log. This cannot be undone.
          </p>
          <div className="mt-4 flex justify-end gap-2">
            <Button variant="ghost" onClick={() => setConfirmDelete(null)}>
              Cancel
            </Button>
            <Button
              variant="danger"
              loading={remove.isPending}
              onClick={() =>
                remove.mutate(confirmDelete.uid, {
                  onSuccess: () => {
                    toast.success("Recording deleted");
                    setConfirmDelete(null);
                  },
                  onError: (e) =>
                    toast.error("Could not delete the recording", e instanceof ApiRequestError ? e.message : undefined),
                })
              }
            >
              Delete
            </Button>
          </div>
        </Modal>
      )}
    </div>
  );
}
