package ledger

import (
	"errors"
	"testing"

	"ledgercore/internal/money"
)

func TestCreditInstalmentsSumExactlyToTheTotal(t *testing.T) {
	l, store := newLedger(t)

	entries, err := l.CreditInstalments(Posting{EventID: "E10", Account: acc2, Amount: bhd("10.000"), Booked: 5, Value: 5}, 3)

	if err != nil {
		t.Fatalf("CreditInstalments: %v", err)
	}
	want := []string{"BHD 3.333", "BHD 3.333", "BHD 3.334"}
	if len(entries) != len(want) {
		t.Fatalf("%d instalments, want %d", len(entries), len(want))
	}
	sum := money.Zero(money.BHD)
	for i, e := range entries {
		if e.Amount.String() != want[i] {
			t.Errorf("instalment %d = %s, want %s", i+1, e.Amount, want[i])
		}
		if e.Kind != Credit || e.EventID != "E10" || e.Part != i+1 || e.Parts != 3 || e.Booked != 5 || e.Value != 5 {
			t.Errorf("instalment %d = %+v, want CREDIT E10 part %d of 3, booked and value day 5", i+1, e, i+1)
		}
		if sum, err = sum.Add(e.Amount); err != nil {
			t.Fatalf("sum: %v", err)
		}
	}
	if sum != bhd("10.000") {
		t.Errorf("instalments sum to %s, want BHD 10.000", sum)
	}
	wantBalance(t, l, acc2, 5, 5, bhd("10.000"))
	if n := len(store.Entries(acc2)); n != 3 {
		t.Errorf("ACC-002 has %d entries, want 3", n)
	}
}

func TestCreditInstalmentsEvenSplit(t *testing.T) {
	l, _ := newLedger(t)

	entries, err := l.CreditInstalments(Posting{EventID: "E1", Account: acc2, Amount: bhd("9.000"), Booked: 1, Value: 1}, 3)

	if err != nil {
		t.Fatalf("CreditInstalments: %v", err)
	}
	for i, e := range entries {
		if e.Amount != bhd("3.000") {
			t.Errorf("instalment %d = %s, want BHD 3.000", i+1, e.Amount)
		}
	}
}

func TestRejectedInstalmentsAppendNothing(t *testing.T) {
	tests := []struct {
		name   string
		amount money.Money
		parts  int
		want   error
	}{
		{"zero parts", bhd("10.000"), 0, money.ErrInvalidParts},
		{"an instalment would be zero", bhd("0.002"), 3, ErrInvalidAmount},
		{"amount in another currency", aed("10.00"), 3, money.ErrCurrencyMismatch},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l, store := newLedger(t)

			_, err := l.CreditInstalments(Posting{EventID: "E10", Account: acc2, Amount: tc.amount, Booked: 5, Value: 5}, tc.parts)

			if !errors.Is(err, tc.want) {
				t.Errorf("CreditInstalments error = %v, want %v", err, tc.want)
			}
			if n := len(store.Entries(acc2)); n != 0 {
				t.Errorf("%d entries appended for a rejected instalment credit, want 0", n)
			}
			if r := store.Rejections(); len(r) != 1 || r[0].EventID != "E10" {
				t.Errorf("rejections = %+v, want one for E10", r)
			}
		})
	}
}

func TestReversingAnInstalmentEventReversesEveryInstalment(t *testing.T) {
	l, _ := newLedger(t)
	if _, err := l.CreditInstalments(Posting{EventID: "E10", Account: acc2, Amount: bhd("10.000"), Booked: 5, Value: 5}, 3); err != nil {
		t.Fatalf("CreditInstalments: %v", err)
	}

	reversals, err := l.Reverse(ReverseRequest{EventID: "R10", Account: acc2, Target: "E10", Booked: 6, Value: 5})

	if err != nil {
		t.Fatalf("Reverse: %v", err)
	}
	if len(reversals) != 3 {
		t.Fatalf("%d reversal entries, want 3", len(reversals))
	}
	wantBalance(t, l, acc2, 5, 6, bhd("0.000"))
	wantBalance(t, l, acc2, 5, 5, bhd("10.000"))
}
