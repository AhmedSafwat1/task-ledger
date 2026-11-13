package ledger

import (
	"fmt"
	"math"

	"ledgercore/internal/money"
)

// accrue brings the account's accrual log up to date at the end of today.
//
// For every day up to today it compares the closing balance as known now
// with the basis on the latest accrual line for that day. Today has no line
// yet and gets its DAILY line. An earlier day whose balance has changed since
// (a backdated entry, a retroactive fee, a waiver) gets an ADJUSTMENT line
// for the difference. Lines are only ever appended.
//
// Rounding: interest is computed exactly, in units of 1/InterestDenominator
// of a minor unit, then rounded half-to-even to whole minor units. What the
// rounding leaves over is not dropped: it is the CarryOut of the line and is
// added to the next line before that one is rounded. So at any moment
//
//	sum(recorded amounts) x denominator + carry = sum(exact interest)
//
// and the recorded amounts are never more than half a minor unit away from
// the exact total, however many lines there are.
func (l *Ledger) accrue(id AccountID, today Day) ([]Accrual, error) {
	if l.cfg.InterestNumerator == 0 {
		return nil, nil // no interest configured
	}
	cur := l.accounts[id]

	var carry int64
	recorded := make(map[Day]money.Money) // latest basis written for each day
	for _, a := range l.store.Accruals(id) {
		recorded[a.ForDay] = a.Basis
		carry = a.CarryOut
	}
	entries := l.store.Entries(id)

	var out []Accrual
	for d := Day(1); d <= today; d++ {
		basis, err := interestBasis(cur, entries, d)
		if err != nil {
			return out, err
		}
		old, seen := recorded[d]
		if seen && old == basis {
			continue
		}
		kind := AccrualAdjustment
		if d == today && !seen {
			kind = AccrualDaily
		}

		now, err := l.exactInterest(basis)
		if err != nil {
			return out, err
		}
		before, err := l.exactInterest(old) // zero when the day has no line yet
		if err != nil {
			return out, err
		}
		raw := now - before // both are in [0, MaxInt64], so this cannot overflow
		if (carry > 0 && raw > math.MaxInt64-carry) || (carry < 0 && raw < math.MinInt64-carry) {
			return out, fmt.Errorf("interest for day %d: %w", d, money.ErrOverflow)
		}
		raw += carry
		rounded, err := money.DivRoundHalfEven(raw, l.cfg.InterestDenominator)
		if err != nil {
			return out, fmt.Errorf("interest for day %d: %w", d, err)
		}
		carryOut := raw - rounded*l.cfg.InterestDenominator

		a, err := l.store.AppendAccrual(Accrual{
			Account:  id,
			Kind:     kind,
			ForDay:   d,
			Booked:   today,
			Basis:    basis,
			Amount:   money.New(rounded, cur),
			CarryIn:  carry,
			CarryOut: carryOut,
		})
		if err != nil {
			return out, fmt.Errorf("append accrual for day %d: %w", d, err)
		}
		carry = carryOut
		out = append(out, a)
	}
	return out, nil
}

// exactInterest returns one day's interest on balance, unrounded, in units of
// 1/InterestDenominator of a minor unit. Only positive balances earn.
func (l *Ledger) exactInterest(balance money.Money) (int64, error) {
	minor := balance.Minor()
	if minor <= 0 {
		return 0, nil
	}
	if minor > math.MaxInt64/l.cfg.InterestNumerator {
		return 0, fmt.Errorf("interest on %s: %w", balance, money.ErrOverflow)
	}
	return minor * l.cfg.InterestNumerator, nil
}

// capitalize credits the account with everything the accrual log holds that
// has not been capitalized yet. The amount is the plain sum of the recorded
// lines, so the lines add up to the credit exactly, by construction. The
// rounding residual is not part of the credit; it stays in the carry.
func (l *Ledger) capitalize(id AccountID, today Day) (*Entry, error) {
	total := money.Zero(l.accounts[id])
	var err error
	for _, a := range l.store.Accruals(id) {
		if total, err = total.Add(a.Amount); err != nil {
			return nil, fmt.Errorf("sum accrual %d: %w", a.Seq, err)
		}
	}
	for _, e := range l.store.Entries(id) {
		if e.Kind != Interest {
			continue
		}
		if total, err = total.Sub(e.Amount); err != nil {
			return nil, fmt.Errorf("subtract capitalization %d: %w", e.Seq, err)
		}
	}
	if total.IsZero() {
		return nil, nil
	}
	e, err := l.store.AppendEntry(Entry{
		Account: id,
		Kind:    Interest,
		Amount:  total,
		Booked:  today,
		Value:   today,
	})
	if err != nil {
		return nil, fmt.Errorf("append capitalization: %w", err)
	}
	return &e, nil
}

// interestBasis is the closing balance of day d that earns interest: every
// entry value-dated on or before d except an interest capitalization dated d
// itself. That credit is the outcome of d's interest run, so it cannot also
// be an input to it; it earns from the next day.
func interestBasis(cur money.Currency, entries []Entry, d Day) (money.Money, error) {
	return sumEntries(cur, entries, func(e Entry) bool {
		return e.Value <= d && !(e.Kind == Interest && e.Value == d)
	})
}
