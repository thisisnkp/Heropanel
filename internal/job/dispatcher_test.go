package job_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/thisisnkp/heropanel/internal/config"
	"github.com/thisisnkp/heropanel/internal/job"
	"github.com/thisisnkp/heropanel/internal/repository"
)

func newDispatcher(t *testing.T) *job.Dispatcher {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	dsn := t.TempDir() + "/jobs.db"
	db, err := repository.Open(config.Database{Driver: "sqlite", DSN: dsn})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := repository.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = rdb.Close(); mr.Close(); _ = db.Close() })

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return job.NewDispatcher(repository.NewJobStore(db), rdb, log)
}

// waitStatus polls until a job reaches one of the wanted statuses.
func waitStatus(t *testing.T, d *job.Dispatcher, uid string, want ...string) *job.Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last *job.Job
	for time.Now().Before(deadline) {
		j, err := d.Get(context.Background(), uid)
		if err == nil {
			last = j
			for _, w := range want {
				if j.Status == w {
					return j
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	status := "?"
	if last != nil {
		status = last.Status
	}
	t.Fatalf("job %s did not reach %v (last status %q)", uid, want, status)
	return nil
}

func TestEnqueueAndProcessSucceeds(t *testing.T) {
	d := newDispatcher(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d.Register("test.echo", func(_ context.Context, j *job.Job, p job.Progress) (any, error) {
		p.Report(50, "halfway")
		var m map[string]string
		_ = json.Unmarshal(j.Payload, &m)
		return map[string]any{"echo": m["msg"]}, nil
	})
	if err := d.StartWorkers(ctx, 1); err != nil {
		t.Fatalf("start workers: %v", err)
	}

	j, err := d.Enqueue(ctx, "test.echo", 7, map[string]string{"msg": "hi"})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if j.Status != string(job.StatusQueued) {
		t.Fatalf("enqueued status = %q, want queued", j.Status)
	}

	final := waitStatus(t, d, j.UID, string(job.StatusSucceeded), string(job.StatusFailed))
	if final.Status != string(job.StatusSucceeded) {
		t.Fatalf("status = %q, want succeeded (err=%v)", final.Status, final.Error.String)
	}
	if final.Progress != 100 {
		t.Fatalf("progress = %d, want 100", final.Progress)
	}
	var res struct {
		Echo string `json:"echo"`
	}
	if err := json.Unmarshal(final.Result, &res); err != nil || res.Echo != "hi" {
		t.Fatalf("result = %s (echo=%q err=%v)", final.Result, res.Echo, err)
	}
}

func TestFailingJobRecordsError(t *testing.T) {
	d := newDispatcher(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d.Register("test.fail", func(_ context.Context, _ *job.Job, _ job.Progress) (any, error) {
		return nil, errors.New("kaboom")
	})
	_ = d.StartWorkers(ctx, 1)

	j, _ := d.Enqueue(ctx, "test.fail", 0, map[string]string{})
	final := waitStatus(t, d, j.UID, string(job.StatusFailed), string(job.StatusSucceeded))
	if final.Status != string(job.StatusFailed) {
		t.Fatalf("status = %q, want failed", final.Status)
	}
	if !final.Error.Valid || !strings.Contains(final.Error.String, "kaboom") {
		t.Fatalf("error = %q, want to contain kaboom", final.Error.String)
	}
}

func TestUnknownJobTypeFails(t *testing.T) {
	d := newDispatcher(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = d.StartWorkers(ctx, 1)

	j, _ := d.Enqueue(ctx, "test.nohandler", 0, map[string]string{})
	final := waitStatus(t, d, j.UID, string(job.StatusFailed), string(job.StatusSucceeded))
	if final.Status != string(job.StatusFailed) || !strings.Contains(final.Error.String, "no handler") {
		t.Fatalf("expected failure with 'no handler', got status=%q err=%q", final.Status, final.Error.String)
	}
}

func TestListReturnsNewestFirst(t *testing.T) {
	d := newDispatcher(t)
	ctx := context.Background()
	a, _ := d.Enqueue(ctx, "test.x", 9, map[string]string{"n": "1"})
	b, _ := d.Enqueue(ctx, "test.x", 9, map[string]string{"n": "2"})

	jobs, err := d.List(ctx, 9, 50, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(jobs) != 2 || jobs[0].UID != b.UID || jobs[1].UID != a.UID {
		t.Fatalf("expected newest-first [%s,%s], got %+v", b.UID, a.UID, jobs)
	}
}
