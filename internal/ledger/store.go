package ledger

import (
	"errors"
	"sync"
)

// ErrAlreadyStored is returned when a record that already carries a sequence
// number is appended again.
var ErrAlreadyStored = errors.New("record already has a sequence number")

// Store is everything the ledger needs from storage: append a record, read
// records back. There is deliberately no method that changes or removes a
// stored record, so the append-only rule cannot be broken through it.
//
// Every append, whichever log it goes to, takes the next number from one
// store-wide sequence. That gives all records a single total order.
type Store interface {
	AppendEntry(Entry) (Entry, error)
	AppendAuthEvent(AuthEvent) (AuthEvent, error)
	AppendAccrual(Accrual) (Accrual, error)
	AppendRejection(Rejection) (Rejection, error)

	// Reads return copies in append order. Changing a returned slice does not
	// change the store.
	Entries(AccountID) []Entry
	AuthEvents(AccountID) []AuthEvent
	Accruals(AccountID) []Accrual
	Rejections() []Rejection
	LastSeq() uint64
}

// MemStore is the in-memory Store. It is safe for concurrent use, but each
// call is atomic only on its own: nothing here makes a read followed by an
// append atomic.
type MemStore struct {
	mu         sync.Mutex
	seq        uint64
	entries    []Entry
	authEvents []AuthEvent
	accruals   []Accrual
	rejections []Rejection
}

var _ Store = (*MemStore)(nil)

// NewMemStore returns an empty in-memory store.
func NewMemStore() *MemStore {
	return &MemStore{}
}

// AppendEntry stores e and returns it with its sequence number.
func (s *MemStore) AppendEntry(e Entry) (Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.Seq != 0 {
		return Entry{}, ErrAlreadyStored
	}
	s.seq++
	e.Seq = s.seq
	s.entries = append(s.entries, e)
	return e, nil
}

// AppendAuthEvent stores a and returns it with its sequence number.
func (s *MemStore) AppendAuthEvent(a AuthEvent) (AuthEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a.Seq != 0 {
		return AuthEvent{}, ErrAlreadyStored
	}
	s.seq++
	a.Seq = s.seq
	s.authEvents = append(s.authEvents, a)
	return a, nil
}

// AppendAccrual stores a and returns it with its sequence number.
func (s *MemStore) AppendAccrual(a Accrual) (Accrual, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a.Seq != 0 {
		return Accrual{}, ErrAlreadyStored
	}
	s.seq++
	a.Seq = s.seq
	s.accruals = append(s.accruals, a)
	return a, nil
}

// AppendRejection stores r and returns it with its sequence number.
func (s *MemStore) AppendRejection(r Rejection) (Rejection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Seq != 0 {
		return Rejection{}, ErrAlreadyStored
	}
	s.seq++
	r.Seq = s.seq
	s.rejections = append(s.rejections, r)
	return r, nil
}

// Entries returns a copy of the account's entries in append order.
func (s *MemStore) Entries(id AccountID) []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Entry
	for _, e := range s.entries {
		if e.Account == id {
			out = append(out, e)
		}
	}
	return out
}

// AuthEvents returns a copy of the account's authorization events in append order.
func (s *MemStore) AuthEvents(id AccountID) []AuthEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []AuthEvent
	for _, a := range s.authEvents {
		if a.Account == id {
			out = append(out, a)
		}
	}
	return out
}

// Accruals returns a copy of the account's accrual log in append order.
func (s *MemStore) Accruals(id AccountID) []Accrual {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Accrual
	for _, a := range s.accruals {
		if a.Account == id {
			out = append(out, a)
		}
	}
	return out
}

// Rejections returns a copy of every rejection in append order.
func (s *MemStore) Rejections() []Rejection {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Rejection(nil), s.rejections...)
}

// LastSeq returns the sequence number of the most recent append, or zero.
func (s *MemStore) LastSeq() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seq
}
