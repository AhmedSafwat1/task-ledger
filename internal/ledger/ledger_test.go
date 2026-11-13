package ledger

import (
	"errors"
	"testing"

	"ledgercore/internal/money"
)

const (
	acc1 AccountID = "ACC-001" // AED
	acc2 AccountID = "ACC-002" // BHD
)

// newLedger returns a ledger over a fresh in-memory store with both accounts
// open. Interest is switched off, so tests about postings, holds and fees see
// balances with no capitalization credit in them; interest tests use
// newLedgerWith(t, DefaultConfig()).
func newLedger(t *testing.T) (*Ledger, *MemStore) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.InterestNumerator = 0
	cfg.CapitalizationDay = 0
	return newLedgerWith(t, cfg)
}

func newLedgerWith(t *testing.T, cfg Config) (*Ledger, *MemStore) {
	t.Helper()
	store := NewMemStore()
	l := New(store, cfg)
	if err := l.OpenAccount(acc1, money.AED); err != nil {
		t.Fatalf("open %s: %v", acc1, err)
	}
	if err := l.OpenAccount(acc2, money.BHD); err != nil {
		t.Fatalf("open %s: %v", acc2, err)
	}
	return l, store
}

func mustCredit(t *testing.T, l *Ledger, id string, amount money.Money, booked, value Day) Entry {
	t.Helper()
	e, err := l.Credit(Posting{EventID: id, Account: acc1, Amount: amount, Booked: booked, Value: value})
	if err != nil {
		t.Fatalf("credit %s: %v", id, err)
	}
	return e
}

func mustDebit(t *testing.T, l *Ledger, id string, amount money.Money, booked, value Day) Entry {
	t.Helper()
	e, err := l.Debit(Posting{EventID: id, Account: acc1, Amount: amount, Booked: booked, Value: value})
	if err != nil {
		t.Fatalf("debit %s: %v", id, err)
	}
	return e
}

func wantBalance(t *testing.T, l *Ledger, id AccountID, value, booked Day, want money.Money) {
	t.Helper()
	got, err := l.Balance(id, value, booked)
	if err != nil {
		t.Fatalf("Balance(%s, value %d, booked %d): %v", id, value, booked, err)
	}
	if got != want {
		t.Errorf("Balance(%s, value day %d, as known on day %d) = %s, want %s", id, value, booked, got, want)
	}
}

func TestOpeningBalanceIsZeroInTheAccountCurrency(t *testing.T) {
	l, _ := newLedger(t)
	wantBalance(t, l, acc1, 1, 1, aed("0.00"))
	wantBalance(t, l, acc2, 1, 1, bhd("0.000"))
}

func TestBalanceIsTheSumOfEntries(t *testing.T) {
	l, _ := newLedger(t)
	mustCredit(t, l, "E1", aed("1,200.00"), 1, 1)
	mustDebit(t, l, "E2", aed("950.00"), 1, 1)
	mustCredit(t, l, "E4", aed("400.00"), 3, 3)

	wantBalance(t, l, acc1, 1, 1, aed("250.00"))
	wantBalance(t, l, acc1, 2, 2, aed("250.00"))
	wantBalance(t, l, acc1, 3, 3, aed("650.00"))
}

func TestBalanceIsDerivedFromTheStoreNotCached(t *testing.T) {
	l, store := newLedger(t)
	mustCredit(t, l, "E1", aed("1,200.00"), 1, 1)
	wantBalance(t, l, acc1, 1, 1, aed("1,200.00"))

	// An entry appended behind the ledger's back must show up: there is no
	// stored balance for it to disagree with.
	if _, err := store.AppendEntry(Entry{Account: acc1, Kind: Debit, Amount: aed("-950.00"), Booked: 1, Value: 1}); err != nil {
		t.Fatalf("AppendEntry: %v", err)
	}
	wantBalance(t, l, acc1, 1, 1, aed("250.00"))
}

func TestBitemporalBalance(t *testing.T) {
	l, _ := newLedger(t)
	mustCredit(t, l, "E1", aed("1,200.00"), 1, 1)
	mustDebit(t, l, "E2", aed("950.00"), 1, 1)
	mustCredit(t, l, "E4", aed("400.00"), 3, 3)
	mustDebit(t, l, "E7", aed("620.00"), 5, 2) // booked Day 5, value-dated Day 2

	tests := []struct {
		name          string
		value, booked Day
		want          string
	}{
		{"Day 2 as known on Day 2", 2, 2, "250.00"},
		{"Day 2 as known on Day 4, before the backdated debit", 2, 4, "250.00"},
		{"Day 2 as known on Day 5, after the backdated debit", 2, 5, "-370.00"},
		{"Day 1 is before the value date and never moves", 1, 5, "250.00"},
		{"Day 3 as known on Day 5", 3, 5, "30.00"},
		{"Day 3 as known on Day 3", 3, 3, "650.00"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wantBalance(t, l, acc1, tc.value, tc.booked, aed(tc.want))
		})
	}
}

