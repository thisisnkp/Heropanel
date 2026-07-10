// Package audit implements the broker's tamper-evident, hash-chained audit log.
//
// Each entry's Hash = SHA-256(canonical(entry-without-hash)), where the entry
// embeds the previous entry's Hash. Any modification to a past entry breaks the
// chain and is detectable by Verify. See docs/05-security-architecture.md §9.
package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Outcome classifies an audited action.
type Outcome string

const (
	OutcomeIntent  Outcome = "intent" // recorded before execution
	OutcomeSuccess Outcome = "success"
	OutcomeFailure Outcome = "failure"
	OutcomeDenied  Outcome = "denied"
)

// Record is the input to Append (Seq, PrevHash and Hash are assigned by Chain).
type Record struct {
	Actor      string    // correlation: who requested (from hpd)
	Capability string    // capability name
	Outcome    Outcome   // intent/success/failure/denied
	Detail     string    // sanitized detail (never secrets or raw stderr)
	At         time.Time // if zero, Append uses time.Now()
}

// Entry is a committed, hash-linked audit record.
type Entry struct {
	Seq        uint64    `json:"seq"`
	At         time.Time `json:"at"`
	Actor      string    `json:"actor"`
	Capability string    `json:"capability"`
	Outcome    Outcome   `json:"outcome"`
	Detail     string    `json:"detail"`
	PrevHash   string    `json:"prev_hash"`
	Hash       string    `json:"hash"`
}

// Sink persists a committed entry (file, DB, remote SIEM). Returning an error
// fails the Append so a privileged action is not recorded silently.
type Sink func(Entry) error

// Chain is an append-only, hash-chained sequence of audit entries. Safe for
// concurrent use.
type Chain struct {
	mu   sync.Mutex
	seq  uint64
	prev string
	sink Sink
}

// NewChain creates a Chain that writes committed entries to sink (may be nil).
func NewChain(sink Sink) *Chain {
	return &Chain{sink: sink}
}

// Append commits r to the chain and returns the committed Entry.
func (c *Chain) Append(r Record) (Entry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	at := r.At
	if at.IsZero() {
		at = time.Now().UTC()
	}
	c.seq++
	e := Entry{
		Seq:        c.seq,
		At:         at,
		Actor:      r.Actor,
		Capability: r.Capability,
		Outcome:    r.Outcome,
		Detail:     r.Detail,
		PrevHash:   c.prev,
	}
	e.Hash = hashEntry(e)

	if c.sink != nil {
		if err := c.sink(e); err != nil {
			// Roll back so the in-memory chain matches what was persisted.
			c.seq--
			return Entry{}, fmt.Errorf("audit: sink failed: %w", err)
		}
	}
	c.prev = e.Hash
	return e, nil
}

// Head returns the current sequence number and last hash.
func (c *Chain) Head() (seq uint64, hash string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.seq, c.prev
}

// Verify checks that entries form an unbroken chain: monotonically increasing
// sequence numbers, correct hash links, and untampered content.
func Verify(entries []Entry) error {
	prev := ""
	for i, e := range entries {
		if e.Seq != uint64(i+1) {
			return fmt.Errorf("audit: non-contiguous seq at index %d: got %d, want %d", i, e.Seq, i+1)
		}
		if e.PrevHash != prev {
			return fmt.Errorf("audit: broken link at seq %d", e.Seq)
		}
		if hashEntry(e) != e.Hash {
			return fmt.Errorf("audit: tampered entry at seq %d", e.Seq)
		}
		prev = e.Hash
	}
	return nil
}

// hashEntry computes the hash over the canonical form of e, excluding e.Hash.
func hashEntry(e Entry) string {
	// Unit-separator-joined canonical form with a fixed field order.
	canonical := strings.Join([]string{
		strconv.FormatUint(e.Seq, 10),
		strconv.FormatInt(e.At.UnixNano(), 10),
		e.Actor,
		e.Capability,
		string(e.Outcome),
		e.Detail,
		e.PrevHash,
	}, "\x1f")
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}
