package ledger

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"ledgercore/internal/money"
)

// ErrCloseOutOfOrder is returned when a day is closed twice or a day is skipped.
var ErrCloseOutOfOrder = errors.New("days must be closed in order, each exactly once")

// DayClose reports what the end-of-day run appended, one section per account
// in account-id order.
type DayClose struct {
	Day      Day
	Accounts []AccountClose
}

// AccountClose is one account's part of an end-of-day run.
type AccountClose struct {
	Account AccountID
	// PreFeeSeq is the store sequence number just before this run touched the
	// account. BalanceAtSeq with it gives balances before any fee of this run.
	PreFeeSeq uint64
	Fees      []Entry  // fee entries appended by this run, in the order assessed
	Notices   []string // conditions that need a human, such as a missing fee schedule
}

// CloseDay runs end of day for every account and then closes the day for
// booking. Days are closed in order, each exactly once.
//
// If the run fails part-way the day stays open and can be closed again:
// every step decides what to append from what is already in the store, so a
// second run does not repeat what the first one managed to append.
func (l *Ledger) CloseDay(day Day) (DayClose, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if day != l.closed+1 {
		return DayClose{}, fmt.Errorf("%w: asked to close day %d after day %d", ErrCloseOutOfOrder, day, l.closed)
	}
	result := DayClose{Day: day}
	for _, id := range slices.Sorted(maps.Keys(l.accounts)) {
		section := AccountClose{Account: id, PreFeeSeq: l.store.LastSeq()}
		var err error
		if section.Fees, section.Notices, err = l.reassessFees(id, day); err != nil {
			return DayClose{}, fmt.Errorf("close day %d: fees for %s: %w", day, id, err)
		}
		result.Accounts = append(result.Accounts, section)
	}
	l.closed = day
	return result, nil
}

// reassessFees brings the overdraft fees of every day up to today in line
// with the balances as they are known now. It looks at every day again, not
// just today, because a backdated entry can turn an earlier day negative
// after that day was closed.
//
// A fee is value-dated on the day it is for, so it lowers that day and every
// later day, never an earlier one. Walking the days in ascending order
// therefore decides each day on the final state of all days before it. The
// outer loop still repeats until a whole pass appends nothing, so stability
// is checked rather than assumed.
func (l *Ledger) reassessFees(id AccountID, today Day) ([]Entry, []string, error) {
	cur := l.accounts[id]
	fee, defined := l.cfg.OverdraftFee[cur.Code]
	if defined && fee.Currency() != cur {
		return nil, nil, fmt.Errorf("%w: overdraft fee for %s is configured in %s", money.ErrCurrencyMismatch, cur.Code, fee.Currency().Code)
	}

	var appended []Entry
	var notices []string
	for pass := 0; ; pass++ {
		// Each pass that appends anything settles at least one more day, so
		// more passes than days means the rule is not converging.
		if pass > int(today) {
			return nil, nil, fmt.Errorf("fee assessment for %s did not stabilise after %d passes", id, pass)
		}
		changed := false
		notices = notices[:0]
		for d := Day(1); d <= today; d++ {
			entries := l.store.Entries(id)
			base, err := feeBase(cur, entries, d)
			if err != nil {
				return nil, nil, err
			}
			_, charged := activeFee(entries, d)
			if !base.IsNegative() || charged {
				continue
			}
			if !defined {
				notices = append(notices, fmt.Sprintf("%s closed day %d at %s: no overdraft fee is defined for %s, none charged", id, d, base, cur.Code))
				continue
			}
			amount, err := fee.Neg()
			if err != nil {
				return nil, nil, err
			}
			e, err := l.store.AppendEntry(Entry{
				Account: id,
				Kind:    Fee,
				Amount:  amount,
				Booked:  today,
				Value:   d,
				FeeDay:  d,
			})
			if err != nil {
				return nil, nil, fmt.Errorf("append fee for day %d: %w", d, err)
			}
			appended = append(appended, e)
			changed = true
		}
		if !changed {
			return appended, notices, nil
		}
	}
}

// feeBase is the closing balance of day d that decides whether d owes a fee:
// every entry value-dated on or before d, except d's own fee entries. Leaving
// the day's own fee out is what keeps a fee from justifying itself.
func feeBase(cur money.Currency, entries []Entry, d Day) (money.Money, error) {
	return sumEntries(cur, entries, func(e Entry) bool {
		if e.Value > d {
			return false
		}
		ownFee := (e.Kind == Fee || e.Kind == FeeWaiver) && e.FeeDay == d
		return !ownFee
	})
}

// activeFee returns the fee currently standing for day d: the last fee entry
// for d that has not been waived.
func activeFee(entries []Entry, d Day) (Entry, bool) {
	var fee Entry
	active := false
	for _, e := range entries {
		if e.FeeDay != d {
			continue
		}
		switch e.Kind {
		case Fee:
			fee, active = e, true
		case FeeWaiver:
			active = false
		}
	}
	return fee, active
}
