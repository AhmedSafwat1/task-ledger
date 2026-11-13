package ledger

import (
	"errors"
	"reflect"
	"testing"
)

func reverse(t *testing.T, l *Ledger, eventID, target string, booked, value Day) []Entry {
	t.Helper()
	entries, err := l.Reverse(ReverseRequest{EventID: eventID, Account: acc1, Target: target, Booked: booked, Value: value})
	if err != nil {
		t.Fatalf("reverse %s: %v", target, err)
	}
	return entries
}

// waiverDays lists the days that carry a waiver entry, in append order.
func waiverDays(entries []Entry) []Day {
	var days []Day
	for _, e := range entries {
		if e.Kind == FeeWaiver {
			days = append(days, e.FeeDay)
		}
	}
	return days
}

// replayThroughDay5 continues replayThroughDay4 with E7 and E8 and closes Day 5.
func replayThroughDay5(t *testing.T, l *Ledger) {
	t.Helper()
	replayThroughDay4(t, l)
	mustDebit(t, l, "E7", aed("620.00"), 5, 2)
	authorize(t, l, "E8", "Auth-B", aed("90.00"), 5)
	closeDays(t, l, 5, 5)
}

func TestReversalAppendsAnEqualAndOppositeEntry(t *testing.T) {
	l, store := newLedger(t)
	mustCredit(t, l, "E1", aed("100.00"), 1, 1)
	original := mustDebit(t, l, "E7", aed("620.00"), 5, 2)

	reversals := reverse(t, l, "E9", "E7", 6, 2)

	if len(reversals) != 1 {
		t.Fatalf("%d reversal entries, want 1", len(reversals))
	}
	r := reversals[0]
	if r.Kind != Reversal || r.Amount != aed("620.00") || r.Reverses != original.Seq || r.Value != 2 || r.Booked != 6 || r.EventID != "E9" {
		t.Errorf("reversal = %+v, want REVERSAL of AED 620.00 pointing at seq %d, value day 2, booked day 6", r, original.Seq)
	}
	// The amount is neutralised from its value date...
	wantBalance(t, l, acc1, 2, 6, aed("100.00"))
	// ...but the original is still there, and so is what was known before.
	wantBalance(t, l, acc1, 2, 5, aed("-520.00"))
	entries := store.Entries(acc1)
	if len(entries) != 3 || entries[1] != original {
		t.Errorf("entries after reversal = %+v, want the original E7 untouched plus one new entry", entries)
	}
}

func TestReversalOfACreditIsADebit(t *testing.T) {
	l, _ := newLedger(t)
	mustCredit(t, l, "E1", aed("100.00"), 1, 1)

	reversals := reverse(t, l, "R1", "E1", 2, 1)

	if reversals[0].Amount != aed("-100.00") {
		t.Errorf("reversal amount = %s, want AED -100.00", reversals[0].Amount)
	}
	wantBalance(t, l, acc1, 1, 2, aed("0.00"))
}

func TestRejectedReversalAppendsNothing(t *testing.T) {
	tests := []struct {
		name   string
		target string
		want   error
	}{
		{"target does not exist", "E404", ErrUnknownEntry},
		{"target already reversed", "E2", ErrAlreadyReversed},
		{"target is itself a reversal", "R2", ErrNotReversible},
		{"target is a settlement", "E5", ErrNotReversible},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l, store := newLedger(t)
			mustCredit(t, l, "E1", aed("1,000.00"), 1, 1)
			mustDebit(t, l, "E2", aed("10.00"), 1, 1)
			reverse(t, l, "R2", "E2", 1, 1)
			authorize(t, l, "E3", "Auth-A", aed("200.00"), 1)
			if _, err := l.Settle(SettleRequest{EventID: "E5", Account: acc1, AuthID: "Auth-A", Amount: aed("185.00"), Booked: 1, Value: 1}); err != nil {
				t.Fatalf("settle: %v", err)
			}
			before := len(store.Entries(acc1))

			_, err := l.Reverse(ReverseRequest{EventID: "E9", Account: acc1, Target: tc.target, Booked: 2, Value: 1})

			if !errors.Is(err, tc.want) {
				t.Errorf("Reverse error = %v, want %v", err, tc.want)
			}
			if n := len(store.Entries(acc1)); n != before {
				t.Errorf("entries went from %d to %d on a rejected reversal", before, n)
			}
			if r := store.Rejections(); len(r) != 1 || r[0].EventID != "E9" {
				t.Errorf("rejections = %+v, want one for E9", r)
			}
		})
	}
}

func TestReversalWaivesFeesWhoseTriggerNoLongerHolds(t *testing.T) {
	l, store := newLedger(t)
	replayThroughDay5(t, l)
	entriesBefore := store.Entries(acc1)
	if got := feeDays(entriesBefore); !reflect.DeepEqual(got, []Day{2, 4, 5}) {
		t.Fatalf("fees before E9 = %v, want [2 4 5]", got)
	}

	reverse(t, l, "E9", "E7", 6, 2)
	closed := closeDays(t, l, 6, 6)

	waivers := accountClose(t, closed, acc1).Fees
	if got := waiverDays(waivers); !reflect.DeepEqual(got, []Day{2, 4, 5}) {
		t.Fatalf("waivers on Day 6 close = %v, want [2 4 5]", got)
	}
	for _, w := range waivers {
		if w.Amount != aed("25.00") || w.Value != w.FeeDay || w.Booked != 6 || w.Reverses == 0 {
			t.Errorf("waiver = %+v, want AED 25.00 value-dated on its fee day, booked day 6, pointing at the fee", w)
		}
	}

	// Balances as known on Day 6 are back to the figures of Days 1 to 4.
	for _, tc := range []struct {
		day  Day
		want string
	}{{1, "250.00"}, {2, "250.00"}, {3, "650.00"}, {4, "465.00"}, {5, "465.00"}, {6, "465.00"}} {
		wantBalance(t, l, acc1, tc.day, 6, aed(tc.want))
	}
	// What was known on Day 5 is still on record.
	wantBalance(t, l, acc1, 2, 5, aed("-395.00"))
	wantBalance(t, l, acc1, 5, 5, aed("-230.00"))

	// Nothing was removed or edited: the old entries are a prefix of the new.
	entriesAfter := store.Entries(acc1)
	if !reflect.DeepEqual(entriesAfter[:len(entriesBefore)], entriesBefore) {
		t.Error("entries that existed before E9 were changed")
	}
	if got := feeDays(entriesAfter); !reflect.DeepEqual(got, []Day{2, 4, 5}) {
		t.Errorf("fee entries after waiver = %v, want the three fees still on record", got)
	}
	for _, d := range []Day{2, 4, 5} {
		if _, standing := activeFee(entriesAfter, d); standing {
			t.Errorf("fee for day %d still stands after its waiver", d)
		}
	}
}

