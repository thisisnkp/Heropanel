package license

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// The licence server's HTTP surface.
//
// Nothing in this file decides anything about entitlement. It fetches a signed
// token and hands it to the verifier; if the network is down, or the server is
// down, or DNS is hijacked, the caller falls through to the token already on
// disk. **A network failure is not a licence failure** — that rule is what
// makes the licence server a thing that can have a bad afternoon without
// thousands of customers noticing.

const (
	// requestTimeout bounds one attempt. Short on purpose: this call sits in
	// front of nothing a user is waiting for, and a socket held open for a
	// minute is a goroutine and a file descriptor spent on a server that is
	// already not answering.
	requestTimeout = 10 * time.Second

	// maxAttempts for a transient failure. Three is enough to ride out a
	// deploy or a blip; more would just be a slower way to reach the same
	// fallback.
	maxAttempts = 3
)

// ServerError is a refusal the licence server made deliberately, carrying the
// code the panel and the CLI switch on.
type ServerError struct {
	Status  int
	Code    string
	Message string
}

func (e *ServerError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return e.Code
}

// Terminal reports whether retrying could possibly help. A revoked key does not
// become valid because we asked twice, and retrying a refusal only makes the
// operator wait longer for the same answer.
func (e *ServerError) Terminal() bool {
	return e.Status >= 400 && e.Status < 500 && e.Status != http.StatusTooManyRequests
}

// The codes the licence server answers with. Mirrored here as constants rather
// than compared as string literals at each call site, because a typo in a
// literal is a branch that silently never runs.
const (
	CodeInvalidKey             = "INVALID_KEY"
	CodeKeyRevoked             = "KEY_REVOKED"
	CodeSubscriptionExpired    = "SUBSCRIPTION_EXPIRED"
	CodeSeatLimitReached       = "SEAT_LIMIT_REACHED"
	CodeFingerprintChangeLimit = "FINGERPRINT_CHANGE_LIMIT"
	CodeRateLimited            = "RATE_LIMITED"
	CodeBadSignature           = "BAD_SIGNATURE"
	CodeNotActivated           = "NOT_ACTIVATED"
	CodeClockSkew              = "CLOCK_SKEW"
	CodeNonceReplayed          = "NONCE_REPLAYED"
)

// Client talks to the licence server.
type Client struct {
	BaseURL string
	HTTP    *http.Client
	Now     func() time.Time
}

// NewClient builds a client against a licence server root, e.g.
// https://license.nexpanel.io.
func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		HTTP:    &http.Client{Timeout: requestTimeout},
		Now:     time.Now,
	}
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// LicenseInfo is the unsigned summary the server returns alongside a token.
// Only ever used for display: everything enforced on comes from the *claims*,
// which are signed, and this is not.
type LicenseInfo struct {
	LID            string   `json:"lid"`
	Acct           string   `json:"acct"`
	Plan           string   `json:"plan"`
	Features       []string `json:"features"`
	Limits         Limits   `json:"limits"`
	MaxActivations int      `json:"max_activations"`
	ExpiresAt      string   `json:"expires_at"`
}

// ActivateResult is what a successful activation yields.
type ActivateResult struct {
	Token         string      `json:"token"`
	InstallSecret string      `json:"install_secret"`
	RefreshAfter  string      `json:"refresh_after"`
	Reactivated   bool        `json:"reactivated"`
	License       LicenseInfo `json:"license"`
	// FingerprintChange is present when the server recognised this machine as
	// one it already knew under different hardware.
	FingerprintChange *struct {
		Matched           int      `json:"matched"`
		ChangedComponents []string `json:"changed_components"`
		ChangesUsed       int      `json:"changes_used"`
		ChangesAllowed    int      `json:"changes_allowed"`
		// Migration is set when the server recognised this machine across a
		// change to its own component set rather than to the customer's
		// hardware. Charged to nobody's allowance; worth logging differently,
		// because "your hardware changed" would be a lie.
		Migration bool `json:"migration"`
	} `json:"fingerprint_change"`
}

// HeartbeatResult is what a successful heartbeat yields. Token is empty when
// the licence has been revoked — the server sends the command and no lease.
type HeartbeatResult struct {
	Token        string      `json:"token"`
	RefreshAfter string      `json:"refresh_after"`
	Commands     []string    `json:"commands"`
	License      LicenseInfo `json:"license"`
}

// The commands a heartbeat can carry.
const (
	CmdRevoke      = "revoke"
	CmdPlanChanged = "plan_changed"
	CmdForceUpdate = "force_update"
)

// Activate binds this machine to a licence key.
func (c *Client) Activate(ctx context.Context, key string, fp Fingerprint, hostname, osName, version string) (ActivateResult, error) {
	body := map[string]any{
		"key":           key,
		"fingerprint":   fp.Hash,
		"fp_components": fp.Components,
		// Reported so an administrator reviewing this machine can see what it
		// is. Nothing on the server scores these — see fingerprint.go on why
		// the CPU and the MAC are not components.
		"soft_signals":  fp.Signals,
		"hostname":      hostname,
		"os":            osName,
		"panel_version": version,
	}
	var out ActivateResult
	err := c.call(ctx, "/v1/license/activate", body, &out)
	return out, err
}

