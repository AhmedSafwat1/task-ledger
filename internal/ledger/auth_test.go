package ledger

import (
	"errors"
	"strings"
	"testing"

	"ledgercore/internal/money"
)

func authorize(t *testing.T, l *Ledger, eventID, authID string, amount money.Money, day Day) Authorization {
	t.Helper()
	a, err := l.Authorize(AuthRequest{EventID: eventID, Account: acc1, AuthID: authID, Amount: amount, Booked: day, Value: day})
	if err != nil {
		t.Fatalf("authorize %s: %v", authID, err)
	}
	return a
}

func wantAvailable(t *testing.T, l *Ledger, value Day, want money.Money) {
	t.Helper()
	got, err := l.Available(acc1, value)
	if err != nil {
		t.Fatalf("Available: %v", err)
	}
	if got != want {
		t.Errorf("available balance on day %d = %s, want %s", value, got, want)
	}
}

func wantStatus(t *testing.T, l *Ledger, authID string, want AuthStatus) Authorization {
	t.Helper()
	auths, err := l.Authorizations(acc1)
	if err != nil {
		t.Fatalf("Authorizations: %v", err)
	}
	for _, a := range auths {
		if a.AuthID == authID {
			if a.Status != want {
				t.Errorf("%s status = %s, want %s", authID, a.Status, want)
			}
			return a
		}
	}
	t.Fatalf("%s not found among authorizations", authID)
	return Authorization{}
}

func TestAuthorizationApprovedWhenAvailableStaysNonNegative(t *testing.T) {
	l, _ := newLedger(t)
	mustCredit(t, l, "E1", aed("1,200.00"), 1, 1)
	mustDebit(t, l, "E2", aed("950.00"), 1, 1)

	got := authorize(t, l, "E3", "Auth-A", aed("200.00"), 2)

	if got.Status != StatusApproved {
		t.Fatalf("Auth-A status = %s, want APPROVED", got.Status)
	}
	// The hold reduces what can be spent, not what the ledger says is there.
	wantAvailable(t, l, 2, aed("50.00"))
	wantBalance(t, l, acc1, 2, 2, aed("250.00"))
}

func TestAuthorizationApprovedWhenAvailableLandsExactlyOnZero(t *testing.T) {
	l, _ := newLedger(t)
	mustCredit(t, l, "E1", aed("250.00"), 1, 1)

	got := authorize(t, l, "E3", "Auth-A", aed("250.00"), 1)

	if got.Status != StatusApproved {
		t.Errorf("status = %s, want APPROVED (available 0.00 is not negative)", got.Status)
	}
	wantAvailable(t, l, 1, aed("0.00"))
}

func TestAuthorizationDeclinedWhenAvailableWouldGoNegative(t *testing.T) {
	l, store := newLedger(t)
	mustCredit(t, l, "E1", aed("250.00"), 1, 1)

	got, err := l.Authorize(AuthRequest{EventID: "E3", Account: acc1, AuthID: "Auth-A", Amount: aed("250.01"), Booked: 1, Value: 1})

	if err != nil {
		t.Fatalf("a decline is an outcome, not an error; got %v", err)
	}
	if got.Status != StatusDeclined {
		t.Errorf("status = %s, want DECLINED", got.Status)
	}
	if !strings.Contains(got.Reason, "AED -0.01") {
		t.Errorf("reason %q should state the available balance the hold would have left (AED -0.01)", got.Reason)
	}
	wantAvailable(t, l, 1, aed("250.00")) // a declined request places no hold
	if n := len(store.Rejections()); n != 0 {
		t.Errorf("%d rejections recorded for a decline, want 0", n)
	}
}

func TestAuthorizationCountsExistingHolds(t *testing.T) {
	l, _ := newLedger(t)
	mustCredit(t, l, "E1", aed("250.00"), 1, 1)
	authorize(t, l, "E3", "Auth-A", aed("200.00"), 1)

	got := authorize(t, l, "E4", "Auth-B", aed("60.00"), 1)

	if got.Status != StatusDeclined {
		t.Errorf("Auth-B status = %s, want DECLINED (only 50.00 available)", got.Status)
	}
	wantAvailable(t, l, 1, aed("50.00"))
}

func TestAuthorizationSeesBackdatedDebit(t *testing.T) {
	l, _ := newLedger(t)
	mustCredit(t, l, "E1", aed("1,200.00"), 1, 1)
	mustDebit(t, l, "E2", aed("950.00"), 1, 1)
	mustCredit(t, l, "E4", aed("400.00"), 3, 3)
	mustDebit(t, l, "E5", aed("185.00"), 4, 4)
	mustDebit(t, l, "E7", aed("620.00"), 5, 2) // backdated: Day 5 ledger balance is now -155.00

	got := authorize(t, l, "E8", "Auth-B", aed("90.00"), 5)

	if got.Status != StatusDeclined {
		t.Errorf("Auth-B status = %s, want DECLINED", got.Status)
	}
	if !strings.Contains(got.Reason, "AED -245.00") {
		t.Errorf("reason %q should state AED -245.00", got.Reason)
	}
}

