package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/thisisnkp/heropanel/internal/repository"
	"github.com/thisisnkp/heropanel/pkg/errx"
	"github.com/thisisnkp/heropanel/pkg/idgen"
)

// apiKeyPrefix is the human-recognizable, non-secret key prefix; keyPrefixLen
// characters are stored for O(1) lookup.
const (
	apiKeyPrefix = "hp_"
	keyPrefixLen = 14 // "hp_" + 11 chars
)

// APIKeyView is the API representation of a key (never includes the secret).
type APIKeyView struct {
	UID       string   `json:"uid"`
	Name      string   `json:"name"`
	Prefix    string   `json:"prefix"`
	Scopes    []string `json:"scopes"`
	CreatedAt string   `json:"created_at"`
}

// WithAPIKeys enables API-key support on the service (repository provided at
// bootstrap). Returns the service for chaining.
func (s *Service) WithAPIKeys(repo *repository.APIKeyRepository) *Service {
	s.apiKeys = repo
	return s
}

// newAPIKey generates a fresh key and its lookup prefix.
func newAPIKey() (full, prefix string, err error) {
	b := make([]byte, 24)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	full = apiKeyPrefix + base64.RawURLEncoding.EncodeToString(b)
	prefix = full[:keyPrefixLen]
	return full, prefix, nil
}

// CreateAPIKey creates a scoped API key for a user, returning the plaintext key
// once (it is never retrievable again).
func (s *Service) CreateAPIKey(ctx context.Context, userID int64, name string, scopes []string) (string, *APIKeyView, error) {
	if s.apiKeys == nil {
		return "", nil, errx.New(errx.KindUnavailable, "api_keys_unavailable", "API keys are not available.")
	}
	if strings.TrimSpace(name) == "" {
		return "", nil, errx.Validation("invalid_name", "A key name is required.")
	}
	if scopes == nil {
		scopes = []string{}
	}
	full, prefix, err := newAPIKey()
	if err != nil {
		return "", nil, errx.Internal(err)
	}
	scopesJSON, _ := json.Marshal(scopes)
	row := &repository.APIKeyRow{
		UID:     idgen.NewULID(),
		UserID:  userID,
		Name:    name,
		Prefix:  prefix,
		KeyHash: hashToken(full),
		Scopes:  scopesJSON,
	}
	if err := s.apiKeys.Create(ctx, row); err != nil {
		return "", nil, err
	}
	return full, apiKeyView(row), nil
}

// ListAPIKeys returns a user's active keys.
func (s *Service) ListAPIKeys(ctx context.Context, userID int64) ([]APIKeyView, error) {
	if s.apiKeys == nil {
		return []APIKeyView{}, nil
	}
	rows, err := s.apiKeys.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]APIKeyView, len(rows))
	for i := range rows {
		out[i] = *apiKeyView(&rows[i])
	}
	return out, nil
}

// RevokeAPIKey revokes a user's key.
func (s *Service) RevokeAPIKey(ctx context.Context, userID int64, uid string) error {
	if s.apiKeys == nil {
		return errx.New(errx.KindUnavailable, "api_keys_unavailable", "API keys are not available.")
	}
	return s.apiKeys.Revoke(ctx, userID, uid, s.now())
}

// AuthenticateAPIKey resolves the principal for a bearer API key. The principal's
// permissions are the key's scopes (so a key can be narrower than its owner).
func (s *Service) AuthenticateAPIKey(ctx context.Context, key string) (*Principal, error) {
	if s.apiKeys == nil || !strings.HasPrefix(key, apiKeyPrefix) || len(key) < keyPrefixLen {
		return nil, errx.Unauthorized("invalid_api_key", "Invalid API key.")
	}
	row, err := s.apiKeys.GetActiveByPrefix(ctx, key[:keyPrefixLen], s.now())
	if err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare([]byte(hashToken(key)), []byte(row.KeyHash)) != 1 {
		return nil, errx.Unauthorized("invalid_api_key", "Invalid API key.")
	}
	_ = s.apiKeys.TouchLastUsed(ctx, row.ID, s.now())

	u, err := s.users.GetByID(ctx, row.UserID)
	if err != nil {
		return nil, errx.Unauthorized("invalid_api_key", "Invalid API key.")
	}
	if u.Status != "active" {
		return nil, errx.Forbidden("account_inactive", "This account is not active.")
	}
	var scopes []string
	_ = json.Unmarshal(row.Scopes, &scopes)
	return &Principal{
		UserID:      u.ID,
		UserUID:     u.UID,
		Email:       u.Email,
		Username:    u.Username,
		DisplayName: u.DisplayName,
		Permissions: scopes,
	}, nil
}

func apiKeyView(r *repository.APIKeyRow) *APIKeyView {
	var scopes []string
	_ = json.Unmarshal(r.Scopes, &scopes)
	if scopes == nil {
		scopes = []string{}
	}
	return &APIKeyView{UID: r.UID, Name: r.Name, Prefix: r.Prefix, Scopes: scopes, CreatedAt: r.CreatedAt}
}
