package ledger

import (
	"errors"
	"fmt"

	"ledgercore/internal/money"
)

// Authorization errors callers can branch on with errors.Is.
var (
	ErrUnknownAuthorization   = errors.New("unknown authorization")
	ErrDuplicateAuthorization = errors.New("authorization id already used")
	ErrAuthorizationNotActive = errors.New("authorization has no active hold")
	ErrSettlementExceedsHold  = errors.New("settlement amount exceeds the hold")
)

// AuthStatus is the state of an authorization, derived from the records.
type AuthStatus string

// Authorization states.
const (
	StatusApproved AuthStatus = "APPROVED" // hold is active
	StatusDeclined AuthStatus = "DECLINED" // no hold was ever placed
	StatusSettled  AuthStatus = "SETTLED"  // hold released by a settlement entry
)

// Authorization is the current state of one authorization. It is a view:
// nothing of this shape is stored, it is folded from the auth events and the
// settlement entries each time it is asked for.
type Authorization struct {
	AuthID      string
	Account     AccountID
	Status      AuthStatus
	Hold        money.Money // amount requested
	Settled     money.Money // amount that left the account; zero unless SETTLED
	RequestedOn Day
	SettledOn   Day    // zero unless SETTLED
	Reason      string // why it was declined
}

// AuthRequest asks to place a hold of Amount on the account.
type AuthRequest struct {
	EventID string
	Account AccountID
	AuthID  string
	Amount  money.Money
	Booked  Day
	Value   Day
}

// SettleRequest asks to settle an authorization for Amount.
type SettleRequest struct {
	EventID string
	Account AccountID
	AuthID  string
	Amount  money.Money
	Booked  Day
	Value   Day
}

// Authorize decides an authorization request. It is approved only if the
// available balance (ledger balance on the request's value day, minus active
// holds) is still zero or more once the new hold is applied. A decline is a
// recorded outcome, not an error; the error return is for requests that could
// not be considered at all.
func (l *Ledger) Authorize(r AuthRequest) (Authorization, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := l.validate(r.Account, r.Amount, r.Booked, r.Value); err != nil {
		return Authorization{}, l.reject(r.Account, r.EventID, r.Booked, err)
	}
	auths, err := l.authorizations(r.Account)
	if err != nil {
		return Authorization{}, err
	}
	if _, found := findAuth(auths, r.AuthID); found {
		return Authorization{}, l.reject(r.Account, r.EventID, r.Booked,
			fmt.Errorf("%w: %s on %s", ErrDuplicateAuthorization, r.AuthID, r.Account))
	}

	available, err := l.available(r.Account, r.Value, auths)
	if err != nil {
		return Authorization{}, err
	}
	after, err := available.Sub(r.Amount)
	if err != nil {
		return Authorization{}, fmt.Errorf("authorize %s: %w", r.AuthID, err)
	}

	event := AuthEvent{
		Account: r.Account,
		AuthID:  r.AuthID,
		Kind:    AuthApproved,
		Hold:    r.Amount,
		Booked:  r.Booked,
		Value:   r.Value,
		EventID: r.EventID,
	}
	if after.IsNegative() {
		event.Kind = AuthDeclined
		event.Reason = fmt.Sprintf("available balance would be %s (available %s, requested %s)", after, available, r.Amount)
	}
	if _, err := l.store.AppendAuthEvent(event); err != nil {
		return Authorization{}, fmt.Errorf("append authorization %s: %w", r.AuthID, err)
	}

	auths, err = l.authorizations(r.Account)
	if err != nil {
		return Authorization{}, err
	}
	decided, _ := findAuth(auths, r.AuthID)
	return decided, nil
}