func TestReversalDoesNotReviveADeclinedAuthorization(t *testing.T) {
	l, _ := newLedger(t)
	replayThroughDay5(t, l)
	wantStatus(t, l, "Auth-B", StatusDeclined)

	reverse(t, l, "E9", "E7", 6, 2)
	closeDays(t, l, 6, 6)

	// The money is back; the declined card payment is not un-declined.
	wantStatus(t, l, "Auth-B", StatusDeclined)
	wantAvailable(t, l, 6, aed("465.00"))
}

func TestReversalValueDatedLaterLeavesEarlierFeesStanding(t *testing.T) {
	l, store := newLedger(t)
	replayThroughDay5(t, l)

	reverse(t, l, "E9", "E7", 6, 6) // value-dated Day 6, not Day 2
	closed := closeDays(t, l, 6, 6)

	if got := accountClose(t, closed, acc1).Fees; len(got) != 0 {
		t.Errorf("Day 6 close appended %+v, want nothing: Days 2, 4 and 5 are still negative", got)
	}
	for _, d := range []Day{2, 4, 5} {
		if _, standing := activeFee(store.Entries(acc1), d); !standing {
			t.Errorf("fee for day %d was waived although day %d is still negative", d, d)
		}
	}
	wantBalance(t, l, acc1, 5, 6, aed("-230.00"))
	wantBalance(t, l, acc1, 6, 6, aed("390.00"))
}

func TestFeeDoesNotKeepItselfAlive(t *testing.T) {
	l, store := newLedger(t)
	mustCredit(t, l, "E1", aed("20.00"), 1, 1)
	mustDebit(t, l, "E2", aed("25.00"), 1, 1) // Day 1 closes -5.00: fee, then -30.00
	closeDays(t, l, 1, 1)
	mustCredit(t, l, "E3", aed("10.00"), 2, 1) // Day 1 is +5.00 without its own fee

	closeDays(t, l, 2, 2)

	// With its own fee counted Day 1 would read -20.00 and the fee would
	// justify itself for ever. The trigger is the balance before the fee.
	if _, standing := activeFee(store.Entries(acc1), 1); standing {
		t.Error("fee for day 1 still stands although the day closes at +5.00 before fees")
	}
	wantBalance(t, l, acc1, 1, 2, aed("5.00"))
}

func TestOnlyCuredDaysAreWaived(t *testing.T) {
	l, store := newLedger(t)
	mustDebit(t, l, "E1", aed("100.00"), 1, 1)
	closeDays(t, l, 1, 2) // fees for Day 1 and Day 2: -125.00, -150.00
	mustCredit(t, l, "E2", aed("200.00"), 3, 2)

	closeDays(t, l, 3, 3)

	entries := store.Entries(acc1)
	if _, standing := activeFee(entries, 1); !standing {
		t.Error("fee for day 1 was waived although day 1 still closes at -100.00 before fees")
	}
	if _, standing := activeFee(entries, 2); standing {
		t.Error("fee for day 2 still stands although day 2 now closes at +75.00 before its fee")
	}
	if got := waiverDays(entries); !reflect.DeepEqual(got, []Day{2}) {
		t.Errorf("waiver days = %v, want [2]", got)
	}
	wantBalance(t, l, acc1, 3, 3, aed("75.00"))
}

func TestWaivedFeeIsChargedAgainIfTheDayTurnsNegativeAgain(t *testing.T) {
	l, store := newLedger(t)
	mustCredit(t, l, "E1", aed("20.00"), 1, 1)
	mustDebit(t, l, "E2", aed("25.00"), 1, 1)
	closeDays(t, l, 1, 1) // fee for Day 1
	mustCredit(t, l, "E3", aed("10.00"), 2, 1)
	closeDays(t, l, 2, 2) // waived
	mustDebit(t, l, "E4", aed("20.00"), 3, 1)

	closeDays(t, l, 3, 3) // Day 1 is -15.00 before fees again

	entries := store.Entries(acc1)
	if _, standing := activeFee(entries, 1); !standing {
		t.Error("no fee stands for day 1 although it closes at -15.00 before fees")
	}
	// Fee, waiver, fee for Day 1; and Days 2 and 3 are now negative too.
	if got := feeDays(entries); !reflect.DeepEqual(got, []Day{1, 1, 2, 3}) {
		t.Errorf("fee days = %v, want [1 1 2 3]", got)
	}
	if got := waiverDays(entries); !reflect.DeepEqual(got, []Day{1}) {
		t.Errorf("waiver days = %v, want [1]", got)
	}
	wantBalance(t, l, acc1, 1, 3, aed("-40.00"))
}
