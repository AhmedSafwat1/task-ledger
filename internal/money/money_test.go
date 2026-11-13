package money

import (
	"errors"
	"math"
	"testing"
)

func TestParseAndString(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		cur       Currency
		wantMinor int64
		wantText  string
	}{
		{"AED with thousands separator", "1,200.00", AED, 120000, "AED 1,200.00"},
		{"AED negative", "-370.00", AED, -37000, "AED -370.00"},
		{"AED zero", "0.00", AED, 0, "AED 0.00"},
		{"AED whole units are padded", "5", AED, 500, "AED 5.00"},
		{"AED one decimal is padded", "5.5", AED, 550, "AED 5.50"},
		{"AED below one unit", "0.04", AED, 4, "AED 0.04"},
		{"AED negative below one unit", "-0.10", AED, -10, "AED -0.10"},
		{"BHD three decimals", "10.000", BHD, 10000, "BHD 10.000"},
		{"BHD instalment", "3.334", BHD, 3334, "BHD 3.334"},
		{"BHD millions", "1,234,567.891", BHD, 1234567891, "BHD 1,234,567.891"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.in, tc.cur)
			if err != nil {
				t.Fatalf("Parse(%q, %s) error: %v", tc.in, tc.cur.Code, err)
			}
			if got.Minor() != tc.wantMinor {
				t.Errorf("Parse(%q, %s) minor = %d, want %d", tc.in, tc.cur.Code, got.Minor(), tc.wantMinor)
			}
			if got.String() != tc.wantText {
				t.Errorf("String() = %q, want %q", got.String(), tc.wantText)
			}
		})
	}
}

func TestParseRejectsBadInput(t *testing.T) {
	tests := []struct {
		name string
		in   string
		cur  Currency
		want error
	}{
		{"more decimals than AED holds", "3.334", AED, ErrPrecision},
		{"more decimals than BHD holds", "10.0005", BHD, ErrPrecision},
		{"empty", "", AED, ErrSyntax},
		{"letters", "12a.00", AED, ErrSyntax},
		{"two decimal points", "1.2.3", AED, ErrSyntax},
		{"sign only", "-", AED, ErrSyntax},
		{"no digits before point", ".50", AED, ErrSyntax},
		{"too large for int64", "99999999999999999999.00", AED, ErrOverflow},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.in, tc.cur)
			if !errors.Is(err, tc.want) {
				t.Errorf("Parse(%q, %s) error = %v, want %v", tc.in, tc.cur.Code, err, tc.want)
			}
		})
	}
}

func TestAddAndSubSameCurrency(t *testing.T) {
	a := MustParse("1,200.00", AED)
	b := MustParse("950.00", AED)

	sum, err := a.Add(b)
	if err != nil {
		t.Fatalf("Add error: %v", err)
	}
	if sum.String() != "AED 2,150.00" {
		t.Errorf("Add = %s, want AED 2,150.00", sum)
	}

	diff, err := a.Sub(b)
	if err != nil {
		t.Fatalf("Sub error: %v", err)
	}
	if diff.String() != "AED 250.00" {
		t.Errorf("Sub = %s, want AED 250.00", diff)
	}
}

func TestCrossCurrencyArithmeticFails(t *testing.T) {
	aed := MustParse("25.00", AED)
	bhd := MustParse("25.000", BHD)

	if _, err := aed.Add(bhd); !errors.Is(err, ErrCurrencyMismatch) {
		t.Errorf("Add across currencies error = %v, want ErrCurrencyMismatch", err)
	}
	if _, err := aed.Sub(bhd); !errors.Is(err, ErrCurrencyMismatch) {
		t.Errorf("Sub across currencies error = %v, want ErrCurrencyMismatch", err)
	}
	if _, err := aed.Cmp(bhd); !errors.Is(err, ErrCurrencyMismatch) {
		t.Errorf("Cmp across currencies error = %v, want ErrCurrencyMismatch", err)
	}
}

func TestZeroValueMoneyHasNoCurrencyAndCannotBeAdded(t *testing.T) {
	var none Money
	if _, err := none.Add(MustParse("1.00", AED)); !errors.Is(err, ErrCurrencyMismatch) {
		t.Errorf("Add with zero-value Money error = %v, want ErrCurrencyMismatch", err)
	}
	if _, err := none.Add(none); !errors.Is(err, ErrCurrencyMismatch) {
		t.Errorf("Add of two zero-value Money error = %v, want ErrCurrencyMismatch", err)
	}
}

