package monitor

import "context"

// Service health: is the software a host depends on actually running?
//
// Unlike node and site metrics, this is not a number in /proc — it is systemd's
// view of a unit, which the panel reads through the broker's service.status
// capability (the read twin of service.restart, sharing its allowlist). hpd never
// execs systemctl itself; it asks. The result is a simple state string per
// service, driving the dashboard's up/down tiles.

// ServiceHealth is one service's reported state: "active", "inactive", "failed",
// or "unknown".
type ServiceHealth struct {
	Service string `json:"service"`
	State   string `json:"state"`
}

// Broker is the slice of the privileged gateway the service-health reader needs.
type Broker interface {
	Invoke(ctx context.Context, capability string, input any) (map[string]any, error)
}

// DefaultServices is the set the dashboard reports on: the web server, the
// database, and the cache. Every one is on the broker's service allowlist, so a
// status query for it is permitted.
var DefaultServices = []string{"openlitespeed", "mariadb", "redis"}

// NewServiceReader returns a reader that asks the broker for the state of the
// given services. It is wired into the Service with WithServices. On any broker
// error it returns each service as "unknown" rather than dropping the tiles —
// "we could not tell" is itself worth showing.
func NewServiceReader(b Broker, services []string) func(ctx context.Context) []ServiceHealth {
	if b == nil || len(services) == 0 {
		return nil
	}
	return func(ctx context.Context) []ServiceHealth {
		out, err := b.Invoke(ctx, "service.status", map[string]any{"services": services})
		if err != nil {
			return unknownAll(services)
		}
		byName := map[string]string{}
		if rows, ok := out["statuses"].([]any); ok {
			for _, r := range rows {
				m, ok := r.(map[string]any)
				if !ok {
					continue
				}
				svc, _ := m["service"].(string)
				state, _ := m["state"].(string)
				if svc != "" {
					byName[svc] = state
				}
			}
		}
		result := make([]ServiceHealth, 0, len(services))
		for _, s := range services {
			state := byName[s]
			if state == "" {
				state = "unknown"
			}
			result = append(result, ServiceHealth{Service: s, State: state})
		}
		return result
	}
}

func unknownAll(services []string) []ServiceHealth {
	out := make([]ServiceHealth, 0, len(services))
	for _, s := range services {
		out = append(out, ServiceHealth{Service: s, State: "unknown"})
	}
	return out
}

// Services returns a one-shot service-health read for the HTTP endpoint. When no
// reader is configured it returns an empty slice.
func (s *Service) Services(ctx context.Context) []ServiceHealth {
	if s.serviceReader == nil {
		return []ServiceHealth{}
	}
	return s.serviceReader(ctx)
}

// Sites returns a one-shot per-site read for the HTTP endpoint.
func (s *Service) Sites() []SiteSample {
	if s.siteLister == nil {
		return []SiteSample{}
	}
	return s.SiteSamples(s.siteLister())
}