// Heartbeat refreshes the lease.
//
// Signed with the installation secret rather than the licence key, so the key
// can be forgotten after activation: it is never on disk and never on the wire
// again. A captured heartbeat is worth one machine for five minutes rather than
// a licence forever.
func (c *Client) Heartbeat(
	ctx context.Context,
	lid, fingerprint, secret, hostname, osName, version string,
	signals SoftSignals,
) (HeartbeatResult, error) {
	body, err := signedBody(lid, fingerprint, secret, c.now())
	if err != nil {
		return HeartbeatResult{}, err
	}
	body["hostname"] = hostname
	body["os"] = osName
	body["panel_version"] = version
	// Sent on every beat, not only on activation, so a resize is visible to
	// support the day it happens rather than the next time somebody
	// re-activates. Deliberately outside the HMAC: the signature covers the
	// fields that authenticate the request, and widening it to cover free-text
	// hardware descriptions would mean a machine that renamed its CPU could no
	// longer authenticate.
	body["soft_signals"] = signals

	var out HeartbeatResult
	err = c.call(ctx, "/v1/license/heartbeat", body, &out)
	return out, err
}

// Deactivate releases this machine's activation slot.
func (c *Client) Deactivate(ctx context.Context, lid, fingerprint, secret string) error {
	body, err := signedBody(lid, fingerprint, secret, c.now())
	if err != nil {
		return err
	}
	var out struct {
		OK       bool `json:"ok"`
		Released bool `json:"released"`
	}
	return c.call(ctx, "/v1/license/deactivate", body, &out)
}

// signedBody builds the five fields every authenticated call carries.
//
// The nonce is fresh per attempt and the timestamp is this machine's clock; the
// server refuses a timestamp more than five minutes out and refuses a nonce it
// has already seen inside that window. Between them, a captured request is
// useless: too old, or already spent.
func signedBody(lid, fingerprint, secret string, now time.Time) (map[string]any, error) {
	nonce, err := newNonce()
	if err != nil {
		return nil, err
	}
	ts := now.Unix()
	mac := hmac.New(sha256.New, []byte(secret))
	// The message and its separator are the wire contract. Any change here has
	// to happen on both sides in the same release.
	mac.Write([]byte(lid + "|" + fingerprint + "|" + nonce + "|" + strconv.FormatInt(ts, 10)))
	return map[string]any{
		"lid":         lid,
		"fingerprint": fingerprint,
		"nonce":       nonce,
		"ts":          ts,
		"hmac":        hex.EncodeToString(mac.Sum(nil)),
	}, nil
}

func newNonce() (string, error) {
	var b [18]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("licence: no randomness for a nonce: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// envelope is the licence server's response shape, shared by every endpoint.
type envelope struct {
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// call performs one request with retries, and decodes the envelope.
//
// Retries cover only what a retry can fix: a transport failure, a 5xx, or a
// 429. A deliberate refusal returns immediately — a revoked licence does not
// become valid because we asked three times, and the operator watching a CLI
// deserves the answer now.
func (c *Client) call(ctx context.Context, path string, body map[string]any, out any) error {
	if c.BaseURL == "" {
		return errors.New("licence: no licence server configured")
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}

	var last error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			if err := sleepCtx(ctx, backoff(attempt)); err != nil {
				return err
			}
		}
		err := c.attempt(ctx, path, raw, out)
		if err == nil {
			return nil
		}
		var se *ServerError
		if errors.As(err, &se) && se.Terminal() {
			return err
		}
		last = err
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return last
}

func (c *Client) attempt(ctx context.Context, path string, raw []byte, out any) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: requestTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("licence server unreachable: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body) // let the connection be reused
		_ = resp.Body.Close()
	}()

	// 256 KiB is far more than any response here, and a bound rather than an
	// unbounded read: whatever is on the other end of that URL is not
	// necessarily the licence server.
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if err != nil {
		return fmt.Errorf("licence server response: %w", err)
	}

	var env envelope
	if err := json.Unmarshal(payload, &env); err != nil {
		// Not JSON. A captive portal, a proxy error page, or something that is
		// not the licence server at all — a transport problem, so retryable.
		return fmt.Errorf("licence server returned %d with an unreadable body", resp.StatusCode)
	}
	if env.Error != nil {
		return &ServerError{Status: resp.StatusCode, Code: env.Error.Code, Message: env.Error.Message}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &ServerError{Status: resp.StatusCode, Code: "unexpected",
			Message: fmt.Sprintf("the licence server answered %d", resp.StatusCode)}
	}
	if out == nil || len(env.Data) == 0 {
		return nil
	}
	return json.Unmarshal(env.Data, out)
}

// backoff is exponential with full jitter: 1s, 2s, 4s, each multiplied by a
// random fraction.
//
// The jitter is not decoration. Without it, a fleet that all failed against the
// same outage retries at the same instant, and the server's first moment back
// up is a thundering herd that knocks it down again.
func backoff(attempt int) time.Duration {
	base := time.Second << (attempt - 1)
	if base > 8*time.Second {
		base = 8 * time.Second
	}
	return time.Duration(randInt63n(int64(base))) + base/2
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// randInt63n draws from crypto/rand. math/rand would do for jitter, but a
// process that seeds it identically — which every install of the same binary
// starting from the same image does — would defeat the point of jittering.
func randInt63n(n int64) int64 {
	if n <= 0 {
		return 0
	}
	v, err := rand.Int(rand.Reader, big.NewInt(n))
	if err != nil {
		return n / 2
	}
	return v.Int64()
}
