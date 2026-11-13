package ledger

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"ledgercore/internal/money"
)

// closeDays closes every day from first to last and returns the last close.
func closeDays(t *testing.T, l *Ledger, first, last Day) DayClose {
	t.Helper()
	var closed DayClose
	for d := first; d <= last; d++ {
		var err error
		if closed, err = l.CloseDay(d); err != nil {
			t.Fatalf("close day %d: %v", d, err)
		}
	}
	return closed
}

func accountClose(t *testing.T, c DayClose, id AccountID) AccountClose {
	t.Helper()
	for _, a := range c.Accounts {
		if a.Account == id {
			return a
		}
	}
	t.Fatalf("day %d close has no section for %s", c.Day, id)
	return AccountClose{}
}

// feeDays lists the days that carry a fee entry, in append order.
func feeDays(entries []Entry) []Day {
	var days []Day
	for _, e := range entries {
		if e.Kind == Fee {
			days = append(days, e.FeeDay)
		}
	}
	return days
}

// replayThroughDay4 posts E1..E6 of the brief and closes Days 1 to 4.
func replayThroughDay4(t *testing.T, l *Ledger) {
	t.Helper()
	mustCredit(t, l, "E1", aed("1,200.00"), 1, 1)
	mustDebit(t, l, "E2", aed("950.00"), 1, 1)
	closeDays(t, l, 1, 1)
	authorize(t, l, "E3", "Auth-A", aed("200.00"), 2)
	closeDays(t, l, 2, 2)
	mustCredit(t, l, "E4", aed("400.00"), 3, 3)
	closeDays(t, l, 3, 3)
	if _, err := l.Settle(SettleRequest{EventID: "E5", Account: acc1, AuthID: "Auth-A", Amount: aed("185.00"), Booked: 4, Value: 4}); err != nil {
		t.Fatalf("settle E5: %v", err)
	}
	if _, err := l.Settle(SettleRequest{EventID: "E6", Account: acc1, AuthID: "Auth-Z", Amount: aed("180.00"), Booked: 4, Value: 4}); !errors.Is(err, ErrUnknownAuthorization) {
		t.Fatalf("settle E6 error = %v, want ErrUnknownAuthorization", err)
	}
	closeDays(t, l, 4, 4)
}

func TestNegativeClosingBalanceGetsOneFeeValueDatedThatDay(t *testing.T) {
	l, store := newLedger(t)
	mustDebit(t, l, "E1", aed("100.00"), 1, 1)

	closed := closeDays(t, l, 1, 1)

	fees := accountClose(t, closed, acc1).Fees
	if len(fees) != 1 {
		t.Fatalf("%d fee entries on close, want 1", len(fees))
	}
	fee := fees[0]
	if fee.Kind != Fee || fee.Amount != aed("-25.00") || fee.FeeDay != 1 || fee.Value != 1 || fee.Booked != 1 {
		t.Errorf("fee = %+v, want FEE of AED -25.00 for day 1, value day 1, booked day 1", fee)
	}
	wantBalance(t, l, acc1, 1, 1, aed("-125.00"))
	if got := feeDays(store.Entries(acc1)); !reflect.DeepEqual(got, []Day{1}) {
		t.Errorf("fee days = %v, want [1]", got)
	}
}

func TestFeeIsAssessedOncePerDayPerAccount(t *testing.T) {
	l, store := newLedger(t)
	mustDebit(t, l, "E1", aed("100.00"), 1, 1)

	closeDays(t, l, 1, 3)

	// Each of the three days closes negative and gets exactly one fee; closing
	// later days must not charge an earlier day again.
	if got := feeDays(store.Entries(acc1)); !reflect.DeepEqual(got, []Day{1, 2, 3}) {
		t.Errorf("fee days = %v, want [1 2 3]", got)
	}
	wantBalance(t, l, acc1, 3, 3, aed("-175.00"))
}

func TestNoFeeWhenClosingBalanceIsZeroOrPositive(t *testing.T) {
	tests := []struct {
		name  string
		debit string
	}{
		{"positive", "99.99"},
		{"exactly zero", "100.00"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l, store := newLedger(t)
			mustCredit(t, l, "E1", aed("100.00"), 1, 1)
			mustDebit(t, l, "E2", aed(tc.debit), 1, 1)

			closeDays(t, l, 1, 1)

			if got := feeDays(store.Entries(acc1)); len(got) != 0 {
				t.Errorf("fee days = %v, want none", got)
			}
		})
	}
}

