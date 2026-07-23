// Package cron is the in-core scheduler: site-scoped jobs rendered into systemd
// timers by the broker. Nothing here is privileged; every effect (writing a
// timer, enabling it, running it, reading its log) is a named broker capability,
// exactly as the app-runtime and web-server modules are.
package cron

import (
	"context"
	"regexp"
	"strings"

	"github.com/thisisnkp/heropanel/internal/broker"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Job is the API view of a scheduled job.
type Job struct {
	UID       string `json:"uid"`
	Name      string `json:"name"`
	Command   string `json:"command"`
	Schedule  string `json:"schedule"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"created_at"`
}

// Record is the persistence view.
type Record struct {
	UID       string `db:"uid"`
	SiteID    int64  `db:"site_id"`
	Name      string `db:"name"`
	Command   string `db:"command"`
	Schedule  string `db:"schedule"`
	Enabled   bool   `db:"enabled"`
	CreatedAt string `db:"created_at"`
}

// CreateInput is a request to schedule a job.
type CreateInput struct {
	Name     string `json:"name"`
	Command  string `json:"command"`
	Schedule string `json:"schedule"`
}

// Repo is the persistence contract.
type Repo interface {
	Insert(ctx context.Context, r *Record) error
	ListBySiteID(ctx context.Context, siteID int64) ([]Record, error)
	GetByUID(ctx context.Context, uid string) (*Record, error)
	SetEnabled(ctx context.Context, uid string, enabled bool) error
	Delete(ctx context.Context, uid string) error
}

// SiteRef is what the scheduler needs about the site a job belongs to.
type SiteRef struct {
	ID        int64
	UID       string
	LinuxUser string // the vhost, and the user the job runs as
	HomeDir   string
}

// Sites resolves a site by UID. Implemented by an adapter over internal/site.
type Sites interface {
	Resolve(ctx context.Context, siteUID string) (*SiteRef, error)
}

// Service manages scheduled jobs.
type Service struct {
	repo   Repo
	sites  Sites
	broker broker.Gateway
}

// NewService constructs the scheduler service. broker may be nil (writes then
// report unavailable; reads still work).
func NewService(repo Repo, sites Sites, gw broker.Gateway) *Service {
	return &Service{repo: repo, sites: sites, broker: gw}
}

// reName and reSchedule mirror the broker's rules so a bad input is a clear
// rejection here rather than an opaque failure at the broker.
var (
	reSchedule = regexp.MustCompile(`^[A-Za-z0-9 :*/,.~-]{1,128}$`)
)

func (s *Service) requireBroker() error {
	if s.broker == nil {
		return errx.New(errx.KindUnavailable, "broker_unavailable", "The broker is not available; jobs cannot be scheduled.")
	}
	return nil
}

// validate checks a create/update request.
func validate(in *CreateInput) error {
	in.Name = strings.TrimSpace(in.Name)
	in.Command = strings.TrimSpace(in.Command)
	in.Schedule = strings.TrimSpace(in.Schedule)
	if in.Name == "" {
		return errx.Validation("invalid_name", "A job name is required.")
	}
	if in.Command == "" || len(in.Command) > 2000 || strings.ContainsAny(in.Command, "\x00\n\r") {
		return errx.Validation("invalid_command", "A single-line command is required.")
	}
	if !reSchedule.MatchString(in.Schedule) {
		return errx.Validation("invalid_schedule",
			"A schedule is required, in systemd OnCalendar form (e.g. \"daily\", \"*-*-* 02:00:00\", \"Mon *-*-* 00:00:00\").")
	}
	return nil
}

// Create schedules a new job for a site.
func (s *Service) Create(ctx context.Context, siteUID string, in CreateInput) (*Job, error) {
	if err := validate(&in); err != nil {
		return nil, err
	}
	if err := s.requireBroker(); err != nil {
		return nil, err
	}
	ref, err := s.sites.Resolve(ctx, siteUID)
	if err != nil {
		return nil, err
	}
	rec := &Record{SiteID: ref.ID, Name: in.Name, Command: in.Command, Schedule: in.Schedule, Enabled: true}
	if err := s.repo.Insert(ctx, rec); err != nil {
		return nil, err
	}
	if err := s.apply(ctx, ref, rec); err != nil {
		// Roll the row back so a failed apply does not leave a phantom job.
		_ = s.repo.Delete(ctx, rec.UID)
		return nil, err
	}
	return toView(rec), nil
}

// apply asks the broker to (re)write and enable the job's timer.
func (s *Service) apply(ctx context.Context, ref *SiteRef, rec *Record) error {
	_, err := s.broker.Invoke(ctx, "cron.apply", map[string]any{
		"uid": rec.UID, "vhost": ref.LinuxUser, "username": ref.LinuxUser,
		"home": ref.HomeDir, "command": rec.Command, "schedule": rec.Schedule,
	})
	return err
}

// List returns a site's scheduled jobs.
func (s *Service) List(ctx context.Context, siteUID string) ([]Job, error) {
	ref, err := s.sites.Resolve(ctx, siteUID)
	if err != nil {
		return nil, err
	}
	recs, err := s.repo.ListBySiteID(ctx, ref.ID)
	if err != nil {
		return nil, err
	}
	out := make([]Job, len(recs))
	for i := range recs {
		out[i] = *toView(&recs[i])
	}
	return out, nil
}

// SetEnabled enables or disables a job. Disabling removes the systemd timer (the
// definition stays in the DB); enabling re-applies it.
func (s *Service) SetEnabled(ctx context.Context, siteUID, jobUID string, enabled bool) error {
	if err := s.requireBroker(); err != nil {
		return err
	}
	ref, err := s.sites.Resolve(ctx, siteUID)
	if err != nil {
		return err
	}
	rec, err := s.repo.GetByUID(ctx, jobUID)
	if err != nil {
		return err
	}
	if rec.SiteID != ref.ID {
		return errx.NotFound("job_not_found", "No such scheduled job for this site.")
	}
	if enabled {
		rec.Enabled = true
		if err := s.apply(ctx, ref, rec); err != nil {
			return err
		}
	} else if _, err := s.broker.Invoke(ctx, "cron.remove", map[string]any{"uid": jobUID}); err != nil {
		return err
	}
	return s.repo.SetEnabled(ctx, jobUID, enabled)
}

// Run triggers a job immediately.
func (s *Service) Run(ctx context.Context, siteUID, jobUID string) error {
	if err := s.requireBroker(); err != nil {
		return err
	}
	if _, err := s.ownedJob(ctx, siteUID, jobUID); err != nil {
		return err
	}
	_, err := s.broker.Invoke(ctx, "cron.run", map[string]any{"uid": jobUID})
	return err
}

// Logs returns a job's captured output.
func (s *Service) Logs(ctx context.Context, siteUID, jobUID string) (string, error) {
	ref, err := s.sites.Resolve(ctx, siteUID)
	if err != nil {
		return "", err
	}
	rec, err := s.repo.GetByUID(ctx, jobUID)
	if err != nil {
		return "", err
	}
	if rec.SiteID != ref.ID {
		return "", errx.NotFound("job_not_found", "No such scheduled job for this site.")
	}
	if err := s.requireBroker(); err != nil {
		return "", err
	}
	out, err := s.broker.Invoke(ctx, "cron.logs", map[string]any{"uid": jobUID, "home": ref.HomeDir})
	if err != nil {
		return "", err
	}
	log, _ := out["log"].(string)
	return log, nil
}

// Delete removes a job (its timer and its row).
func (s *Service) Delete(ctx context.Context, siteUID, jobUID string) error {
	if err := s.requireBroker(); err != nil {
		return err
	}
	if _, err := s.ownedJob(ctx, siteUID, jobUID); err != nil {
		return err
	}
	if _, err := s.broker.Invoke(ctx, "cron.remove", map[string]any{"uid": jobUID}); err != nil {
		return err
	}
	return s.repo.Delete(ctx, jobUID)
}

// ownedJob loads a job and confirms it belongs to the named site — so one site's
// UID can never act on another's job.
func (s *Service) ownedJob(ctx context.Context, siteUID, jobUID string) (*Record, error) {
	ref, err := s.sites.Resolve(ctx, siteUID)
	if err != nil {
		return nil, err
	}
	rec, err := s.repo.GetByUID(ctx, jobUID)
	if err != nil {
		return nil, err
	}
	if rec.SiteID != ref.ID {
		return nil, errx.NotFound("job_not_found", "No such scheduled job for this site.")
	}
	return rec, nil
}

func toView(r *Record) *Job {
	return &Job{
		UID: r.UID, Name: r.Name, Command: r.Command, Schedule: r.Schedule,
		Enabled: r.Enabled, CreatedAt: r.CreatedAt,
	}
}
