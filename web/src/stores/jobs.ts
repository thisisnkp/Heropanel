import { create } from "zustand";
import { api, type Job } from "@/lib/api";
import { wsSubscribe } from "@/lib/ws";
import { toast } from "./toast";

// The global job tracker. Any async action (site create/delete/clone, deploy,
// rollback, DB import/export) returns a job; instead of each page growing its
// own progress bar, it calls trackJob() and the job appears in the global drawer
// with live progress. This is the "job/progress drawer" of the Phase-0 shell,
// promoted from the per-page JobProgress that only SitesPage had.

export interface TrackedJob {
  id: string;
  label: string;
  progress: number;
  step: string;
  status: string; // queued | running | succeeded | failed
  startedAt: number;
}

interface JobState {
  jobs: TrackedJob[];
  open: boolean;
  track: (id: string, label: string) => void;
  setOpen: (v: boolean) => void;
  clearFinished: () => void;
}

// unsub holds the per-job WS unsubscribe so a finished job stops listening.
const unsub = new Map<string, () => void>();

export const useJobs = create<JobState>((set, get) => ({
  jobs: [],
  open: false,
  track: (id, label) => {
    if (get().jobs.some((j) => j.id === id)) return; // already tracked
    set((s) => ({
      open: true,
      jobs: [{ id, label, progress: 0, step: "queued", status: "queued", startedAt: Date.now() }, ...s.jobs],
    }));

    const update = (patch: Partial<TrackedJob>) =>
      set((s) => ({ jobs: s.jobs.map((j) => (j.id === id ? { ...j, ...patch } : j)) }));

    const finish = (status: string, label: string) => {
      const u = unsub.get(id);
      if (u) {
        u();
        unsub.delete(id);
      }
      if (status === "succeeded") toast.success(`${label} completed`);
      else if (status === "failed") toast.error(`${label} failed`, "See the resource for details.");
    };

    // Seed from REST — also the fallback if a WS event was missed before we
    // subscribed — then follow live over the job's channel.
    api
      .get<Job>(`/jobs/${id}`)
      .then((j) => {
        update({ progress: j.progress, step: j.status, status: j.status });
        if (j.status === "succeeded" || j.status === "failed") finish(j.status, label);
      })
      .catch(() => {});

    const off = wsSubscribe(`job:${id}`, (data) => {
      const d = data as { progress?: number; step?: string; status?: string };
      const status = d.status ?? "running";
      update({ progress: d.progress ?? 0, step: d.step ?? "", status });
      if (status === "succeeded" || status === "failed") finish(status, label);
    });
    unsub.set(id, off);
  },
  setOpen: (v) => set({ open: v }),
  clearFinished: () =>
    set((s) => ({ jobs: s.jobs.filter((j) => j.status !== "succeeded" && j.status !== "failed") })),
}));

// activeCount is how many jobs are still running — the badge on the topbar.
export function activeJobCount(jobs: TrackedJob[]): number {
  return jobs.filter((j) => j.status === "queued" || j.status === "running").length;
}
