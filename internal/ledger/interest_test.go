package ledger

import (
	"errors"
	"reflect"
	"testing"

	"ledgercore/internal/money"
)

// newInterestLedger is newLedger with the full default schedule, interest included.
func newInterestLedger(t *testing.T) (*Ledger, *MemStore) {
	t.Helper()
	return newLedgerWith(t, DefaultConfig())
}

// accrualLine is the part of an accrual record the tests compare.
type accrualLine struct {
	Kind   AccrualKind
	ForDay Day
	Booked Day
	Amount string
}

func lines(accruals []Accrual) []accrualLine {
	var out []accrualLine
	for _, a := range accruals {
		out = append(out, accrualLine{a.Kind, a.ForDay, a.Booked, a.Amount.String()})
	}
	return out
}

func sumAccruals(t *testing.T, cur money.Currency, accruals []Accrual) money.Money {
	t.Helper()
	total := money.Zero(cur)
	for _, a := range accruals {
		var err error
		if total, err = total.Add(a.Amount); err != nil {
			t.Fatalf("sum accruals: %v", err)
		}
	}
	return total
}

func TestDailyInterestOnPositiveClosingBalance(t *testing.T) {
	l, store := newInterestLedger(t)
	mustCredit(t, l, "E1", aed("250.00"), 1, 1)

	closed := closeDays(t, l, 1, 1)

	// 250.00 x 0.04% = 0.10 exactly, so nothing is carried.
	got := accountClose(t, closed, acc1).Accruals
	if len(got) != 1 {
		t.Fatalf("%d accrual lines on close, want 1", len(got))
	}
	want := Accrual{
		Seq: got[0].Seq, Account: acc1, Kind: AccrualDaily, ForDay: 1, Booked: 1,
		Basis: aed("250.00"), Amount: aed("0.10"),
	}
	if got[0] != want {
		t.Errorf("accrual = %+v, want %+v", got[0], want)
	}
	if stored := store.Accruals(acc1); len(stored) != 1 || stored[0] != want {
		t.Errorf("accrual log = %+v, want exactly the line reported by the close", stored)
	}
	// Accrued interest is not in the ledger balance until it is capitalized.
	wantBalance(t, l, acc1, 1, 1, aed("250.00"))
}

func TestNoInterestOnZeroOrNegativeBalance(t *testing.T) {
	tests := []struct {
		name  string
		debit string
		basis string
	}{
		{"zero", "100.00", "0.00"},
		{"negative", "150.00", "-75.00"}, // -50.00, then the overdraft fee
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l, store := newInterestLedger(t)
			mustCredit(t, l, "E1", aed("100.00"), 1, 1)
			mustDebit(t, l, "E2", aed(tc.debit), 1, 1)

			closeDays(t, l, 1, 1)

			got := store.Accruals(acc1)
			if len(got) != 1 || got[0].Amount != aed("0.00") || got[0].Basis != aed(tc.basis) {
				t.Errorf("accruals = %+v, want one line of AED 0.00 on a basis of AED %s", got, tc.basis)
			}
		})
	}
}

func TestRoundingResidualIsCarriedToTheNextDay(t *testing.T) {
	l, store := newInterestLedger(t)
	mustCredit(t, l, "E1", aed("465.00"), 1, 1)

	closeDays(t, l, 1, 3)

	// 465.00 x 0.04% = 0.186 a day. Rounded on its own every day would be
	// 0.19; with the residual carried the days are 0.19, 0.18, 0.19 and the
	// total 0.56 is the rounding of the exact 0.558.
	got := store.Accruals(acc1)
	want := []accrualLine{
		{AccrualDaily, 1, 1, "AED 0.19"},
		{AccrualDaily, 2, 2, "AED 0.18"},
		{AccrualDaily, 3, 3, "AED 0.19"},
	}
	if !reflect.DeepEqual(lines(got), want) {
		t.Errorf("accruals = %v, want %v", lines(got), want)
	}
	carries := []int64{got[0].CarryOut, got[1].CarryOut, got[2].CarryOut}
	if !reflect.DeepEqual(carries, []int64{-4000, 2000, -2000}) {
		t.Errorf("carry after each day = %v, want [-4000 2000 -2000] (in 1/10000 of a fil)", carries)
	}
	if got[1].CarryIn != got[0].CarryOut || got[2].CarryIn != got[1].CarryOut {
		t.Errorf("each line must start from the residual the previous one left: %+v", got)
	}
	if total := sumAccruals(t, money.AED, got); total != aed("0.56") {
		t.Errorf("accruals sum to %s, want AED 0.56", total)
	}
}