func TestBackdatedDebitTriggersFeesOnEveryDayItTurnsNegative(t *testing.T) {
	l, store := newLedger(t)
	replayThroughDay4(t, l)
	if got := feeDays(store.Entries(acc1)); len(got) != 0 {
		t.Fatalf("fees before E7 = %v, want none", got)
	}

	mustDebit(t, l, "E7", aed("620.00"), 5, 2)
	closed := closeDays(t, l, 5, 5)
	section := accountClose(t, closed, acc1)

	// Before any fee: Day 2 -370.00, Day 3 +30.00, Day 4 -155.00, Day 5 -155.00.
	for _, tc := range []struct {
		day  Day
		want string
	}{{2, "-370.00"}, {3, "30.00"}, {4, "-155.00"}, {5, "-155.00"}} {
		got, err := l.BalanceAtSeq(acc1, tc.day, section.PreFeeSeq)
		if err != nil {
			t.Fatalf("BalanceAtSeq: %v", err)
		}
		if got != aed(tc.want) {
			t.Errorf("day %d before fees = %s, want AED %s", tc.day, got, tc.want)
		}
	}

	if got := feeDays(section.Fees); !reflect.DeepEqual(got, []Day{2, 4, 5}) {
		t.Errorf("fees assessed for days %v, want [2 4 5]", got)
	}
	for _, fee := range section.Fees {
		if fee.Booked != 5 || fee.Value != fee.FeeDay {
			t.Errorf("fee for day %d booked day %d value day %d, want booked 5 and value-dated on the fee day", fee.FeeDay, fee.Booked, fee.Value)
		}
	}

	// After fees. Day 3 keeps 5.00 after the Day 2 fee, so it gets no fee.
	wantBalance(t, l, acc1, 2, 5, aed("-395.00"))
	wantBalance(t, l, acc1, 3, 5, aed("5.00"))
	wantBalance(t, l, acc1, 4, 5, aed("-205.00"))
	wantBalance(t, l, acc1, 5, 5, aed("-230.00"))
	// What was known on Day 4 has not changed.
	wantBalance(t, l, acc1, 2, 4, aed("250.00"))
	wantBalance(t, l, acc1, 4, 4, aed("465.00"))
}

func TestFeeCanPushALaterDayNegative(t *testing.T) {
	l, store := newLedger(t)
	mustCredit(t, l, "E1", aed("20.00"), 1, 1)
	mustDebit(t, l, "E2", aed("25.00"), 1, 1) // Day 1 closes -5.00: fee, then -30.00
	closeDays(t, l, 1, 1)
	mustCredit(t, l, "E3", aed("20.00"), 2, 2) // Day 2 would be +15.00 without the Day 1 fee

	closeDays(t, l, 2, 2)

	// -5.00 + 20.00 - 25.00 (Day 1 fee) = -10.00: negative only because of the fee.
	if got := feeDays(store.Entries(acc1)); !reflect.DeepEqual(got, []Day{1, 2}) {
		t.Errorf("fee days = %v, want [1 2]", got)
	}
	wantBalance(t, l, acc1, 2, 2, aed("-35.00"))
}

func TestRetroactiveFeeCascadesThroughLaterDays(t *testing.T) {
	l, store := newLedger(t)
	mustCredit(t, l, "E1", aed("10.00"), 1, 1)
	closeDays(t, l, 1, 1)
	mustCredit(t, l, "E2", aed("10.00"), 2, 2)
	closeDays(t, l, 2, 3) // Days 1..3 close at 10.00, 20.00, 20.00: no fees
	mustDebit(t, l, "E3", aed("15.00"), 4, 1)

	closed := closeDays(t, l, 4, 4)
	section := accountClose(t, closed, acc1)

	// Before fees Day 1 is -5.00 but Days 2..4 are +5.00. The Day 1 fee drags
	// Day 2 to -20.00, whose fee drags Day 3 down, and so on.
	if got, _ := l.BalanceAtSeq(acc1, 2, section.PreFeeSeq); got != aed("5.00") {
		t.Fatalf("day 2 before fees = %s, want AED 5.00", got)
	}
	if got := feeDays(store.Entries(acc1)); !reflect.DeepEqual(got, []Day{1, 2, 3, 4}) {
		t.Errorf("fee days = %v, want [1 2 3 4]", got)
	}
	wantBalance(t, l, acc1, 4, 4, aed("-95.00"))
}

