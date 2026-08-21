import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createPinia, setActivePinia } from "pinia";

import { useJobsStore } from "./jobs";

describe("jobs store", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.useFakeTimers();
  });
  afterEach(() => vi.useRealTimers());

  it("opens the tray when work starts, so the first job is never silent", () => {
    const jobs = useJobsStore();
    jobs.open = false;

    jobs.start("Deploy main@4f2a1c9", "novaretail.in", "Fetching repository");

    expect(jobs.open).toBe(true);
    expect(jobs.jobs).toHaveLength(1);
    expect(jobs.jobs[0].state).toBe("running");
    expect(jobs.jobs[0].step).toBe("Fetching repository");
  });

  it("counts only what is still running", () => {
    const jobs = useJobsStore();
    jobs.start("a", "t", "s");
    jobs.start("b", "t", "s");
    expect(jobs.running).toBe(2);
    expect(jobs.label).toBe("2 jobs running");

    // Far enough for the first to finish; it stays listed as done for a moment,
    // but "2 jobs running" while one has finished would be a lie.
    vi.advanceTimersByTime(900 * 6);
    expect(jobs.jobs.filter((j) => j.state === "done").length).toBeGreaterThan(0);
    expect(jobs.running).toBe(jobs.jobs.filter((j) => j.state === "running").length);
  });

  it("finishes, then clears itself", () => {
    const jobs = useJobsStore();
    jobs.start("Add index", "nexp_novaretail", "Snapshotting database");

    vi.advanceTimersByTime(900 * 6);
    expect(jobs.jobs[0].state).toBe("done");
    expect(jobs.jobs[0].pct).toBe(100);

    // Long enough to read, then gone: a tray that keeps every completed job
    // becomes a list nobody looks at.
    vi.advanceTimersByTime(4000);
    expect(jobs.jobs).toHaveLength(0);
  });

  it("stops ticking a cancelled job", () => {
    const jobs = useJobsStore();
    const id = jobs.start("Restore", "novaretail.in", "Downloading");
    jobs.cancel(id);
    expect(jobs.jobs).toHaveLength(0);

    // The interval must be cleared with it — otherwise a cancelled job keeps
    // firing forever and the next one that reuses the id starts corrupted.
    vi.advanceTimersByTime(900 * 10);
    expect(jobs.jobs).toHaveLength(0);
  });

  it("keeps two jobs apart even when started in the same millisecond", () => {
    const jobs = useJobsStore();
    const a = jobs.start("a", "t", "s");
    const b = jobs.start("b", "t", "s");
    expect(a).not.toBe(b);
  });
});