func TestExactHalfRoundsToEven(t *testing.T) {
	l, store := newInterestLedger(t)
	mustCredit(t, l, "E1", aed("12.50"), 1, 1) // 12.50 x 0.04% = exactly half a fil

	closeDays(t, l, 1, 2)

	want := []accrualLine{
		{AccrualDaily, 1, 1, "AED 0.00"}, // 0.5 goes to the even neighbour, 0
		{AccrualDaily, 2, 2, "AED 0.01"}, // carried 0.5 + 0.5 = 1 exactly
	}
	if got := lines(store.Accruals(acc1)); !reflect.DeepEqual(got, want) {
		t.Errorf("accruals = %v, want %v", got, want)
	}
}

func TestBackdatedChangeAppendsAdjustmentsAndCapitalizationMatchesTheLog(t *testing.T) {
	l, store := newInterestLedger(t)
	replayThroughDay4(t, l)

	want := []accrualLine{
		{AccrualDaily, 1, 1, "AED 0.10"},
		{AccrualDaily, 2, 2, "AED 0.10"},
		{AccrualDaily, 3, 3, "AED 0.26"},
		{AccrualDaily, 4, 4, "AED 0.19"},
	}
	if got := lines(store.Accruals(acc1)); !reflect.DeepEqual(got, want) {
		t.Fatalf("accruals after Day 4 = %v, want %v", got, want)
	}
	logAfterDay4 := store.Accruals(acc1)

	// Day 5: E7 turns Days 2, 4 and 5 negative and leaves Day 3 at 5.00.
	mustDebit(t, l, "E7", aed("620.00"), 5, 2)
	closeDays(t, l, 5, 5)

	want = append(want,
		accrualLine{AccrualAdjustment, 2, 5, "AED -0.10"},
		accrualLine{AccrualAdjustment, 3, 5, "AED -0.26"},
		accrualLine{AccrualAdjustment, 4, 5, "AED -0.19"},
		accrualLine{AccrualDaily, 5, 5, "AED 0.00"},
	)
	logAfterDay5 := store.Accruals(acc1)
	if got := lines(logAfterDay5); !reflect.DeepEqual(got, want) {
		t.Fatalf("accruals after Day 5 = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(logAfterDay5[:len(logAfterDay4)], logAfterDay4) {
		t.Error("accrual lines written before Day 5 were changed; corrections must be appended")
	}
	if total := sumAccruals(t, money.AED, logAfterDay5); total != aed("0.10") {
		t.Errorf("accrued to date after Day 5 = %s, want AED 0.10", total)
	}

	// Day 6: E9 restores every day; fees are waived first, then interest is
	// corrected on the restored balances and capitalized.
	reverse(t, l, "E9", "E7", 6, 2)
	closed := closeDays(t, l, 6, 6)

	want = append(want,
		accrualLine{AccrualAdjustment, 2, 6, "AED 0.10"},
		accrualLine{AccrualAdjustment, 3, 6, "AED 0.26"},
		accrualLine{AccrualAdjustment, 4, 6, "AED 0.19"},
		accrualLine{AccrualAdjustment, 5, 6, "AED 0.18"},
		accrualLine{AccrualDaily, 6, 6, "AED 0.19"},
	)
	log := store.Accruals(acc1)
	if got := lines(log); !reflect.DeepEqual(got, want) {
		t.Fatalf("accruals after Day 6 = %v, want %v", got, want)
	}

	capitalized := accountClose(t, closed, acc1).Capitalized
	if capitalized == nil {
		t.Fatal("no capitalization entry on Day 6")
	}
	if capitalized.Kind != Interest || capitalized.Amount != aed("1.02") || capitalized.Value != 6 || capitalized.Booked != 6 {
		t.Errorf("capitalization = %+v, want INTEREST of AED 1.02, value day 6, booked day 6", *capitalized)
	}
	if total := sumAccruals(t, money.AED, log); total != capitalized.Amount {
		t.Errorf("recorded accruals sum to %s but %s was capitalized", total, capitalized.Amount)
	}
	// Exact interest is 1.018; the 0.2 fil over-credit stays in the carry.
	if last := log[len(log)-1]; last.CarryOut != -2000 {
		t.Errorf("residual after capitalization = %d, want -2000 (carried, not discarded)", last.CarryOut)
	}
	wantBalance(t, l, acc1, 6, 6, aed("466.02"))
}

func TestCapitalizationEqualsSumOfRecordedAccruals(t *testing.T) {
	// Balances chosen so that every day leaves a different residual.
	for _, balance := range []string{"0.01", "12.50", "123.45", "465.00", "999.99", "1,234,567.89"} {
		t.Run(balance, func(t *testing.T) {
			l, store := newInterestLedger(t)
			mustCredit(t, l, "E1", aed(balance), 1, 1)

			closed := closeDays(t, l, 1, 6)

			total := sumAccruals(t, money.AED, store.Accruals(acc1))
			capitalized := accountClose(t, closed, acc1).Capitalized
			switch {
			case total.IsZero() && capitalized != nil:
				t.Errorf("capitalized %s although nothing accrued", capitalized.Amount)
			case !total.IsZero() && (capitalized == nil || capitalized.Amount != total):
				t.Errorf("accruals sum to %s, capitalization = %+v", total, capitalized)
			}
		})
	}
}

func TestInterestInBHDUsesThreeDecimals(t *testing.T) {
	l, store := newInterestLedger(t)
	closeDays(t, l, 1, 4)
	if _, err := l.CreditInstalments(Posting{EventID: "E10", Account: acc2, Amount: bhd("10.000"), Booked: 5, Value: 5}, 3); err != nil {
		t.Fatalf("CreditInstalments: %v", err)
	}

	closed := closeDays(t, l, 5, 6)

	want := []accrualLine{
		{AccrualDaily, 1, 1, "BHD 0.000"},
		{AccrualDaily, 2, 2, "BHD 0.000"},
		{AccrualDaily, 3, 3, "BHD 0.000"},
		{AccrualDaily, 4, 4, "BHD 0.000"},
		{AccrualDaily, 5, 5, "BHD 0.004"},
		{AccrualDaily, 6, 6, "BHD 0.004"},
	}
	if got := lines(store.Accruals(acc2)); !reflect.DeepEqual(got, want) {
		t.Errorf("accruals = %v, want %v", got, want)
	}
	capitalized := accountClose(t, closed, acc2).Capitalized
	if capitalized == nil || capitalized.Amount != bhd("0.008") {
		t.Errorf("capitalization = %+v, want BHD 0.008", capitalized)
	}
	wantBalance(t, l, acc2, 6, 6, bhd("10.008"))
}

func TestNothingIsCapitalizedBeforeTheCapitalizationDay(t *testing.T) {
	l, store := newInterestLedger(t)
	mustCredit(t, l, "E1", aed("465.00"), 1, 1)

	for d := Day(1); d <= 5; d++ {
		closed := closeDays(t, l, d, d)
		if c := accountClose(t, closed, acc1).Capitalized; c != nil {
			t.Errorf("day %d capitalized %s, want nothing before day 6", d, c.Amount)
		}
	}
	for _, e := range store.Entries(acc1) {
		if e.Kind == Interest {
			t.Errorf("interest entry %+v exists before the capitalization day", e)
		}
	}
}

func TestCapitalizedInterestStartsEarningTheNextDay(t *testing.T) {
	l, store := newInterestLedger(t)
	mustCredit(t, l, "E1", aed("465.00"), 1, 1)
	closeDays(t, l, 1, 6) // capitalizes on Day 6; Day 6 itself accrued on 465.00
	day6 := store.Accruals(acc1)[5]
	if day6.ForDay != 6 || day6.Basis != aed("465.00") {
		t.Fatalf("Day 6 accrual = %+v, want a basis of AED 465.00 (before the capitalization credit)", day6)
	}

	closed := closeDays(t, l, 7, 7)

	// The credit is value-dated Day 6 but is the result of Day 6's interest,
	// so it must not send Day 6 back for an adjustment. It earns from Day 7.
	got := accountClose(t, closed, acc1).Accruals
	if len(got) != 1 || got[0].Kind != AccrualDaily || got[0].ForDay != 7 {
		t.Fatalf("Day 7 accruals = %+v, want only the daily line for Day 7", got)
	}
	balance, err := l.Balance(acc1, 7, 7)
	if err != nil {
		t.Fatalf("Balance: %v", err)
	}
	if got[0].Basis != balance {
		t.Errorf("Day 7 basis = %s, want the full balance %s including capitalized interest", got[0].Basis, balance)
	}
}

func TestCapitalizationDoesNotUndoTheSameDaysFee(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CapitalizationDay = 2
	l, store := newLedgerWith(t, cfg)
	mustCredit(t, l, "E1", aed("100,000.00"), 1, 1) // Day 1 earns 40.00
	closeDays(t, l, 1, 1)
	mustDebit(t, l, "E2", aed("100,010.00"), 2, 2) // Day 2 closes -10.00: fee

	closeDays(t, l, 2, 2)
	// -10.00 - 25.00 fee + 40.00 capitalized interest: Day 2 now reads +5.00.
	wantBalance(t, l, acc1, 2, 2, aed("5.00"))

	closed := closeDays(t, l, 3, 3)

	// The fee was decided on the balance before the capitalization credit and
	// that decision must not flip once the credit is on the books.
	if got := accountClose(t, closed, acc1).Fees; len(got) != 0 {
		t.Errorf("Day 3 close appended %+v, want no fee changes", got)
	}
	if _, standing := activeFee(store.Entries(acc1), 2); !standing {
		t.Error("fee for day 2 was waived because of the same day's interest credit")
	}
}

func TestInvalidInterestRateFailsTheClose(t *testing.T) {
	cfg := DefaultConfig()
	cfg.InterestDenominator = 0
	l, _ := newLedgerWith(t, cfg)

	if _, err := l.CloseDay(1); !errors.Is(err, money.ErrInvalidDivisor) {
		t.Errorf("CloseDay with a zero rate denominator error = %v, want ErrInvalidDivisor", err)
	}
}
