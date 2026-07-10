package audit_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/thisisnkp/heropanel/broker/audit"
)

func appendN(t *testing.T, n int) []audit.Entry {
	t.Helper()
	var got []audit.Entry
	c := audit.NewChain(func(e audit.Entry) error {
		got = append(got, e)
		return nil
	})
	for i := 0; i < n; i++ {
		if _, err := c.Append(audit.Record{
			Actor:      "actor",
			Capability: "service.restart",
			Outcome:    audit.OutcomeSuccess,
			Detail:     fmt.Sprintf("d%d", i),
		}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	return got
}

func TestChainLinksAndVerifies(t *testing.T) {
	got := appendN(t, 3)
	if len(got) != 3 {
		t.Fatalf("got %d entries", len(got))
	}
	if got[0].PrevHash != "" {
		t.Fatal("genesis entry PrevHash should be empty")
	}
	if got[1].PrevHash != got[0].Hash || got[2].PrevHash != got[1].Hash {
		t.Fatal("hash links are not chained")
	}
	if err := audit.Verify(got); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestVerifyDetectsTamper(t *testing.T) {
	got := appendN(t, 3)
	got[1].Detail = "tampered"
	if err := audit.Verify(got); err == nil {
		t.Fatal("expected verify to detect the tampered entry")
	}
}

func TestVerifyDetectsReorder(t *testing.T) {
	got := appendN(t, 3)
	got[0], got[1] = got[1], got[0]
	if err := audit.Verify(got); err == nil {
		t.Fatal("expected verify to detect reordering")
	}
}

func TestAppendSinkErrorRollsBack(t *testing.T) {
	c := audit.NewChain(func(e audit.Entry) error {
		return errors.New("boom")
	})
	if _, err := c.Append(audit.Record{Capability: "x", Outcome: audit.OutcomeIntent}); err == nil {
		t.Fatal("expected error from failing sink")
	}
	seq, prev := c.Head()
	if seq != 0 || prev != "" {
		t.Fatalf("chain must be unchanged after sink failure: seq=%d prev=%q", seq, prev)
	}
}