func TestOverflowIsAnErrorNotAWrap(t *testing.T) {
	maxAED := New(math.MaxInt64, AED)
	minAED := New(math.MinInt64, AED)
	one := New(1, AED)

	if _, err := maxAED.Add(one); !errors.Is(err, ErrOverflow) {
		t.Errorf("MaxInt64 + 1 error = %v, want ErrOverflow", err)
	}
	if _, err := minAED.Sub(one); !errors.Is(err, ErrOverflow) {
		t.Errorf("MinInt64 - 1 error = %v, want ErrOverflow", err)
	}
	if _, err := minAED.Neg(); !errors.Is(err, ErrOverflow) {
		t.Errorf("-MinInt64 error = %v, want ErrOverflow", err)
	}
}

func TestCmpAndSign(t *testing.T) {
	small := MustParse("1.00", AED)
	large := MustParse("2.00", AED)

	if got, _ := small.Cmp(large); got != -1 {
		t.Errorf("Cmp(1.00, 2.00) = %d, want -1", got)
	}
	if got, _ := large.Cmp(small); got != 1 {
		t.Errorf("Cmp(2.00, 1.00) = %d, want 1", got)
	}
	if got, _ := small.Cmp(small); got != 0 {
		t.Errorf("Cmp(1.00, 1.00) = %d, want 0", got)
	}
	if !MustParse("-0.01", AED).IsNegative() {
		t.Error("-0.01 should be negative")
	}
	if Zero(AED).IsNegative() {
		t.Error("zero is not negative")
	}
}

func TestSplitPutsRemainderOnLastPart(t *testing.T) {
	tests := []struct {
		name  string
		total Money
		n     int
		want  []string
	}{
		{"BHD 10.000 in three", MustParse("10.000", BHD), 3, []string{"BHD 3.333", "BHD 3.333", "BHD 3.334"}},
		{"divides evenly", MustParse("9.000", BHD), 3, []string{"BHD 3.000", "BHD 3.000", "BHD 3.000"}},
		{"single part", MustParse("10.000", BHD), 1, []string{"BHD 10.000"}},
		{"remainder larger than one unit", MustParse("0.10", AED), 4, []string{"AED 0.02", "AED 0.02", "AED 0.02", "AED 0.04"}},
		{"negative total", MustParse("-10.000", BHD), 3, []string{"BHD -3.333", "BHD -3.333", "BHD -3.334"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parts, err := tc.total.Split(tc.n)
			if err != nil {
				t.Fatalf("Split error: %v", err)
			}
			if len(parts) != len(tc.want) {
				t.Fatalf("Split returned %d parts, want %d", len(parts), len(tc.want))
			}
			sum := Zero(tc.total.Currency())
			for i, p := range parts {
				if p.String() != tc.want[i] {
					t.Errorf("part %d = %s, want %s", i+1, p, tc.want[i])
				}
				if sum, err = sum.Add(p); err != nil {
					t.Fatalf("summing parts: %v", err)
				}
			}
			if sum != tc.total {
				t.Errorf("parts sum to %s, want %s", sum, tc.total)
			}
		})
	}
}

func TestSplitRejectsNonPositiveCount(t *testing.T) {
	for _, n := range []int{0, -1} {
		if _, err := MustParse("10.000", BHD).Split(n); !errors.Is(err, ErrInvalidParts) {
			t.Errorf("Split(%d) error = %v, want ErrInvalidParts", n, err)
		}
	}
}

func TestDivRoundHalfEven(t *testing.T) {
	tests := []struct {
		name     string
		num, den int64
		want     int64
	}{
		{"exact", 100000, 10000, 10},
		{"above half rounds up", 186000, 10000, 19},
		{"below half rounds down", 182000, 10000, 18},
		{"tie to even from 0.5", 5000, 10000, 0},
		{"tie to even from 1.5", 15000, 10000, 2},
		{"tie to even from 2.5", 25000, 10000, 2},
		{"negative below half", -104000, 10000, -10},
		{"negative above half", -188000, 10000, -19},
		{"negative tie from -0.5", -5000, 10000, 0},
		{"negative tie from -1.5", -15000, 10000, -2},
		{"negative tie from -2.5", -25000, 10000, -2},
		{"zero", 0, 10000, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DivRoundHalfEven(tc.num, tc.den)
			if err != nil {
				t.Fatalf("DivRoundHalfEven error: %v", err)
			}
			if got != tc.want {
				t.Errorf("DivRoundHalfEven(%d, %d) = %d, want %d", tc.num, tc.den, got, tc.want)
			}
		})
	}
}

func TestDivRoundHalfEvenRejectsNonPositiveDivisor(t *testing.T) {
	for _, den := range []int64{0, -10000} {
		if _, err := DivRoundHalfEven(1, den); !errors.Is(err, ErrInvalidDivisor) {
			t.Errorf("DivRoundHalfEven(1, %d) error = %v, want ErrInvalidDivisor", den, err)
		}
	}
}
