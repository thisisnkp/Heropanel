import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiRequestError, can } from "@/lib/api";
import { Alert, Badge, Button, Card, Field, Input, Modal, Spinner } from "@/components/ui";
import { toast } from "@/stores/toast";
import { useMe } from "@/features/auth/auth";

// The site's scheduled jobs. Each is a real systemd timer triggering a oneshot
// service that runs as the site's user, inside its cgroup slice — never root.
// Overlap comes free: a job still running when its timer fires again is not
// started a second time.

interface CronJob {
  uid: string;
  name: string;
  command: string;
  schedule: string;
  enabled: boolean;
  created_at: string;
}

function useCronJobs(uid: string) {
  return useQuery({
    queryKey: ["cron", uid],
    queryFn: () => api.get<CronJob[]>(`/sites/${uid}/cron`),
  });
}

export function CronTab({ uid }: { uid: string }) {
  const { data: me } = useMe();
  const canWrite = can(me, "site.write");
  const qc = useQueryClient();
  const { data, isLoading, error } = useCronJobs(uid);
  const [showForm, setShowForm] = useState(false);
  const [logsFor, setLogsFor] = useState<CronJob | null>(null);

  const invalidate = () => qc.invalidateQueries({ queryKey: ["cron", uid] });

  const toggle = useMutation({
    mutationFn: ({ jid, enabled }: { jid: string; enabled: boolean }) =>
      api.put(`/sites/${uid}/cron/${jid}`, { enabled }),
    onSuccess: invalidate,
  });
  const run = useMutation({
    mutationFn: (jid: string) => api.post(`/sites/${uid}/cron/${jid}/run`, {}),
  });
  const del = useMutation({
    mutationFn: (jid: string) => api.del(`/sites/${uid}/cron/${jid}`),
    onSuccess: invalidate,
  });

  if (isLoading) return <Spinner />;
  if (error) return <Alert>Could not load scheduled jobs.</Alert>;
  const jobs = data ?? [];

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <p className="text-sm text-muted">
          Jobs run as this site&apos;s user, in its home, inside its resource limits — never as root. A job still
          running when its schedule fires again is skipped, not stacked.
        </p>
        {canWrite && (
          <Button onClick={() => setShowForm((v) => !v)}>{showForm ? "Cancel" : "New job"}</Button>
        )}
      </div>

      {showForm && <CronForm uid={uid} onDone={() => { setShowForm(false); invalidate(); }} />}

      {jobs.length === 0 ? (
        <Card className="p-6 text-center text-sm text-muted">
          No scheduled jobs. Typical uses: a framework scheduler (Laravel <code className="font-mono text-xs">schedule:run</code>),
          WP-Cron, cache cleanup.
        </Card>
      ) : (
        <Card className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border text-left text-xs text-muted">
                <th className="px-4 py-2 font-medium">Job</th>
                <th className="px-4 py-2 font-medium">Schedule</th>
                <th className="px-4 py-2 font-medium">Command</th>
                <th className="px-4 py-2" />
              </tr>
            </thead>
            <tbody>
              {jobs.map((j) => (
                <tr key={j.uid} className="border-b border-border/60 last:border-0">
                  <td className="px-4 py-2.5">
                    <div className="flex items-center gap-2">
                      <span className={`h-2 w-2 rounded-full ${j.enabled ? "bg-emerald-500" : "bg-border"}`} aria-hidden />
                      <span className="text-fg">{j.name}</span>
                    </div>
                  </td>
                  <td className="px-4 py-2.5">
                    <Badge>{j.schedule}</Badge>
                  </td>
                  <td className="max-w-[18rem] truncate px-4 py-2.5 font-mono text-xs text-muted">{j.command}</td>
                  <td className="px-4 py-2.5 text-right">
                    <div className="flex justify-end gap-2">
                      <Button variant="ghost" className="h-8 px-3" onClick={() => setLogsFor(j)}>
                        Logs
                      </Button>
                      {canWrite && (
                        <>
                          <Button
                            variant="ghost"
                            className="h-8 px-3"
                            loading={run.isPending}
                            onClick={() =>
                              run.mutate(j.uid, {
                                onSuccess: () => toast.success("Job ran", j.name),
                                onError: (e) =>
                                  toast.error("The job failed", e instanceof ApiRequestError ? e.message : undefined),
                              })
                            }
                          >
                            Run now
                          </Button>
                          <Button
                            variant="ghost"
                            className="h-8 px-3"
                            onClick={() => toggle.mutate({ jid: j.uid, enabled: !j.enabled })}
                          >
                            {j.enabled ? "Disable" : "Enable"}
                          </Button>
                          <Button
                            variant="danger"
                            className="h-8 px-3"
                            onClick={() =>
                              del.mutate(j.uid, { onSuccess: () => toast.success("Job deleted", j.name) })
                            }
                          >
                            Delete
                          </Button>
                        </>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
      )}

      {logsFor && <CronLogsModal uid={uid} job={logsFor} onClose={() => setLogsFor(null)} />}
    </div>
  );
}

function CronForm({ uid, onDone }: { uid: string; onDone: () => void }) {
  const [name, setName] = useState("");
  const [command, setCommand] = useState("");
  const [schedule, setSchedule] = useState("daily");
  const create = useMutation({
    mutationFn: () => api.post<CronJob>(`/sites/${uid}/cron`, { name, command, schedule }),
  });

  return (
    <form
      className="space-y-3 rounded-lg border border-border bg-surface p-4"
      onSubmit={(e) => {
        e.preventDefault();
        create.mutate(undefined, {
          onSuccess: () => {
            toast.success("Job scheduled", name);
            onDone();
          },
          onError: (err) =>
            toast.error("Could not schedule the job", err instanceof ApiRequestError ? err.message : undefined),
        });
      }}
    >
      <Field label="Name">
        <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Nightly cleanup" required />
      </Field>
      <Field label="Command" hint="One line. Runs as this site's user in the site home.">
        <Input
          value={command}
          onChange={(e) => setCommand(e.target.value)}
          placeholder="php artisan schedule:run"
          required
        />
      </Field>
      <Field
        label="Schedule"
        hint='systemd calendar form: "daily", "hourly", "*-*-* 02:00:00", "Mon *-*-* 09:00:00".'
      >
        <Input value={schedule} onChange={(e) => setSchedule(e.target.value)} required />
      </Field>
      <div className="flex justify-end">
        <Button type="submit" loading={create.isPending}>
          Schedule
        </Button>
      </div>
    </form>
  );
}

function CronLogsModal({ uid, job, onClose }: { uid: string; job: CronJob; onClose: () => void }) {
  const { data, isLoading, error } = useQuery({
    queryKey: ["cron", uid, job.uid, "logs"],
    queryFn: () => api.get<{ log: string }>(`/sites/${uid}/cron/${job.uid}/logs`),
    refetchInterval: 5_000,
  });

  return (
    <Modal title={`Logs — ${job.name}`} wide onClose={onClose}>
      <div className="h-[60vh] overflow-auto rounded-lg bg-surface p-3">
        {isLoading ? (
          <Spinner />
        ) : error ? (
          <Alert>Could not read the job&apos;s logs.</Alert>
        ) : data?.log ? (
          <pre className="whitespace-pre-wrap break-all font-mono text-xs text-fg">{data.log}</pre>
        ) : (
          <p className="text-sm text-muted">This job has not written anything yet.</p>
        )}
      </div>
    </Modal>
  );
}
