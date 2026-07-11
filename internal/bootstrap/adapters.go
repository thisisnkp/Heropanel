package bootstrap

import (
	"context"

	"github.com/thisisnkp/heropanel/internal/httpapi"
	"github.com/thisisnkp/heropanel/internal/repository"
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