func TestBalanceAtSeqSeesOnlyEarlierAppends(t *testing.T) {
	l, store := newLedger(t)
	mustCredit(t, l, "E1", aed("1,200.00"), 1, 1)
	mustDebit(t, l, "E2", aed("950.00"), 1, 1)
	before := store.LastSeq()
	mustDebit(t, l, "E7", aed("620.00"), 5, 2)

	got, err := l.BalanceAtSeq(acc1, 2, before)
	if err != nil {
		t.Fatalf("BalanceAtSeq: %v", err)
	}
	if got != aed("250.00") {
		t.Errorf("Day 2 balance up to seq %d = %s, want AED 250.00", before, got)
	}
	got, err = l.BalanceAtSeq(acc1, 2, store.LastSeq())
	if err != nil {
		t.Fatalf("BalanceAtSeq: %v", err)
	}
	if got != aed("-370.00") {
		t.Errorf("Day 2 balance up to latest seq = %s, want AED -370.00", got)
	}
}

func TestForwardValueDatedEntryDoesNotCountBeforeItsValueDay(t *testing.T) {
	l, _ := newLedger(t)
	mustCredit(t, l, "E1", aed("100.00"), 1, 3) // known on Day 1, counts from Day 3

	wantBalance(t, l, acc1, 1, 1, aed("0.00"))
	wantBalance(t, l, acc1, 2, 2, aed("0.00"))
	wantBalance(t, l, acc1, 3, 3, aed("100.00"))
}

func TestDebitMayOverdraw(t *testing.T) {
	l, _ := newLedger(t)
	mustDebit(t, l, "E1", aed("620.00"), 1, 1)
	wantBalance(t, l, acc1, 1, 1, aed("-620.00"))
}

func TestBalanceOfUnknownAccount(t *testing.T) {
	l, _ := newLedger(t)
	if _, err := l.Balance("ACC-404", 1, 1); !errors.Is(err, ErrUnknownAccount) {
		t.Errorf("Balance of unknown account error = %v, want ErrUnknownAccount", err)
	}
}

func TestOpenAccountTwiceFails(t *testing.T) {
	l, _ := newLedger(t)
	if err := l.OpenAccount(acc1, money.AED); !errors.Is(err, ErrDuplicateAccount) {
		t.Errorf("opening %s twice error = %v, want ErrDuplicateAccount", acc1, err)
	}
}

func TestRejectedPostingMovesNoFundsAndIsRecorded(t *testing.T) {
	tests := []struct {
		name    string
		posting Posting
		want    error
	}{
		{"unknown account", Posting{EventID: "X1", Account: "ACC-404", Amount: aed("1.00"), Booked: 1, Value: 1}, ErrUnknownAccount},
		{"amount in another currency", Posting{EventID: "X2", Account: acc1, Amount: bhd("1.000"), Booked: 1, Value: 1}, money.ErrCurrencyMismatch},
		{"zero amount", Posting{EventID: "X3", Account: acc1, Amount: aed("0.00"), Booked: 1, Value: 1}, ErrInvalidAmount},
		{"negative amount", Posting{EventID: "X4", Account: acc1, Amount: aed("-5.00"), Booked: 1, Value: 1}, ErrInvalidAmount},
		{"booked day zero", Posting{EventID: "X5", Account: acc1, Amount: aed("1.00"), Booked: 0, Value: 1}, ErrInvalidDay},
		{"value day zero", Posting{EventID: "X6", Account: acc1, Amount: aed("1.00"), Booked: 1, Value: 0}, ErrInvalidDay},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l, store := newLedger(t)

			if _, err := l.Credit(tc.posting); !errors.Is(err, tc.want) {
				t.Errorf("Credit error = %v, want %v", err, tc.want)
			}
			if _, err := l.Debit(tc.posting); !errors.Is(err, tc.want) {
				t.Errorf("Debit error = %v, want %v", err, tc.want)
			}

			if n := len(store.Entries(tc.posting.Account)); n != 0 {
				t.Errorf("%d entries appended for a rejected posting, want 0", n)
			}
			rejections := store.Rejections()
			if len(rejections) != 2 {
				t.Fatalf("%d rejections recorded, want 2 (one per attempt)", len(rejections))
			}
			if rejections[0].EventID != tc.posting.EventID || rejections[0].Reason == "" {
				t.Errorf("rejection = %+v, want event %s with a reason", rejections[0], tc.posting.EventID)
			}
		})
	}
}