func TestSettlementDebitsTheLedgerAndReleasesTheWholeHold(t *testing.T) {
	l, _ := newLedger(t)
	mustCredit(t, l, "E1", aed("650.00"), 1, 1)
	authorize(t, l, "E3", "Auth-A", aed("200.00"), 2)
	wantAvailable(t, l, 2, aed("450.00"))

	entry, err := l.Settle(SettleRequest{EventID: "E5", Account: acc1, AuthID: "Auth-A", Amount: aed("185.00"), Booked: 4, Value: 4})

	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if entry.Kind != Settlement || entry.Amount != aed("-185.00") || entry.AuthID != "Auth-A" {
		t.Errorf("settlement entry = %+v, want a SETTLEMENT of AED -185.00 for Auth-A", entry)
	}
	wantBalance(t, l, acc1, 4, 4, aed("465.00"))
	// 200.00 was held, 185.00 left the account, the other 15.00 is spendable again.
	wantAvailable(t, l, 4, aed("465.00"))
	got := wantStatus(t, l, "Auth-A", StatusSettled)
	if got.Settled != aed("185.00") || got.SettledOn != 4 {
		t.Errorf("Auth-A settled %s on day %d, want AED 185.00 on day 4", got.Settled, got.SettledOn)
	}
}

func TestRejectedSettlementMovesNoFunds(t *testing.T) {
	tests := []struct {
		name   string
		authID string
		amount string
		want   error
	}{
		{"authorization never existed", "Auth-Z", "180.00", ErrUnknownAuthorization},
		{"authorization was declined", "Auth-Declined", "10.00", ErrAuthorizationNotActive},
		{"authorization already settled", "Auth-Settled", "10.00", ErrAuthorizationNotActive},
		{"amount above the hold", "Auth-Open", "100.01", ErrSettlementExceedsHold},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l, store := newLedger(t)
			mustCredit(t, l, "E1", aed("650.00"), 1, 1)
			authorize(t, l, "A1", "Auth-Declined", aed("9,999.00"), 1)
			authorize(t, l, "A2", "Auth-Settled", aed("50.00"), 1)
			authorize(t, l, "A3", "Auth-Open", aed("100.00"), 1)
			if _, err := l.Settle(SettleRequest{EventID: "S2", Account: acc1, AuthID: "Auth-Settled", Amount: aed("50.00"), Booked: 2, Value: 2}); err != nil {
				t.Fatalf("setup settle: %v", err)
			}
			entriesBefore := len(store.Entries(acc1))

			_, err := l.Settle(SettleRequest{EventID: "E6", Account: acc1, AuthID: tc.authID, Amount: aed(tc.amount), Booked: 4, Value: 4})

			if !errors.Is(err, tc.want) {
				t.Errorf("Settle error = %v, want %v", err, tc.want)
			}
			if n := len(store.Entries(acc1)); n != entriesBefore {
				t.Errorf("entries went from %d to %d on a rejected settlement", entriesBefore, n)
			}
			wantBalance(t, l, acc1, 4, 4, aed("600.00"))
			rejections := store.Rejections()
			if len(rejections) != 1 || rejections[0].EventID != "E6" || rejections[0].Booked != 4 {
				t.Errorf("rejections = %+v, want exactly one for E6 booked on day 4", rejections)
			}
		})
	}
}

func TestSettlementCannotUseAnotherAccountsAuthorization(t *testing.T) {
	l, _ := newLedger(t)
	mustCredit(t, l, "E1", aed("650.00"), 1, 1)
	authorize(t, l, "E3", "Auth-A", aed("200.00"), 2)

	_, err := l.Settle(SettleRequest{EventID: "X1", Account: acc2, AuthID: "Auth-A", Amount: bhd("1.000"), Booked: 2, Value: 2})

	if !errors.Is(err, ErrUnknownAuthorization) {
		t.Errorf("Settle error = %v, want ErrUnknownAuthorization", err)
	}
}

func TestDuplicateAuthorizationIDIsRejected(t *testing.T) {
	l, store := newLedger(t)
	mustCredit(t, l, "E1", aed("650.00"), 1, 1)
	authorize(t, l, "E3", "Auth-A", aed("200.00"), 2)

	_, err := l.Authorize(AuthRequest{EventID: "E3b", Account: acc1, AuthID: "Auth-A", Amount: aed("10.00"), Booked: 2, Value: 2})

	if !errors.Is(err, ErrDuplicateAuthorization) {
		t.Errorf("Authorize error = %v, want ErrDuplicateAuthorization", err)
	}
	wantAvailable(t, l, 2, aed("450.00"))
	if n := len(store.Rejections()); n != 1 {
		t.Errorf("%d rejections recorded, want 1", n)
	}
}

func TestAuthorizationsAreListedInRequestOrder(t *testing.T) {
	l, _ := newLedger(t)
	mustCredit(t, l, "E1", aed("250.00"), 1, 1)
	authorize(t, l, "E3", "Auth-A", aed("200.00"), 2)
	authorize(t, l, "E8", "Auth-B", aed("90.00"), 5)

	auths, err := l.Authorizations(acc1)
	if err != nil {
		t.Fatalf("Authorizations: %v", err)
	}
	if len(auths) != 2 || auths[0].AuthID != "Auth-A" || auths[1].AuthID != "Auth-B" {
		t.Fatalf("authorizations = %+v, want Auth-A then Auth-B", auths)
	}
	if auths[0].Status != StatusApproved || auths[1].Status != StatusDeclined {
		t.Errorf("statuses = %s, %s, want APPROVED, DECLINED", auths[0].Status, auths[1].Status)
	}
	if auths[0].Hold != aed("200.00") || auths[0].RequestedOn != 2 {
		t.Errorf("Auth-A = %+v, want hold AED 200.00 requested on day 2", auths[0])
	}
}
