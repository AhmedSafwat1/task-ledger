package replay

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"ledgercore/internal/ledger"
	"ledgercore/internal/money"
)

const (
	acc1 ledger.AccountID = "ACC-001"
	acc2 ledger.AccountID = "ACC-002"
)

func aed(s string) money.Money { return money.MustParse(s, money.AED) }
func bhd(s string) money.Money { return money.MustParse(s, money.BHD) }

func run(t *testing.T, events []Event) *Result {
	t.Helper()
	res, err := Run(events)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return res
}

// without returns the stream with the given events removed: the world in
// which they never happened.
func without(ids ...string) []Event {
	var out []Event
	for _, e := range Stream() {
		if !contains(ids, e.ID) {
			out = append(out, e)
		}
	}
	return out
}

func contains(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func auth(t *testing.T, a AccountReport, authID string) ledger.Authorization {
	t.Helper()
	for _, x := range a.Authorizations {
		if x.AuthID == authID {
			return x
		}
	}
	t.Fatalf("%s not among the authorizations of %s", authID, a.Account)
	return ledger.Authorization{}
}

func feeDays(entries []ledger.Entry, kind ledger.EntryKind) []ledger.Day {
	var days []ledger.Day
	for _, e := range entries {
		if e.Kind == kind {
			days = append(days, e.FeeDay)
		}
	}
	return days
}

// The brief's sanity checkpoints, derived independently by hand before the
// code existed (see WORKLOG.md) and asserted here.
func TestSanityCheckpoints(t *testing.T) {
	res := run(t, Stream())

	for day, want := range map[ledger.Day]string{1: "250.00", 2: "250.00", 3: "650.00", 4: "465.00"} {
		if got := res.Day(day).Account(acc1).Closing; got != aed(want) {
			t.Errorf("ACC-001 closing on Day %d as known that day = %s, want AED %s", day, got, want)
		}
	}
	if got := auth(t, res.Day(2).Account(acc1), "Auth-A").Status; got != ledger.StatusApproved {
		t.Errorf("Auth-A on Day 2 = %s, want APPROVED", got)
	}
	if got := auth(t, res.Day(4).Account(acc1), "Auth-A").Status; got != ledger.StatusSettled {
		t.Errorf("Auth-A on Day 4 = %s, want SETTLED", got)
	}
	if got := auth(t, res.Day(5).Account(acc1), "Auth-B").Status; got != ledger.StatusDeclined {
		t.Errorf("Auth-B on Day 5 = %s, want DECLINED", got)
	}
}

// Criterion 1 (ACCEPTED): the Day 2 closing ledger balance, evaluated at end
// of Day 5 and before any fee is assessed, is AED -370.00.
func TestCriterion1_Accepted_Day2AsOfDay5BeforeFeesIsMinus370(t *testing.T) {
	res := run(t, Stream())
	day5 := res.Day(5).Account(acc1)

	got, err := res.Ledger.BalanceAtSeq(acc1, 2, day5.PreFeeSeq)
	if err != nil {
		t.Fatalf("BalanceAtSeq: %v", err)
	}
	if got != aed("-370.00") {
		t.Errorf("Day 2 closing at end of Day 5 before fees = %s, want AED -370.00", got)
	}
	// The same day as it was known on Day 4, before E7 arrived.
	if was, _ := res.Ledger.Balance(acc1, 2, 4); was != aed("250.00") {
		t.Errorf("Day 2 closing as known on Day 4 = %s, want AED 250.00", was)
	}
}

// Criterion 2 (REJECTED): "E7 causes exactly one overdraft fee to be
// assessed, on Day 2." E7 turns Days 2, 4 and 5 negative; each is its own day
// and each owes its own fee. Three fees, not one.
func TestCriterion2_Rejected_E7CausesThreeFeesNotOne(t *testing.T) {
	res := run(t, Stream())
	for day := ledger.Day(1); day <= 4; day++ {
		if fees := res.Day(day).Account(acc1).Fees; len(fees) != 0 {
			t.Fatalf("Day %d assessed %d fees before E7 existed, want 0", day, len(fees))
		}
	}

	day5 := res.Day(5).Account(acc1)

	var days []ledger.Day
	var triggers []string
	for _, f := range day5.Fees {
		days = append(days, f.Entry.FeeDay)
		triggers = append(triggers, f.Balance.String())
	}
	if !reflect.DeepEqual(days, []ledger.Day{2, 4, 5}) {
		t.Errorf("fees assessed on Day 5 are for days %v, want [2 4 5]", days)
	}
	// Closing of each day just before its own fee, earlier fees included.
	if want := []string{"AED -370.00", "AED -180.00", "AED -205.00"}; !reflect.DeepEqual(triggers, want) {
		t.Errorf("balances that triggered the fees = %v, want %v", triggers, want)
	}
	// Day 3 is the near miss: +30.00 before fees, +5.00 after the Day 2 fee.
	if got, _ := res.Ledger.Balance(acc1, 3, 5); got != aed("5.00") {
		t.Errorf("Day 3 closing as known on Day 5 = %s, want AED 5.00 (positive, so no fee)", got)
	}
	// In a world without E7 there is no fee at all, so all three are E7's.
	counterfactual := run(t, without("E7", "E9"))
	for day := ledger.Day(1); day <= LastDay; day++ {
		if fees := counterfactual.Day(day).Account(acc1).Fees; len(fees) != 0 {
			t.Errorf("without E7, Day %d still assessed %d fees", day, len(fees))
		}
	}
}

// Criterion 3 (ACCEPTED): the Day 4 settlement of Auth-A must be accepted.
func TestCriterion3_Accepted_AuthASettlementIsAccepted(t *testing.T) {
	res := run(t, Stream())
	day4 := res.Day(4)

	a := auth(t, day4.Account(acc1), "Auth-A")
	if a.Status != ledger.StatusSettled || a.Settled != aed("185.00") || a.SettledOn != 4 {
		t.Errorf("Auth-A = %+v, want SETTLED for AED 185.00 on Day 4", a)
	}
	for _, r := range day4.Errors {
		if r.EventID == "E5" {
			t.Errorf("E5 was rejected: %s", r.Reason)
		}
	}
	// 650.00 - 185.00; the 15.00 not used from the 200.00 hold is released.
	if got := day4.Account(acc1).Closing; got != aed("465.00") {
		t.Errorf("Day 4 closing = %s, want AED 465.00", got)
	}
	if got := day4.Account(acc1).Available; got != aed("465.00") {
		t.Errorf("Day 4 available = %s, want AED 465.00 (no hold left)", got)
	}
}

// Criterion 4 (ACCEPTED, with the ambiguity recorded in AMBIGUITIES.md): a
// settlement referencing an authorization that is not in the ledger is
// rejected and the funds do not leave the account.
func TestCriterion4_Accepted_SettlementWithoutAuthorizationIsRejected(t *testing.T) {
	res := run(t, Stream())
	day4 := res.Day(4)

	if len(day4.Errors) != 1 || day4.Errors[0].EventID != "E6" {
		t.Fatalf("Day 4 errors = %+v, want exactly E6", day4.Errors)
	}
	if !strings.Contains(day4.Errors[0].Reason, "unknown authorization") || !strings.Contains(day4.Errors[0].Reason, "Auth-Z") {
		t.Errorf("E6 reason = %q, want it to name the unknown authorization Auth-Z", day4.Errors[0].Reason)
	}
	for _, e := range res.Store.Entries(acc1) {
		if e.EventID == "E6" {
			t.Errorf("E6 produced an entry: %+v", e)
		}
	}
	// Had the AED 180.00 left, Day 4 would close at 285.00.
	if got := day4.Account(acc1).Closing; got != aed("465.00") {
		t.Errorf("Day 4 closing = %s, want AED 465.00", got)
	}
}

// Criterion 5 (ACCEPTED as a conditional): if an authorization is approved,
// its hold reduces available balance but not ledger balance. In the real
// stream Auth-B is declined (E7 comes first), so the property is shown on
// Auth-A, and on Auth-B in the world without E7.
func TestCriterion5_Accepted_ApprovedHoldReducesAvailableNotLedger(t *testing.T) {
	t.Run("Auth-A in the real stream", func(t *testing.T) {
		day2 := run(t, Stream()).Day(2).Account(acc1)
		if got := auth(t, day2, "Auth-A").Status; got != ledger.StatusApproved {
			t.Fatalf("Auth-A = %s, want APPROVED", got)
		}
		if day2.Closing != aed("250.00") {
			t.Errorf("ledger balance = %s, want AED 250.00 (untouched by the hold)", day2.Closing)
		}
		if day2.Available != aed("50.00") {
			t.Errorf("available balance = %s, want AED 50.00 (250.00 - 200.00 held)", day2.Available)
		}
	})
	t.Run("Auth-B is declined in the real stream", func(t *testing.T) {
		day5 := run(t, Stream()).Day(5).Account(acc1)
		b := auth(t, day5, "Auth-B")
		if b.Status != ledger.StatusDeclined || !strings.Contains(b.Reason, "AED -245.00") {
			t.Errorf("Auth-B = %+v, want DECLINED because available would be AED -245.00", b)
		}
	})
	t.Run("Auth-B without E7", func(t *testing.T) {
		day5 := run(t, without("E7", "E9")).Day(5).Account(acc1)
		if got := auth(t, day5, "Auth-B").Status; got != ledger.StatusApproved {
			t.Fatalf("Auth-B without E7 = %s, want APPROVED", got)
		}
		if day5.Closing != aed("465.00") {
			t.Errorf("ledger balance = %s, want AED 465.00 (untouched by the hold)", day5.Closing)
		}
		if day5.Available != aed("375.00") {
			t.Errorf("available balance = %s, want AED 375.00 (465.00 - 90.00 held)", day5.Available)
		}
	})
}

// Criterion 6 (REJECTED): "After E9, all balances and fees return to their
// pre-E7 values." A reversal neutralises an amount. It does not rewind the
// ledger: nothing is removed, the fees are cancelled only later and only by
// new entries, and what was decided while E7 stood stays decided.
func TestCriterion6_Rejected_ReversalDoesNotRestoreEverything(t *testing.T) {
	res := run(t, Stream())
	day6 := res.Day(6).Account(acc1)
	never := run(t, without("E7", "E9")) // the world in which E7 never happened

	// Straight after E9, before the Day 6 close, the three fees still stand:
	// the balance is 390.00, not the 465.00 it was before E7.
	if day6.BeforeFees != aed("390.00") {
		t.Errorf("Day 6 balance after E9, before end of day = %s, want AED 390.00", day6.BeforeFees)
	}

	// The Day 6 close then cancels the fees with three new entries. The fee
	// entries themselves are still in the ledger.
	var waived []ledger.Day
	for _, f := range day6.Fees {
		if f.Entry.Kind == ledger.FeeWaiver {
			waived = append(waived, f.Entry.FeeDay)
		}
	}
	if !reflect.DeepEqual(waived, []ledger.Day{2, 4, 5}) {
		t.Errorf("waivers on Day 6 = %v, want [2 4 5]", waived)
	}
	entries := res.Store.Entries(acc1)
	if got := feeDays(entries, ledger.Fee); !reflect.DeepEqual(got, []ledger.Day{2, 4, 5}) {
		t.Errorf("fee entries on record = %v, want [2 4 5] still present", got)
	}
	// E7, three fees, E9, three waivers: eight entries a rewind would not have.
	if extra := len(entries) - len(never.Store.Entries(acc1)); extra != 8 {
		t.Errorf("ledger has %d more entries than the world without E7, want 8", extra)
	}

	// What is true: the amounts net out, so the closing ledger balance matches.
	if got, want := day6.Closing, never.Day(6).Account(acc1).Closing; got != want {
		t.Errorf("Day 6 closing = %s, without E7 it is %s; the amounts should net out", got, want)
	}

	// What is not restored: Auth-B was declined because of E7 and stays
	// declined, so the available balance differs from the world without E7.
	if got := auth(t, day6, "Auth-B").Status; got != ledger.StatusDeclined {
		t.Errorf("Auth-B after E9 = %s, want DECLINED", got)
	}
	if got := auth(t, never.Day(6).Account(acc1), "Auth-B").Status; got != ledger.StatusApproved {
		t.Errorf("Auth-B without E7 = %s, want APPROVED", got)
	}
	if day6.Available == never.Day(6).Account(acc1).Available {
		t.Errorf("available balance %s equals the world without E7; the declined Auth-B should make it differ", day6.Available)
	}

	// And the record of what was known on Day 5 is not erased.
	if got, _ := res.Ledger.Balance(acc1, 2, 5); got != aed("-395.00") {
		t.Errorf("Day 2 as known on Day 5 = %s, want AED -395.00 still on record", got)
	}
}

// Criterion 7 (REJECTED): "The three BHD instalments in E10 must each be BHD
// 3.334." Three times 3.334 is 10.002, which is not the 10.000 credited.
func TestCriterion7_Rejected_InstalmentsSumToTheAmountCredited(t *testing.T) {
	res := run(t, Stream())

	var parts []string
	sum := money.Zero(money.BHD)
	for _, e := range res.Store.Entries(acc2) {
		if e.EventID != "E10" {
			continue
		}
		parts = append(parts, e.Amount.String())
		var err error
		if sum, err = sum.Add(e.Amount); err != nil {
			t.Fatalf("sum: %v", err)
		}
	}
	if want := []string{"BHD 3.333", "BHD 3.333", "BHD 3.334"}; !reflect.DeepEqual(parts, want) {
		t.Errorf("E10 instalments = %v, want %v", parts, want)
	}
	if sum != bhd("10.000") {
		t.Errorf("instalments sum to %s, want BHD 10.000", sum)
	}

	asCriterionAsks := money.Zero(money.BHD)
	for range 3 {
		asCriterionAsks, _ = asCriterionAsks.Add(bhd("3.334"))
	}
	if asCriterionAsks != bhd("10.002") {
		t.Fatalf("3 x 3.334 = %s, expected BHD 10.002", asCriterionAsks)
	}
	if asCriterionAsks == bhd("10.000") {
		t.Error("3 x 3.334 equals the credited amount; the criterion would then be acceptable")
	}
}

// Criterion 8 (REJECTED): "If the rounded daily interest accruals do not sum
// to the capitalized total, the remainder is discarded." Rule 2 says they
// must sum exactly, so there is never a remainder to discard: the rounding
// residual is carried from line to line and the capitalized total is defined
// as the sum of the recorded lines.
func TestCriterion8_Rejected_ResidualIsCarriedNotDiscarded(t *testing.T) {
	res := run(t, Stream())

	for _, id := range []ledger.AccountID{acc1, acc2} {
		log := res.Store.Accruals(id)
		total := money.Zero(log[0].Amount.Currency())
		for i, a := range log {
			var err error
			if total, err = total.Add(a.Amount); err != nil {
				t.Fatalf("sum: %v", err)
			}
			if i > 0 && a.CarryIn != log[i-1].CarryOut {
				t.Errorf("%s accrual %d starts from carry %d, previous line left %d", id, i, a.CarryIn, log[i-1].CarryOut)
			}
		}
		capitalized := res.Day(6).Account(id).Capitalized
		if capitalized == nil || capitalized.Amount != total {
			t.Errorf("%s: accrual lines sum to %s, capitalized %+v", id, total, capitalized)
		}
	}
	if got := res.Day(6).Account(acc1).Capitalized.Amount; got != aed("1.02") {
		t.Errorf("ACC-001 capitalized %s, want AED 1.02", got)
	}
	if got := res.Day(6).Account(acc2).Capitalized.Amount; got != bhd("0.008") {
		t.Errorf("ACC-002 capitalized %s, want BHD 0.008", got)
	}

	// The residual exists and is kept: exact interest is 1.018, 1.02 was
	// credited, and the 0.2 fil difference sits in the carry for the next day.
	log := res.Store.Accruals(acc1)
	if got := log[len(log)-1].CarryOut; got != -2000 {
		t.Errorf("ACC-001 residual after Day 6 = %d/10000 of a fil, want -2000", got)
	}

	// What the criterion describes: round each day on its own and the lines
	// (1.03) disagree with the rounded exact total (1.02) by a fil.
	cfg := ledger.DefaultConfig()
	var naive, exact int64
	for day := ledger.Day(1); day <= LastDay; day++ {
		// Interest basis of each day: the closing balance without Day 6's own capitalization.
		balance := map[ledger.Day]string{1: "250.00", 2: "250.00", 3: "650.00", 4: "465.00", 5: "465.00", 6: "465.00"}[day]
		raw := aed(balance).Minor() * cfg.InterestNumerator
		rounded, err := money.DivRoundHalfEven(raw, cfg.InterestDenominator)
		if err != nil {
			t.Fatalf("round: %v", err)
		}
		naive += rounded
		exact += raw
	}
	roundedTotal, _ := money.DivRoundHalfEven(exact, cfg.InterestDenominator)
	if naive != 103 || roundedTotal != 102 {
		t.Errorf("independent daily rounding = %d fils, rounded exact total = %d fils; expected 103 and 102", naive, roundedTotal)
	}
}

// E10 is listed after E9 (booked Day 6) but is itself booked Day 5.
func TestE10IsAppliedOnItsBookedDay(t *testing.T) {
	stream := Stream()
	if last := stream[len(stream)-1]; last.ID != "E10" || last.Booked != 5 {
		t.Fatalf("last event in the stream = %+v, want E10 booked Day 5", last)
	}

	res := run(t, stream)

	var ids []string
	for _, o := range res.Day(5).Events {
		ids = append(ids, o.Event.ID)
	}
	if want := []string{"E7", "E8", "E10"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("events applied on Day 5 = %v, want %v", ids, want)
	}
	if got := res.Day(5).Account(acc2).Closing; got != bhd("10.000") {
		t.Errorf("ACC-002 closing on Day 5 as known that day = %s, want BHD 10.000", got)
	}
	if got := res.Day(6).Account(acc2).Closing; got != bhd("10.008") {
		t.Errorf("ACC-002 closing on Day 6 = %s, want BHD 10.008", got)
	}
}

// Replayed literally, E10 would be booked into Day 5 after Day 5 was closed.
// The ledger refuses that, which is why Run groups by booked day first.
func TestE10InLiteralStreamOrderIsRefusedByTheLedger(t *testing.T) {
	store := ledger.NewMemStore()
	l := ledger.New(store, ledger.DefaultConfig())
	for _, a := range Accounts() {
		if err := l.OpenAccount(a.ID, a.Currency); err != nil {
			t.Fatalf("open: %v", err)
		}
	}
	for day := ledger.Day(1); day <= 5; day++ {
		if _, err := l.CloseDay(day); err != nil {
			t.Fatalf("close: %v", err)
		}
	}
	stream := Stream()

	_, err := Apply(l, stream[len(stream)-1])

	if !errors.Is(err, ledger.ErrDayClosed) {
		t.Errorf("applying E10 after Day 5 closed error = %v, want ErrDayClosed", err)
	}
	if n := len(store.Entries(acc2)); n != 0 {
		t.Errorf("ACC-002 has %d entries, want 0", n)
	}
}

func TestFinalBalances(t *testing.T) {
	res := run(t, Stream())
	day6 := res.Day(6)

	if got := day6.Account(acc1).Closing; got != aed("466.02") {
		t.Errorf("ACC-001 closing on Day 6 = %s, want AED 466.02", got)
	}
	if got := day6.Account(acc2).Closing; got != bhd("10.008") {
		t.Errorf("ACC-002 closing on Day 6 = %s, want BHD 10.008", got)
	}
	if got := res.Day(5).Account(acc1).Closing; got != aed("-230.00") {
		t.Errorf("ACC-001 closing on Day 5 = %s, want AED -230.00", got)
	}
	if got := res.Day(5).Account(acc1).BeforeFees; got != aed("-155.00") {
		t.Errorf("ACC-001 on Day 5 before fees = %s, want AED -155.00", got)
	}
}
