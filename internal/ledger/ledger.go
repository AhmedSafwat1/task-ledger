// Package ledger is an in-memory, append-only account ledger. Postings are
// value-dated and booked-dated; balances are never stored, only derived by
// summing entries, which is what makes backdated entries and "as known on
// day Y" queries work without rewriting anything.
package ledger

import (
	"errors"
	"fmt"
	"sync"

	"ledgercore/internal/money"
)

// Errors callers can branch on with errors.Is.
var (
	ErrUnknownAccount   = errors.New("unknown account")
	ErrDuplicateAccount = errors.New("account already open")
	ErrInvalidAmount    = errors.New("amount must be positive")
	ErrInvalidDay       = errors.New("day must be 1 or later")
)

// Ledger applies the posting rules on top of a Store. It keeps no balance of
// its own: every figure it reports is recomputed from the store's records.
//
// A Ledger is safe for concurrent use. The mutex makes each operation atomic
// within this one Ledger value only; it does nothing for a second Ledger, or
// a second process, writing to the same store.
type Ledger struct {
	mu       sync.Mutex
	store    Store
	accounts map[AccountID]money.Currency
}

// New returns a ledger over store with no accounts.
func New(store Store) *Ledger {
	return &Ledger{store: store, accounts: make(map[AccountID]money.Currency)}
}

// OpenAccount registers an account with a zero opening balance in cur.
func (l *Ledger) OpenAccount(id AccountID, cur money.Currency) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.accounts[id]; ok {
		return fmt.Errorf("open %s: %w", id, ErrDuplicateAccount)
	}
	l.accounts[id] = cur
	return nil
}

// Posting is a request to credit or debit an account. Amount is always the
// positive magnitude; Credit and Debit decide the sign.
type Posting struct {
	EventID string
	Account AccountID
	Amount  money.Money
	Booked  Day
	Value   Day
}

// Credit raises the ledger balance from the posting's value day.
func (l *Ledger) Credit(p Posting) (Entry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.post(Credit, p)
}

// Debit lowers the ledger balance from the posting's value day. A debit is
// not checked against the balance: an account may be overdrawn.
func (l *Ledger) Debit(p Posting) (Entry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.post(Debit, p)
}

func (l *Ledger) post(kind EntryKind, p Posting) (Entry, error) {
	if err := l.validate(p.Account, p.Amount, p.Booked, p.Value); err != nil {
		return Entry{}, l.reject(p.Account, p.EventID, p.Booked, err)
	}
	amount := p.Amount
	if kind == Debit {
		neg, err := amount.Neg()
		if err != nil {
			return Entry{}, l.reject(p.Account, p.EventID, p.Booked, err)
		}
		amount = neg
	}
	e, err := l.store.AppendEntry(Entry{
		Account: p.Account,
		Kind:    kind,
		Amount:  amount,
		Booked:  p.Booked,
		Value:   p.Value,
		EventID: p.EventID,
	})
	if err != nil {
		return Entry{}, fmt.Errorf("append %s %s: %w", kind, p.EventID, err)
	}
	return e, nil
}

// validate checks the parts every event shares: a known account, a positive
// amount in the account's currency, and usable days.
func (l *Ledger) validate(id AccountID, amount money.Money, booked, value Day) error {
	cur, ok := l.accounts[id]
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownAccount, id)
	}
	if booked < 1 || value < 1 {
		return fmt.Errorf("%w: booked day %d, value day %d", ErrInvalidDay, booked, value)
	}
	if amount.Currency() != cur {
		return fmt.Errorf("%w: %s amount on %s account %s", money.ErrCurrencyMismatch, amount.Currency().Code, cur.Code, id)
	}
	if amount.Minor() <= 0 {
		return fmt.Errorf("%w: got %s", ErrInvalidAmount, amount)
	}
	return nil
}

// reject records that an event was refused and returns the cause. Nothing
// else is appended for a refused event, so it moves no funds.
func (l *Ledger) reject(id AccountID, eventID string, booked Day, cause error) error {
	_, err := l.store.AppendRejection(Rejection{
		Account: id,
		EventID: eventID,
		Booked:  booked,
		Reason:  cause.Error(),
	})
	if err != nil {
		return errors.Join(cause, fmt.Errorf("record rejection of %s: %w", eventID, err))
	}
	return cause
}

// Balance returns the ledger balance of value day `value` as it was known at
// the end of booked day `booked`: the sum of every entry value-dated on or
// before `value` that the ledger had learned of by `booked`.
func (l *Ledger) Balance(id AccountID, value, booked Day) (money.Money, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.sum(id, func(e Entry) bool { return e.Value <= value && e.Booked <= booked })
}

// BalanceAtSeq is Balance with a finer notion of "as known": only entries
// appended up to and including sequence number seq are counted. It answers
// questions inside a single day, such as the balance before fees were assessed.
func (l *Ledger) BalanceAtSeq(id AccountID, value Day, seq uint64) (money.Money, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.sum(id, func(e Entry) bool { return e.Value <= value && e.Seq <= seq })
}

// sum adds up the account's entries that keep selects.
func (l *Ledger) sum(id AccountID, keep func(Entry) bool) (money.Money, error) {
	cur, ok := l.accounts[id]
	if !ok {
		return money.Money{}, fmt.Errorf("%w: %s", ErrUnknownAccount, id)
	}
	return sumEntries(cur, l.store.Entries(id), keep)
}

func sumEntries(cur money.Currency, entries []Entry, keep func(Entry) bool) (money.Money, error) {
	total := money.Zero(cur)
	for _, e := range entries {
		if !keep(e) {
			continue
		}
		var err error
		if total, err = total.Add(e.Amount); err != nil {
			return money.Money{}, fmt.Errorf("sum entry %d: %w", e.Seq, err)
		}
	}
	return total, nil
}
