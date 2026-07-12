package bootstrap

import (
	"context"
	"strings"

	"github.com/thisisnkp/heropanel/internal/auth"
	"github.com/thisisnkp/heropanel/internal/httpapi"
	"github.com/thisisnkp/heropanel/internal/job"
	"github.com/thisisnkp/heropanel/internal/repository"
	"github.com/thisisnkp/heropanel/internal/ws"
)

// userDirectoryAdapter adapts the user repository to httpapi.UserDirectory,
// mapping persistence rows to the API view. Living in the composition root keeps
// httpapi decoupled from the repository package.
type userDirectoryAdapter struct {
	repo *repository.UserRepository
}

func (a *userDirectoryAdapter) List(ctx context.Context, limit, offset int) ([]httpapi.UserSummary, error) {
	rows, err := a.repo.List(ctx, limit, offset)
	if err != nil {
		return nil, err
	}
	out := make([]httpapi.UserSummary, len(rows))
	for i, u := range rows {
		out[i] = httpapi.UserSummary{
			UID:         u.UID,
			Email:       u.Email,
			Username:    u.Username,
			DisplayName: u.DisplayName,
			Status:      u.Status,
		}
	}
	return out, nil
}

// jobChannelAuthorizer authorizes WebSocket channel subscriptions. A principal
// may subscribe to "job:<uid>" only if they own that job (or are an admin).
func jobChannelAuthorizer(jobs *job.Dispatcher) ws.Authorizer {
	return ws.AuthorizerFunc(func(ctx context.Context, p *auth.Principal, channel string) bool {
		if p == nil {
			return false
		}
		if p.Can("*") {
			return true
		}
		if uid, ok := strings.CutPrefix(channel, "job:"); ok {
			j, err := jobs.Get(ctx, uid)
			if err != nil {
				return false
			}
			return j.OwnerUserID.Valid && j.OwnerUserID.Int64 == p.UserID
		}
		return false // unknown channel family -> deny
	})
}
