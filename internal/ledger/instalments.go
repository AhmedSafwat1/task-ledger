package ledger

import "fmt"

// CreditInstalments posts one credit as `parts` entries that sum exactly to
// the posting's amount. Each instalment is the amount divided by parts,
// truncated to the currency's precision; the whole remainder goes on the last
// one. BHD 10.000 in three is 3.333, 3.333, 3.334, never 3 x 3.334 = 10.002.
//
// Everything is checked before the first append, so a refused request leaves
// no partial set of instalments behind.
func (l *Ledger) CreditInstalments(p Posting, parts int) ([]Entry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := l.validate(p.Account, p.Amount, p.Booked, p.Value); err != nil {
		return nil, l.reject(p.Account, p.EventID, p.Booked, err)
	}
	amounts, err := p.Amount.Split(parts)
	if err != nil {
		return nil, l.reject(p.Account, p.EventID, p.Booked, err)
	}
	for _, a := range amounts {
		if a.Minor() <= 0 {
			return nil, l.reject(p.Account, p.EventID, p.Booked,
				fmt.Errorf("%w: %s in %d instalments leaves an instalment of %s", ErrInvalidAmount, p.Amount, parts, a))
		}
	}

	out := make([]Entry, 0, parts)
	for i, a := range amounts {
		e, err := l.store.AppendEntry(Entry{
			Account: p.Account,
			Kind:    Credit,
			Amount:  a,
			Booked:  p.Booked,
			Value:   p.Value,
			EventID: p.EventID,
			Part:    i + 1,
			Parts:   parts,
		})
		if err != nil {
			return out, fmt.Errorf("append instalment %d of %d for %s: %w", i+1, parts, p.EventID, err)
		}
		out = append(out, e)
	}
	return out, nil
}
