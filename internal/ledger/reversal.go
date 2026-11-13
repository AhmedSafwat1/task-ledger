package ledger

import (
	"errors"
	"fmt"
)

// Reversal errors callers can branch on with errors.Is.
var (
	ErrUnknownEntry    = errors.New("no entry for that event")
	ErrNotReversible   = errors.New("only credits and debits can be reversed")
	ErrAlreadyReversed = errors.New("entry is already reversed")
)

// ReverseRequest asks to reverse the entries posted by the event Target.
type ReverseRequest struct {
	EventID string // the reversal event itself
	Account AccountID
	Target  string // event id of the posting to reverse
	Booked  Day
	Value   Day // the day from which the reversal counts; normally the target's value day
}

// Reverse neutralises a credit or debit by appending an equal and opposite
// entry. The original entry is not touched and stays in every balance; the
// two cancel from the reversal's value day onward.
//
// That is all a reversal does. It does not undo anything that was decided
// while the original stood: a declined authorization stays declined, and
// fees are reconsidered by the next end-of-day run, not here.
func (l *Ledger) Reverse(r ReverseRequest) ([]Entry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if _, err := l.validateDays(r.Account, r.Booked, r.Value); err != nil {
		return nil, l.reject(r.Account, r.EventID, r.Booked, err)
	}
	entries := l.store.Entries(r.Account)
	reversed := make(map[uint64]bool)
	for _, e := range entries {
		if e.Kind == Reversal {
			reversed[e.Reverses] = true
		}
	}

	var targets []Entry
	for _, e := range entries {
		if e.EventID != r.Target {
			continue
		}
		var err error
		switch {
		case e.Kind != Credit && e.Kind != Debit:
			err = fmt.Errorf("%w: %s is a %s", ErrNotReversible, r.Target, e.Kind)
		case reversed[e.Seq]:
			err = fmt.Errorf("%w: %s", ErrAlreadyReversed, r.Target)
		}
		if err != nil {
			return nil, l.reject(r.Account, r.EventID, r.Booked, err)
		}
		targets = append(targets, e)
	}
	if len(targets) == 0 {
		return nil, l.reject(r.Account, r.EventID, r.Booked,
			fmt.Errorf("%w: %s on %s", ErrUnknownEntry, r.Target, r.Account))
	}

	// An event posted as instalments has several entries; each gets its own
	// reversal so every reversal points at exactly one original.
	var out []Entry
	for _, target := range targets {
		amount, err := target.Amount.Neg()
		if err != nil {
			return out, fmt.Errorf("reverse %s: %w", r.Target, err)
		}
		e, err := l.store.AppendEntry(Entry{
			Account:  r.Account,
			Kind:     Reversal,
			Amount:   amount,
			Booked:   r.Booked,
			Value:    r.Value,
			EventID:  r.EventID,
			Reverses: target.Seq,
		})
		if err != nil {
			return out, fmt.Errorf("append reversal of %s: %w", r.Target, err)
		}
		out = append(out, e)
	}
	return out, nil
}
