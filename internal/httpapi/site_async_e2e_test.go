package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/thisisnkp/heropanel/internal/auth"
	"github.com/thisisnkp/heropanel/internal/config"
	"github.com/thisisnkp/heropanel/internal/httpapi"
	"github.com/thisisnkp/heropanel/internal/job"
	"github.com/thisisnkp/heropanel/internal/php"
	"github.com/thisisnkp/heropanel/internal/repository"
	"github.com/thisisnkp/heropanel/internal/site"
	"github.com/thisisnkp/heropanel/internal/webserver"
	pcache "github.com/thisisnkp/heropanel/pkg/cache"
)

func newAsyncSiteRouter(t *testing.T) http.Handler {
	t.Helper()
	db, err := repository.Open(config.Database{Driver: "sqlite", DSN: t.TempDir() + "/async.db"})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := repository.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	users := repository.NewUserRepository(db)
	sessions := repository.NewSessionRepository(db)
	rbac := repository.NewRBACRepository(db)
	if err := auth.SeedRBAC(context.Background(), rbac); err != nil {
		t.Fatalf("seed: %v", err)
	}
	l1 := pcache.NewLocal(pcache.LocalConfig{})
	authSvc := auth.NewService(users, sessions, rbac, pcache.NewTiered(l1, nil, pcache.TieredConfig{}), auth.DefaultConfig())
	sites := site.NewService(site.Deps{
		Repo:   repository.NewSiteStore(db),
		Broker: fakeGateway{},
		Web:    webserver.NewService(fakeGateway{}),
		PHP:    php.NewService(repository.NewPHPPoolStore(db), fakeGateway{}),
	})

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	disp := job.NewDispatcher(repository.NewJobStore(db), rdb, slog.New(slog.NewTextHandler(io.Discard, nil)))
	disp.Register("site.create", func(ctx context.Context, j *job.Job, p job.Progress) (any, error) {
		var in site.CreateInput
		if err := json.Unmarshal(j.Payload, &in); err != nil {
			return nil, err
		}
		s, err := sites.RunCreate(ctx, in, p)
		if err != nil {
			return nil, err
		}
		return map[string]any{"site_uid": s.UID}, nil
	})
	disp.Register("site.delete", func(ctx context.Context, j *job.Job, p job.Progress) (any, error) {
		var pl struct {
			UID string `json:"uid"`
		}
		if err := json.Unmarshal(j.Payload, &pl); err != nil {
			return nil, err
		}
		return nil, sites.RunDelete(ctx, pl.UID, p)
	})

	ctx, cancel := context.WithCancel(context.Background())
	if err := disp.StartWorkers(ctx, 1); err != nil {
		t.Fatalf("workers: %v", err)
	}
	t.Cleanup(func() { cancel(); _ = rdb.Close(); mr.Close(); _ = l1.Close(); _ = db.Close() })

	cfg := config.Default()
	cfg.Security.RateLimit.Enabled = false
	return httpapi.NewRouter(httpapi.Deps{
		Ctx:       ctx,
		Config:    cfg,
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Version:   "test",
		StartedAt: time.Now(),
		Auth:      authSvc,
		Users:     testUserDir{repo: users},
		Sites:     sites,
		Jobs:      disp,
	})
}

func waitJobStatus(t *testing.T, h http.Handler, cookie *http.Cookie, id string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rec := getWith(t, h, "/api/v1/jobs/"+id, cookie)
		var env struct {
			Data struct {
				Status string `json:"status"`
			} `json:"data"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &env)
		if env.Data.Status == "succeeded" || env.Data.Status == "failed" {
			return env.Data.Status
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("job did not finish in time")
	return ""
}

func TestSitesAsyncCreateAndDelete(t *testing.T) {
	h := newAsyncSiteRouter(t)

	_ = postJSON(t, h, "/api/v1/auth/bootstrap",
		map[string]string{"email": "admin@example.com", "username": "admin", "password": "supersecret1"}, nil)
	cookie := sessionCookie(t, postJSON(t, h, "/api/v1/auth/login",
		map[string]string{"email": "admin@example.com", "password": "supersecret1"}, nil))

	// Create returns 202 + a job.
	rec := postJSON(t, h, "/api/v1/sites",
		map[string]string{"name": "Acme", "primary_domain": "acme.example.com", "type": "static"}, cookie)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("create status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		Data struct {
			Job struct {
				ID        string `json:"id"`
				Status    string `json:"status"`
				WSChannel string `json:"ws_channel"`
			} `json:"job"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.Data.Job.ID == "" || created.Data.Job.Status != "queued" || created.Data.Job.WSChannel == "" {
		t.Fatalf("unexpected job envelope: %+v", created.Data.Job)
	}

	if status := waitJobStatus(t, h, cookie, created.Data.Job.ID); status != "succeeded" {
		t.Fatalf("create job status = %q, want succeeded", status)
	}

	// The site now exists and is active.
	listRec := getWith(t, h, "/api/v1/sites", cookie)
	var list struct {
		Data []site.Site `json:"data"`
	}
	_ = json.Unmarshal(listRec.Body.Bytes(), &list)
	if len(list.Data) != 1 || list.Data[0].Status != site.StatusActive {
		t.Fatalf("expected 1 active site, got %+v", list.Data)
	}
	uid := list.Data[0].UID

	// Delete returns 202 + a job that succeeds; the site is then gone.
	delRec := deleteWith(t, h, "/api/v1/sites/"+uid, cookie)
	if delRec.Code != http.StatusAccepted {
		t.Fatalf("delete status = %d, want 202", delRec.Code)
	}
	var deleted struct {
		Data struct {
			Job struct {
				ID string `json:"id"`
			} `json:"job"`
		} `json:"data"`
	}
	_ = json.Unmarshal(delRec.Body.Bytes(), &deleted)
	if status := waitJobStatus(t, h, cookie, deleted.Data.Job.ID); status != "succeeded" {
		t.Fatalf("delete job status = %q, want succeeded", status)
	}
	if rec := getWith(t, h, "/api/v1/sites/"+uid, cookie); rec.Code != http.StatusNotFound {
		t.Fatalf("site should be gone after delete job, got %d", rec.Code)
	}
}
