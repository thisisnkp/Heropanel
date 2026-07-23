package capabilities

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// The mail queue and per-mailbox quota, read and managed through postfix's and
// dovecot's own admin tools — the panel never gropes around the spool.

const (
	postqueuePath = "/usr/sbin/postqueue"
	postsuperPath = "/usr/sbin/postsuper"
)

// reQueueID accepts postfix queue IDs — short (uppercase hex) and long
// (base-52ish) formats both fall inside this charset, which is argv-safe.
var reQueueID = regexp.MustCompile(`^[A-Za-z0-9]{5,32}$`)

// maxQueueJSON bounds the queue listing handed back to hpd. postqueue -j is
// one JSON object per line; a megabyte is thousands of queued messages, which
// is already a "the panel shows the top of it" situation.
const maxQueueJSON = 1 << 20

// maxQueueDelete bounds one delete batch.
const maxQueueDelete = 100

// ── mail.queue.list ──────────────────────────────────────────────────────────

// MailQueueList returns the raw `postqueue -j` output (JSON lines). Parsing
// stays in hpd — the broker's job is running the pinned binary, not modeling
// postfix's schema.
type MailQueueList struct{}

// Name implements capability.Capability.
func (MailQueueList) Name() string { return "mail.queue.list" }

// Execute implements capability.Capability.
func (MailQueueList) Execute(c capability.Context, _ json.RawMessage) (capability.Result, error) {
	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: postqueuePath, Args: []string{"-j"}, Timeout: 60 * time.Second,
	})
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "queue_list_failed", "Could not read the mail queue.")
	}
	// postqueue exits non-zero when postfix is down; an empty queue is exit 0
	// with no output. Both are answers, not errors the operator can act on
	// differently from "the queue view shows nothing".
	out := string(res.Stdout)
	if len(out) > maxQueueJSON {
		out = out[:maxQueueJSON]
		// Cut at the last complete line so hpd never parses a torn object.
		if i := strings.LastIndexByte(out, '\n'); i > 0 {
			out = out[:i+1]
		}
	}
	return capability.Result{Data: map[string]any{
		"raw": out, "running": res.ExitCode == 0,
	}}, nil
}

// ── mail.queue.flush ─────────────────────────────────────────────────────────

// MailQueueFlush asks postfix to attempt delivery of everything deferred.
type MailQueueFlush struct{}

// Name implements capability.Capability.
func (MailQueueFlush) Name() string { return "mail.queue.flush" }

// Execute implements capability.Capability.
func (MailQueueFlush) Execute(c capability.Context, _ json.RawMessage) (capability.Result, error) {
	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: postqueuePath, Args: []string{"-f"}, Timeout: 60 * time.Second,
	})
	if err != nil || res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "queue_flush_failed", "Could not flush the mail queue.")
	}
	return capability.Result{Data: map[string]any{"flushed": true}}, nil
}

// ── mail.queue.delete ────────────────────────────────────────────────────────

// MailQueueDelete removes specific messages by queue ID. Explicit IDs only —
// there is deliberately no "delete ALL": destroying the whole queue is not a
// button, and a compromised hpd must not be able to make mail disappear
// wholesale through this capability.
type MailQueueDelete struct{}

type mailQueueDeleteInput struct {
	IDs []string `json:"ids"`
}

// Name implements capability.Capability.
func (MailQueueDelete) Name() string { return "mail.queue.delete" }

// Execute implements capability.Capability.
func (MailQueueDelete) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in mailQueueDeleteInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for mail.queue.delete.")
	}
	if len(in.IDs) == 0 || len(in.IDs) > maxQueueDelete {
		return capability.Result{}, errx.Validation("bad_input", "Between 1 and 100 queue IDs per call.")
	}
	for _, id := range in.IDs {
		if !reQueueID.MatchString(id) || strings.EqualFold(id, "ALL") {
			return capability.Result{}, errx.Validation("invalid_queue_id", "Invalid queue ID.")
		}
	}
	deleted := 0
	for _, id := range in.IDs {
		res, err := c.Runner.Run(c.Ctx, exec.Command{
			Path: postsuperPath, Args: []string{"-d", id}, Timeout: 30 * time.Second,
		})
		// A message delivered between list and delete is gone either way;
		// postsuper says so with a warning and exit 0. A real failure stops.
		if err != nil || res.ExitCode != 0 {
			return capability.Result{Data: map[string]any{"deleted": deleted}},
				errx.New(errx.KindUpstream, "queue_delete_failed", "Could not delete queue message "+id+".")
		}
		deleted++
	}
	return capability.Result{Data: map[string]any{"deleted": deleted}}, nil
}

// ── mail.quota ───────────────────────────────────────────────────────────────

// MailQuota reads per-mailbox usage through doveadm. One address per doveadm
// call (its -u takes one user); a mailbox that has never been delivered to has
// no quota state yet, which is reported as known=false rather than an error.
type MailQuota struct{}

type mailQuotaInput struct {
	Addresses []string `json:"addresses"`
}

// Name implements capability.Capability.
func (MailQuota) Name() string { return "mail.quota" }

// maxQuotaBatch bounds one quota sweep.
const maxQuotaBatch = 200

// reMailAddress bounds a full address for argv safety (local@fqdn, the
// module's own charsets — hpd already validated semantics).
var reMailAddress = regexp.MustCompile(`^[a-z0-9][a-z0-9._+-]{0,63}@[a-z0-9.-]{1,253}$`)

// Execute implements capability.Capability.
func (MailQuota) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in mailQuotaInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for mail.quota.")
	}
	if len(in.Addresses) == 0 || len(in.Addresses) > maxQuotaBatch {
		return capability.Result{}, errx.Validation("bad_input", "Between 1 and 200 addresses per call.")
	}
	for _, a := range in.Addresses {
		if !reMailAddress.MatchString(a) {
			return capability.Result{}, errx.Validation("invalid_address", "Invalid mailbox address.")
		}
	}

	out := map[string]any{}
	for _, addr := range in.Addresses {
		res, err := c.Runner.Run(c.Ctx, exec.Command{
			Path: doveadmPath, Args: []string{"quota", "get", "-u", addr}, Timeout: 30 * time.Second,
		})
		if err != nil || res.ExitCode != 0 {
			out[addr] = map[string]any{"known": false}
			continue
		}
		used, limit, ok := parseDoveadmQuota(string(res.Stdout))
		out[addr] = map[string]any{"known": ok, "used_kb": used, "limit_kb": limit}
	}
	return capability.Result{Data: map[string]any{"quotas": out}}, nil
}

// parseDoveadmQuota pulls the STORAGE row out of `doveadm quota get` output:
//
//	Quota name Type    Value Limit  %
//	User quota STORAGE   102  1024  9
//
// Values are KB; a "-" limit means unlimited (0 here).
func parseDoveadmQuota(out string) (usedKB, limitKB int64, ok bool) {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		for i, f := range fields {
			if f == "STORAGE" && len(fields) >= i+3 {
				usedKB = parseKB(fields[i+1])
				limitKB = parseKB(fields[i+2])
				return usedKB, limitKB, true
			}
		}
	}
	return 0, 0, false
}

func parseKB(s string) int64 {
	if s == "-" {
		return 0
	}
	var n int64
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int64(r-'0')
	}
	return n
}