func TestCurrencyWithoutAFeeDefinedGetsNoFeeAndANotice(t *testing.T) {
	l, store := newLedger(t)
	if _, err := l.Debit(Posting{EventID: "E1", Account: acc2, Amount: bhd("1.000"), Booked: 1, Value: 1}); err != nil {
		t.Fatalf("debit: %v", err)
	}

	closed := closeDays(t, l, 1, 1)
	section := accountClose(t, closed, acc2)

	if n := len(store.Entries(acc2)); n != 1 {
		t.Errorf("ACC-002 has %d entries, want only the debit (no BHD fee is defined)", n)
	}
	if len(section.Notices) != 1 || !strings.Contains(section.Notices[0], "BHD") {
		t.Errorf("notices = %q, want one naming BHD", section.Notices)
	}
	wantBalance(t, l, acc2, 1, 1, bhd("-1.000"))
}

func TestFeeIsBookedInTheAccountCurrencyOnly(t *testing.T) {
	store := NewMemStore()
	l := New(store, Config{OverdraftFee: map[string]money.Money{"BHD": aed("25.00")}})
	if err := l.OpenAccount(acc2, money.BHD); err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := l.Debit(Posting{EventID: "E1", Account: acc2, Amount: bhd("1.000"), Booked: 1, Value: 1}); err != nil {
		t.Fatalf("debit: %v", err)
	}

	if _, err := l.CloseDay(1); !errors.Is(err, money.ErrCurrencyMismatch) {
		t.Errorf("CloseDay with an AED fee configured for BHD error = %v, want ErrCurrencyMismatch", err)
	}
	if n := len(store.Entries(acc2)); n != 1 {
		t.Errorf("ACC-002 has %d entries, want only the debit", n)
	}
}

func TestDaysCloseInOrderExactlyOnce(t *testing.T) {
	l, _ := newLedger(t)

	if _, err := l.CloseDay(2); !errors.Is(err, ErrCloseOutOfOrder) {
		t.Errorf("closing day 2 first error = %v, want ErrCloseOutOfOrder", err)
	}
	closeDays(t, l, 1, 1)
	if _, err := l.CloseDay(1); !errors.Is(err, ErrCloseOutOfOrder) {
		t.Errorf("closing day 1 twice error = %v, want ErrCloseOutOfOrder", err)
	}
}

func TestPostingIntoAClosedBookedDayIsRejected(t *testing.T) {
	l, store := newLedger(t)
	mustCredit(t, l, "E1", aed("100.00"), 1, 1)
	closeDays(t, l, 1, 2)

	// Booked Day 2 is closed: accepting this would silently change what
	// "as known on Day 2" means. The same event booked today, value-dated
	// Day 2, is the supported way to say it.
	_, err := l.Credit(Posting{EventID: "LATE", Account: acc1, Amount: aed("5.00"), Booked: 2, Value: 2})

	if !errors.Is(err, ErrDayClosed) {
		t.Errorf("Credit booked on a closed day error = %v, want ErrDayClosed", err)
	}
	wantBalance(t, l, acc1, 2, 2, aed("100.00"))
	if r := store.Rejections(); len(r) != 1 || r[0].EventID != "LATE" {
		t.Errorf("rejections = %+v, want one for LATE", r)
	}
	if _, err := l.Credit(Posting{EventID: "OK", Account: acc1, Amount: aed("5.00"), Booked: 3, Value: 2}); err != nil {
		t.Errorf("Credit booked today, value-dated Day 2: %v", err)
	}
	wantBalance(t, l, acc1, 2, 3, aed("105.00"))
	wantBalance(t, l, acc1, 2, 2, aed("100.00"))
}
