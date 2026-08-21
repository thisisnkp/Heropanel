import { computed, ref } from "vue";
import { defineStore } from "pinia";

export type JobState = "running" | "done" | "failed";

export interface Job {
  readonly id: string;
  readonly name: string;
  /** What the job is acting on — a domain, a database. */
  readonly target: string;
  readonly step: string;
  readonly pct: number;
  readonly state: JobState;
}

/**
 * Long-running work, tracked where you can still see it after you navigate.
 *
 * The panel does things that outlive the screen that started them — a deploy, an
 * index build, a restore. Reporting those with a toast means the only record of
 * a running job disappears after four seconds, and the only way to find out
 * whether it finished is to go back and look. The tray persists until the job
 * ends, and survives route changes because it lives in the shell.
 *
 * The ticking here is the fixture's; a real panel will replace `start()` with a
 * subscription to npd's job stream and keep the same shape.
 */
export const useJobsStore = defineStore("jobs", () => {
  const jobs = ref<Job[]>([]);
  const open = ref(true);

  const running = computed(() => jobs.value.filter((j) => j.state === "running").length);
  const label = computed(() =>
    running.value === 1 ? "1 job running" : running.value + " jobs running",
  );

  // Bare timer globals, not `window.*`: the store is plain state with no DOM in
  // it, and reaching through `window` is what forced the unit tests to boot a
  // browser environment to test a progress counter.
  const timers = new Map<string, ReturnType<typeof setInterval>>();

  function patch(id: string, next: Partial<Job>) {
    jobs.value = jobs.value.map((j) => (j.id === id ? { ...j, ...next } : j));
  }

  function remove(id: string) {
    const t = timers.get(id);
    if (t !== undefined) clearInterval(t);
    timers.delete(id);
    jobs.value = jobs.value.filter((j) => j.id !== id);
  }

  const STEPS = ["Snapshotting database", "Applying change", "Verifying", "Reloading service"];

  function start(name: string, target: string, step: string) {
    const id = "j" + Date.now() + "-" + jobs.value.length;
    jobs.value = [...jobs.value, { id, name, target, step, pct: 8, state: "running" }];
    open.value = true;

    let pct = 8;
    const tick = setInterval(() => {
      pct += 18;
      if (pct >= 100) {
        patch(id, { pct: 100, state: "done", step: "Finished" });
        clearInterval(tick);
        timers.delete(id);
        // Left on screen long enough to be read, then cleared: a tray that keeps
        // every finished job becomes a list nobody reads.
        setTimeout(() => remove(id), 4000);
      } else {
        patch(id, { pct, step: STEPS[Math.min(STEPS.length - 1, Math.floor(pct / 26))] });
      }
    }, 900);
    timers.set(id, tick);
    return id;
  }

  function cancel(id: string) {
    remove(id);
  }

  function toggle() {
    open.value = !open.value;
  }

  return { jobs, open, running, label, start, cancel, toggle };
});
