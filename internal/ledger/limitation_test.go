package ledger

import (
	"testing"

	"ledgercore/internal/money"
)

// racingStore is a Store in which a second writer gets to act at the worst
// possible moment: after the first writer has read the records and decided,
// and before its decision is appended. Between two processes sharing a real
// database this interleaving needs no help; here it is forced so that the
// test fails the same way on every run instead of once in a thousand.
type racingStore struct {
	Store
	beforeAuthAppend func()
}

func (s *racingStore) AppendAuthEvent(e AuthEvent) (AuthEvent, error) {
	if hook := s.beforeAuthAppend; hook != nil {
		s.beforeAuthAppend = nil
		hook()
	}
	return s.Store.AppendAuthEvent(e)
}

// TestKnownLimitation_NoAtomicCheckAndHold is the ONE test in this repository
// that is EXPECTED TO FAIL. It is not a bug in the test and it must not be
// "fixed" by weakening the assertion. `go test ./...` reports exactly this
// failure; everything else passes (`go test ./... -skip TestKnownLimitation`).
//
// What it asserts: rule 5. An authorization is approved only if the available
// balance stays at or above zero once its hold is applied. So after any
// sequence of approvals, available balance must not be negative.
//
// Why it fails: Authorize reads the entries and holds, decides, then appends
// the decision. Those are separate calls on the Store, and the Store
// interface offers nothing that makes them one step: it has Append and reads,
// no conditional append, no transaction, no lock, no version to compare. The
// only thing that makes check-and-hold atomic in this design is the mutex
// inside one Ledger value, and that protects one Ledger in one process. Here
// two Ledger values share one store, which is exactly the shape of two
// application instances over one database. Both read AED 250.00 available,
// both approve AED 200.00, and the account ends with AED 400.00 held against
// AED 250.00.
//
// What that reveals about the design: "the store is behind an interface, so
// moving to a database is just another implementation" is not true. A
// database-backed MemStore replacement that satisfies this interface
// perfectly would still double-approve under concurrency. Making this test
// pass needs a change to the Store contract itself (an append that is
// conditional on the account's last sequence number, or a per-account lock or
// serializable transaction spanning the read and the append), and then a
// retry path in Authorize. The same gap applies to every other
// read-then-append in the ledger (Settle, Reverse, CloseDay). See
// ARCHITECTURE.md section 4.
func TestKnownLimitation_NoAtomicCheckAndHold(t *testing.T) {
	const account AccountID = "ACC-001"
	store := &racingStore{Store: NewMemStore()}
	nodeA := New(store, DefaultConfig())
	nodeB := New(store, DefaultConfig())
	for _, node := range []*Ledger{nodeA, nodeB} {
		if err := node.OpenAccount(account, money.AED); err != nil {
			t.Fatalf("open: %v", err)
		}
	}
	if _, err := nodeA.Credit(Posting{EventID: "E1", Account: account, Amount: aed("250.00"), Booked: 1, Value: 1}); err != nil {
		t.Fatalf("credit: %v", err)
	}

	// Node B's request arrives after node A has checked the balance and
	// before node A has recorded its hold.
	var fromB Authorization
	store.beforeAuthAppend = func() {
		var err error
		fromB, err = nodeB.Authorize(AuthRequest{EventID: "B1", Account: account, AuthID: "Auth-B1", Amount: aed("200.00"), Booked: 1, Value: 1})
		if err != nil {
			t.Fatalf("node B authorize: %v", err)
		}
	}
	fromA, err := nodeA.Authorize(AuthRequest{EventID: "A1", Account: account, AuthID: "Auth-A1", Amount: aed("200.00"), Booked: 1, Value: 1})
	if err != nil {
		t.Fatalf("node A authorize: %v", err)
	}

	available, err := nodeA.Available(account, 1)
	if err != nil {
		t.Fatalf("Available: %v", err)
	}
	if available.IsNegative() {
		t.Errorf("EXPECTED FAILURE (known limitation, see the comment on this test): "+
			"rule 5 broken across two writers. Ledger balance AED 250.00; node A's hold of AED 200.00 was %s, "+
			"node B's hold of AED 200.00 was %s; available balance is now %s, want not negative. "+
			"The Store interface has no atomic check-and-hold.",
			fromA.Status, fromB.Status, available)
	}
}
