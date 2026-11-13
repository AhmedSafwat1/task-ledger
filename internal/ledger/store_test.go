package ledger

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"ledgercore/internal/money"
)

func aed(s string) money.Money { return money.MustParse(s, money.AED) }
func bhd(s string) money.Money { return money.MustParse(s, money.BHD) }

func TestMemStoreAssignsOneSequenceAcrossAllLogs(t *testing.T) {
	s := NewMemStore()

	e, err := s.AppendEntry(Entry{Account: "ACC-001", Kind: Credit, Amount: aed("1.00"), Booked: 1, Value: 1})
	if err != nil {
		t.Fatalf("AppendEntry: %v", err)
	}
	a, err := s.AppendAuthEvent(AuthEvent{Account: "ACC-001", AuthID: "Auth-A", Kind: AuthApproved, Hold: aed("1.00")})
	if err != nil {
		t.Fatalf("AppendAuthEvent: %v", err)
	}
	c, err := s.AppendAccrual(Accrual{Account: "ACC-001", Kind: AccrualDaily, ForDay: 1, Booked: 1})
	if err != nil {
		t.Fatalf("AppendAccrual: %v", err)
	}
	r, err := s.AppendRejection(Rejection{Account: "ACC-001", EventID: "E6", Booked: 4, Reason: "unknown authorization"})
	if err != nil {
		t.Fatalf("AppendRejection: %v", err)
	}

	got := []uint64{e.Seq, a.Seq, c.Seq, r.Seq}
	want := []uint64{1, 2, 3, 4}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sequence numbers = %v, want %v", got, want)
	}
	if s.LastSeq() != 4 {
		t.Errorf("LastSeq = %d, want 4", s.LastSeq())
	}
}

func TestMemStoreRefusesARecordThatAlreadyHasASequence(t *testing.T) {
	s := NewMemStore()
	stored, err := s.AppendEntry(Entry{Account: "ACC-001", Kind: Credit, Amount: aed("1.00"), Booked: 1, Value: 1})
	if err != nil {
		t.Fatalf("AppendEntry: %v", err)
	}

	// Appending a stored record again would be a way to smuggle in a rewrite.
	if _, err := s.AppendEntry(stored); !errors.Is(err, ErrAlreadyStored) {
		t.Errorf("re-appending a stored entry error = %v, want ErrAlreadyStored", err)
	}
	if _, err := s.AppendAuthEvent(AuthEvent{Seq: 9}); !errors.Is(err, ErrAlreadyStored) {
		t.Errorf("AppendAuthEvent with Seq error = %v, want ErrAlreadyStored", err)
	}
	if _, err := s.AppendAccrual(Accrual{Seq: 9}); !errors.Is(err, ErrAlreadyStored) {
		t.Errorf("AppendAccrual with Seq error = %v, want ErrAlreadyStored", err)
	}
	if _, err := s.AppendRejection(Rejection{Seq: 9}); !errors.Is(err, ErrAlreadyStored) {
		t.Errorf("AppendRejection with Seq error = %v, want ErrAlreadyStored", err)
	}
	if got := len(s.Entries("ACC-001")); got != 1 {
		t.Errorf("store holds %d entries after refused appends, want 1", got)
	}
}

func TestMemStoreReadsAreCopies(t *testing.T) {
	s := NewMemStore()
	if _, err := s.AppendEntry(Entry{Account: "ACC-001", Kind: Credit, Amount: aed("1,200.00"), Booked: 1, Value: 1}); err != nil {
		t.Fatalf("AppendEntry: %v", err)
	}

	read := s.Entries("ACC-001")
	read[0].Amount = aed("999,999.00")
	read[0].Kind = Debit

	again := s.Entries("ACC-001")
	if again[0].Amount != aed("1,200.00") || again[0].Kind != Credit {
		t.Errorf("stored entry changed through a returned slice: %+v", again[0])
	}
}

func TestMemStoreReadsFilterByAccountInAppendOrder(t *testing.T) {
	s := NewMemStore()
	for _, e := range []Entry{
		{Account: "ACC-001", Kind: Credit, Amount: aed("1.00"), EventID: "E1"},
		{Account: "ACC-002", Kind: Credit, Amount: bhd("1.000"), EventID: "E10"},
		{Account: "ACC-001", Kind: Debit, Amount: aed("-1.00"), EventID: "E2"},
	} {
		if _, err := s.AppendEntry(e); err != nil {
			t.Fatalf("AppendEntry: %v", err)
		}
	}

	var got []string
	for _, e := range s.Entries("ACC-001") {
		got = append(got, e.EventID)
	}
	if want := []string{"E1", "E2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ACC-001 entries = %v, want %v", got, want)
	}
	if n := len(s.Entries("ACC-002")); n != 1 {
		t.Errorf("ACC-002 has %d entries, want 1", n)
	}
	if n := len(s.Entries("ACC-404")); n != 0 {
		t.Errorf("unknown account has %d entries, want 0", n)
	}
}

// The append-only rule is carried by the shape of the interface: there is no
// method through which a stored record could be changed or removed.
func TestStoreInterfaceOnlyAppendsAndReads(t *testing.T) {
	reads := map[string]bool{
		"Entries": true, "AuthEvents": true, "Accruals": true, "Rejections": true, "LastSeq": true,
	}
	typ := reflect.TypeOf((*Store)(nil)).Elem()
	for i := range typ.NumMethod() {
		name := typ.Method(i).Name
		if !strings.HasPrefix(name, "Append") && !reads[name] {
			t.Errorf("Store has method %s, which is neither an append nor a read", name)
		}
	}
}

func TestMemStoreConcurrentAppendsGetUniqueSequences(t *testing.T) {
	const writers, perWriter = 8, 200
	s := NewMemStore()

	var wg sync.WaitGroup
	for range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range perWriter {
				if _, err := s.AppendEntry(Entry{Account: "ACC-001", Kind: Credit, Amount: aed("0.01")}); err != nil {
					t.Errorf("AppendEntry: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	entries := s.Entries("ACC-001")
	if len(entries) != writers*perWriter {
		t.Fatalf("store holds %d entries, want %d", len(entries), writers*perWriter)
	}
	for i, e := range entries {
		if e.Seq != uint64(i+1) {
			t.Fatalf("entry %d has Seq %d, want %d (sequence must be gapless and ordered)", i, e.Seq, i+1)
		}
	}
}
