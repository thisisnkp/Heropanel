package runtime

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"time"
)

// Health probing for app runtimes.
//
// This exists because "systemd started the unit" and "the app works" are not the
// same claim. Without a probe the panel reports `running` the moment systemd
// forks the process, so an app that crashes on boot, fails to bind, or dies on a
// bad config still shows green — which is exactly when an operator most needs the
// panel to be honest.

const (
	// ProbeTimeout bounds a single probe. An app that cannot answer its own
	// health endpoint in this long is not healthy, whatever it is doing.
	ProbeTimeout = 3 * time.Second

	// ReadyTimeout is how long to wait for an app to come up after a restart
	// before calling it unhealthy. Generous: a JVM or a Next.js server can take
	// several seconds to start listening.
	ReadyTimeout = 20 * time.Second

	// minPoll/maxPoll bound the gap between readiness attempts.
	minPoll = 50 * time.Millisecond
	maxPoll = 500 * time.Millisecond
)

// pollFor spreads roughly 40 attempts across the readiness window, so the poll
// rate scales with the window instead of being a constant that is too coarse for
// a short one and too chatty for a long one.
func pollFor(window time.Duration) time.Duration {
	p := window / 40
	if p < minPoll {
		return minPoll
	}
	if p > maxPoll {
		return maxPoll
	}
	return p
}

// Prober performs one HTTP probe. Injected so tests do not need a live socket.
type Prober interface {
	// Probe returns the response status code, or an error if the request could
	// not be completed.
	Probe(ctx context.Context, url string) (int, error)
}

// httpProber is the default Prober.
type httpProber struct{ client *http.Client }

func newHTTPProber() *httpProber {
	return &httpProber{client: &http.Client{
		Timeout: ProbeTimeout,
		// Never follow a redirect: a 302 is an answer about the app's health, and
		// chasing it could take the probe somewhere it has no business going.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

func (p *httpProber) Probe(ctx context.Context, url string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "HeroPanel-HealthCheck/1")
	res, err := p.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = res.Body.Close() }()
	return res.StatusCode, nil
}

// WithProber overrides the health prober. Returns s for chaining.
func (s *Service) WithProber(p Prober) *Service {
	s.prober = p
	return s
}

// WithReadyTimeout overrides how long a restart waits for the app to answer.
// Returns s for chaining.
func (s *Service) WithReadyTimeout(d time.Duration) *Service {
	s.readyTimeout = d
	return s
}

// readyWindow is the effective readiness timeout.
func (s *Service) readyWindow() time.Duration {
	if s.readyTimeout > 0 {
		return s.readyTimeout
	}
	return ReadyTimeout
}

// healthURL builds the probe target. It is always loopback: the app listens on
// 127.0.0.1 and OpenLiteSpeed reverse-proxies to it, so the panel probes the
// same place the web server does.
func healthURL(port int, path string) string {
	return "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) + path
}

// Health probes a site's app and returns the result. It does not change the
// stored status: this is a question, not an action.
func (s *Service) Health(ctx context.Context, siteUID string) (*Health, error) {
	ref, err := s.sites.Resolve(ctx, siteUID)
	if err != nil {
		return nil, err
	}
	rec, err := s.repo.GetBySiteID(ctx, ref.ID)
	if err != nil {
		return nil, err
	}
	return s.probe(ctx, rec), nil
}

// probe runs one health check against a runtime record.
func (s *Service) probe(ctx context.Context, rec *Record) *Health {
	h := &Health{CheckedAt: time.Now().UTC().Format(time.RFC3339)}
	if rec.HealthPath == "" {
		// No probe configured: say so rather than claiming health we cannot know.
		return h
	}
	h.Configured = true

	ctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()

	start := time.Now()
	code, err := s.prober.Probe(ctx, healthURL(rec.Port, rec.HealthPath))
	h.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		h.Error = err.Error()
		return h
	}
	h.StatusCode = code
	// Any 2xx or 3xx is a served response. Health endpoints are conventionally
	// 200, but a 204 is just as much a "yes", and being strict here would fail
	// perfectly healthy apps for no reason.
	h.Healthy = code >= 200 && code < 400
	return h
}

// waitHealthy polls until the app answers or ReadyTimeout elapses, then reports
// the final result. Returns nil when no probe is configured.
//
// The wait is what makes a restart's reported status trustworthy: an app needs a
// moment to bind its port, and probing once, immediately, would call every
// healthy app broken.
func (s *Service) waitHealthy(ctx context.Context, rec *Record) *Health {
	if rec.HealthPath == "" {
		return nil
	}
	window := s.readyWindow()
	deadline := time.Now().Add(window)
	poll := pollFor(window)
	var last *Health
	for {
		last = s.probe(ctx, rec)
		if last.Healthy || time.Now().After(deadline) {
			return last
		}
		select {
		case <-ctx.Done():
			return last
		case <-time.After(poll):
		}
	}
}

// statusFor maps a probe result to a stored status. A nil health means no probe
// was configured, so the caller's optimistic status stands.
func statusFor(h *Health, optimistic string) string {
	if h == nil {
		return optimistic
	}
	if h.Healthy {
		return StatusRunning
	}
	return StatusError
}
