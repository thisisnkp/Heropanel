// Package audit implements hpd's tamper-evident, hash-chained audit log: who
// did what, to which resource, from where, with what outcome, and when.
//
// It is the panel-side counterpart to the broker's in-process chain
// (broker/audit): the broker records that a privileged capability ran, this
// records the human or API key that asked for it. Both are needed — the broker
// cannot see the session behind a request, and hpd cannot vouch for what
// happened after the socket.
//
// The chain follows docs/05-security-architecture.md §9:
//
//	row_hash = SHA-256(prev_hash || canonical(row))
//
// Any edit to a committed row breaks every hash after it, which Verify detects.
// This makes tampering *evident*; it does not make it impossible. An attacker
// with write access to the table can still rewrite the whole chain from the
// edited row forward, so the chain's value depends on the head being witnessed
// somewhere the attacker does not control (an export to a remote sink is the
// documented follow-up).
package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Outcome classifies how an audited action ended.
type Outcome string

const (
	OutcomeSuccess Outcome = "success"
	OutcomeFailure Outcome = "failure"
	OutcomeDenied  Outcome = "denied" // authn/authz refused it
)

// ActorKind identifies what sort of principal acted. docs/05 §9.
type ActorKind string

const (
	ActorUser      ActorKind = "user"      // a logged-in human
	ActorAPIKey    ActorKind = "apikey"    // a programmatic key
	ActorAnonymous ActorKind = "anonymous" // unauthenticated (a failed login, a webhook)
	ActorSystem    ActorKind = "system"    // hpd itself (schedulers, sweepers)
)

// tsLayout is the canonical timestamp form. The hash covers the timestamp *as
// persisted*, so this has to be the exact text both engines hand back.
//
// The six decimal places are not decoration. MariaDB types this column DATETIME(6)
// and always renders it with six fractional digits: write "10:30:15" and it
// returns "10:30:15.000000". SQLite's TEXT column returns whatever it was given.
// Writing microseconds explicitly is the one format that survives both
// unchanged — with the repository's second-precision layout, every row on
// MariaDB would come back one suffix longer than it went in and fail Verify,
// while SQLite (what the tests run on) would pass happily.
//
// This is also why the audit package keeps its own layout instead of borrowing
// the repository's: they are answering different questions. The repository needs
// timestamps that sort; this needs bytes that round-trip.
const tsLayout = "2006-01-02 15:04:05.000000"

// FormatTime renders t in the canonical, hashable form.
func FormatTime(t time.Time) string { return t.UTC().Format(tsLayout) }

// emptyDetail is the zero value for the detail column. It is "{}" rather than ""
// because MariaDB types the column as JSON and rejects a bare empty string,
// while SQLite's TEXT column accepts either.
const emptyDetail = "{}"

// Entry is a committed audit row.
type Entry struct {
	ID           int64     `json:"-"`
	UID          string    `json:"uid"`
	ActorUserID  int64     `json:"actor_user_id"` // 0 when there is no user (anonymous/system)
	ActorIP      string    `json:"actor_ip"`
	ActorKind    ActorKind `json:"actor_kind"`
	Action       string    `json:"action"`
	ResourceType string    `json:"resource_type"`
	ResourceID   string    `json:"resource_id"`
	Outcome      Outcome   `json:"outcome"`
	Detail       string    `json:"detail"` // JSON object; "{}" when empty
	PrevHash     string    `json:"prev_hash"`
	RowHash      string    `json:"row_hash"`
	CreatedAt    string    `json:"created_at"` // canonical form; see tsLayout
}

// Record is the input to Service.Record. The chain fields (UID, PrevHash,
// RowHash, CreatedAt) are assigned on commit.
type Record struct {
	ActorUserID  int64
	ActorIP      string
	ActorKind    ActorKind
	Action       string
	ResourceType string
	ResourceID   string
	Outcome      Outcome
	Detail       string    // JSON object, or "" for none
	At           time.Time // zero => now
}

// hashEntry computes row_hash over prev_hash || canonical(e), excluding RowHash
// itself. The field order is fixed and the separator is the ASCII unit
// separator, which cannot appear in any of these values, so no field can be
// shifted into another to forge a colliding row.
func hashEntry(e Entry) string {
	canonical := strings.Join([]string{
		e.UID,
		e.CreatedAt,
		strconv.FormatInt(e.ActorUserID, 10),
		e.ActorIP,
		string(e.ActorKind),
		e.Action,
		e.ResourceType,
		e.ResourceID,
		string(e.Outcome),
		e.Detail,
	}, "\x1f")
	sum := sha256.Sum256([]byte(e.PrevHash + "\x1f" + canonical))
	return hex.EncodeToString(sum[:])
}

// Verify checks that entries — in ascending id order — form an unbroken chain.
// The caller supplies the hash the chain is expected to start from ("" for the
// first row ever written), so a page of entries can be verified without loading
// the whole table.
func Verify(entries []Entry, startPrev string) error {
	prev := startPrev
	for _, e := range entries {
		if e.PrevHash != prev {
			return fmt.Errorf("audit: broken link at uid %s: prev_hash does not match the preceding row", e.UID)
		}
		if hashEntry(e) != e.RowHash {
			return fmt.Errorf("audit: tampered entry at uid %s: contents do not match row_hash", e.UID)
		}
		prev = e.RowHash
	}
	return nil
}