// Settle debits the account for an approved authorization and releases its
// whole hold. The amount may be lower than the hold, never higher. A
// settlement that names no approved authorization is refused: no entry is
// appended, so no funds leave the account.
func (l *Ledger) Settle(r SettleRequest) (Entry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := l.validate(r.Account, r.Amount, r.Booked, r.Value); err != nil {
		return Entry{}, l.reject(r.Account, r.EventID, r.Booked, err)
	}
	auths, err := l.authorizations(r.Account)
	if err != nil {
		return Entry{}, err
	}
	auth, found := findAuth(auths, r.AuthID)
	switch {
	case !found:
		err = fmt.Errorf("%w: %s on %s", ErrUnknownAuthorization, r.AuthID, r.Account)
	case auth.Status != StatusApproved:
		err = fmt.Errorf("%w: %s is %s", ErrAuthorizationNotActive, r.AuthID, auth.Status)
	case r.Amount.Minor() > auth.Hold.Minor():
		err = fmt.Errorf("%w: %s against a hold of %s on %s", ErrSettlementExceedsHold, r.Amount, auth.Hold, r.AuthID)
	}
	if err != nil {
		return Entry{}, l.reject(r.Account, r.EventID, r.Booked, err)
	}

	amount, err := r.Amount.Neg()
	if err != nil {
		return Entry{}, l.reject(r.Account, r.EventID, r.Booked, err)
	}
	// One append is both the debit and the release of the hold: the hold counts
	// as released exactly when a Settlement entry carries its AuthID.
	e, err := l.store.AppendEntry(Entry{
		Account: r.Account,
		Kind:    Settlement,
		Amount:  amount,
		Booked:  r.Booked,
		Value:   r.Value,
		EventID: r.EventID,
		AuthID:  r.AuthID,
	})
	if err != nil {
		return Entry{}, fmt.Errorf("append settlement %s: %w", r.EventID, err)
	}
	return e, nil
}

// Authorizations returns every authorization on the account, in the order
// they were requested, with its current state.
func (l *Ledger) Authorizations(id AccountID) ([]Authorization, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.authorizations(id)
}

// Available returns the ledger balance of the given value day, as known now,
// minus the active holds.
func (l *Ledger) Available(id AccountID, value Day) (money.Money, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	auths, err := l.authorizations(id)
	if err != nil {
		return money.Money{}, err
	}
	return l.available(id, value, auths)
}

func (l *Ledger) available(id AccountID, value Day, auths []Authorization) (money.Money, error) {
	balance, err := l.sum(id, func(e Entry) bool { return e.Value <= value })
	if err != nil {
		return money.Money{}, err
	}
	for _, a := range auths {
		if a.Status != StatusApproved {
			continue
		}
		if balance, err = balance.Sub(a.Hold); err != nil {
			return money.Money{}, fmt.Errorf("subtract hold %s: %w", a.AuthID, err)
		}
	}
	return balance, nil
}

// authorizations folds the account's auth events and settlement entries into
// the current state of each authorization.
func (l *Ledger) authorizations(id AccountID) ([]Authorization, error) {
	cur, ok := l.accounts[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownAccount, id)
	}
	settlements := make(map[string]Entry)
	for _, e := range l.store.Entries(id) {
		if e.Kind == Settlement {
			settlements[e.AuthID] = e
		}
	}

	var out []Authorization
	for _, ev := range l.store.AuthEvents(id) {
		a := Authorization{
			AuthID:      ev.AuthID,
			Account:     ev.Account,
			Hold:        ev.Hold,
			Settled:     money.Zero(cur),
			RequestedOn: ev.Booked,
			Reason:      ev.Reason,
		}
		switch ev.Kind {
		case AuthDeclined:
			a.Status = StatusDeclined
		case AuthApproved:
			a.Status = StatusApproved
			if s, settled := settlements[ev.AuthID]; settled {
				paid, err := s.Amount.Neg()
				if err != nil {
					return nil, fmt.Errorf("settlement of %s: %w", ev.AuthID, err)
				}
				a.Status = StatusSettled
				a.Settled = paid
				a.SettledOn = s.Booked
			}
		default:
			return nil, fmt.Errorf("authorization %s: unknown event kind %q", ev.AuthID, ev.Kind)
		}
		out = append(out, a)
	}
	return out, nil
}

func findAuth(auths []Authorization, authID string) (Authorization, bool) {
	for _, a := range auths {
		if a.AuthID == authID {
			return a, true
		}
	}
	return Authorization{}, false
}
