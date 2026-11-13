// Package money holds amounts as whole minor units tagged with their
// currency. There is no floating point anywhere in this package: an amount is
// an int64 count of fils, and every operation that could lose information
// (mixing currencies, overflowing, parsing more decimals than the currency
// has) returns an error instead of a rounded or wrapped result.
package money

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

// Errors callers can branch on with errors.Is.
var (
	ErrCurrencyMismatch = errors.New("currency mismatch")
	ErrOverflow         = errors.New("amount overflows int64 minor units")
	ErrPrecision        = errors.New("more decimal places than the currency holds")
	ErrSyntax           = errors.New("malformed amount")
	ErrInvalidParts     = errors.New("number of parts must be at least 1")
	ErrInvalidDivisor   = errors.New("divisor must be positive")
)

// Currency is an ISO 4217 code together with its number of decimal places
// (the minor-unit exponent).
type Currency struct {
	Code     string
	Exponent int
}

// The two currencies this ledger knows. AED has 2 decimal places (100 fils to
// the dirham); BHD has 3 (1000 fils to the dinar).
var (
	AED = Currency{Code: "AED", Exponent: 2}
	BHD = Currency{Code: "BHD", Exponent: 3}
)

// Money is an amount in minor units of one currency. The zero value has no
// currency and cannot take part in arithmetic.
type Money struct {
	minor int64
	cur   Currency
}

// New returns minor units of cur as Money.
func New(minor int64, cur Currency) Money {
	return Money{minor: minor, cur: cur}
}

// Zero returns a zero amount of cur.
func Zero(cur Currency) Money {
	return Money{cur: cur}
}

// Minor returns the amount in minor units.
func (m Money) Minor() int64 { return m.minor }

// Currency returns the currency of the amount.
func (m Money) Currency() Currency { return m.cur }

// IsNegative reports whether the amount is below zero.
func (m Money) IsNegative() bool { return m.minor < 0 }

// IsZero reports whether the amount is exactly zero.
func (m Money) IsZero() bool { return m.minor == 0 }

func (m Money) sameCurrency(o Money) error {
	if m.cur.Code == "" || o.cur.Code == "" || m.cur != o.cur {
		return fmt.Errorf("%w: %q and %q", ErrCurrencyMismatch, m.cur.Code, o.cur.Code)
	}
	return nil
}

// Add returns m + o. It fails if the currencies differ or the sum does not
// fit in int64.
func (m Money) Add(o Money) (Money, error) {
	if err := m.sameCurrency(o); err != nil {
		return Money{}, err
	}
	if (o.minor > 0 && m.minor > math.MaxInt64-o.minor) ||
		(o.minor < 0 && m.minor < math.MinInt64-o.minor) {
		return Money{}, fmt.Errorf("%w: %d + %d %s", ErrOverflow, m.minor, o.minor, m.cur.Code)
	}
	return Money{minor: m.minor + o.minor, cur: m.cur}, nil
}

// Sub returns m - o. It fails if the currencies differ or the difference does
// not fit in int64.
func (m Money) Sub(o Money) (Money, error) {
	if err := m.sameCurrency(o); err != nil {
		return Money{}, err
	}
	if (o.minor < 0 && m.minor > math.MaxInt64+o.minor) ||
		(o.minor > 0 && m.minor < math.MinInt64+o.minor) {
		return Money{}, fmt.Errorf("%w: %d - %d %s", ErrOverflow, m.minor, o.minor, m.cur.Code)
	}
	return Money{minor: m.minor - o.minor, cur: m.cur}, nil
}

// Neg returns -m. It fails only for the one value whose negation does not fit
// in int64.
func (m Money) Neg() (Money, error) {
	if m.minor == math.MinInt64 {
		return Money{}, fmt.Errorf("%w: negating %d %s", ErrOverflow, m.minor, m.cur.Code)
	}
	return Money{minor: -m.minor, cur: m.cur}, nil
}

// Cmp returns -1, 0 or +1 as m is less than, equal to or greater than o. It
// fails if the currencies differ.
func (m Money) Cmp(o Money) (int, error) {
	if err := m.sameCurrency(o); err != nil {
		return 0, err
	}
	switch {
	case m.minor < o.minor:
		return -1, nil
	case m.minor > o.minor:
		return 1, nil
	}
	return 0, nil
}

// Split divides m into n parts that sum exactly to m. Every part is the
// truncated quotient; the whole remainder goes on the last part.
func (m Money) Split(n int) ([]Money, error) {
	if n < 1 {
		return nil, fmt.Errorf("%w: got %d", ErrInvalidParts, n)
	}
	base := m.minor / int64(n)
	remainder := m.minor - base*int64(n)
	parts := make([]Money, n)
	for i := range parts {
		parts[i] = Money{minor: base, cur: m.cur}
	}
	parts[n-1].minor += remainder
	return parts, nil
}

// String formats the amount as "AED 1,200.00" or "BHD -3.334": currency code,
// thousands separators, and exactly as many decimals as the currency holds.
func (m Money) String() string {
	if m.cur.Code == "" {
		return "<no currency>"
	}
	sign := ""
	abs := uint64(m.minor)
	if m.minor < 0 {
		sign = "-"
		abs = uint64(-(m.minor + 1)) + 1 // safe for MinInt64
	}
	scale := pow10(m.cur.Exponent)
	whole := group(abs / scale)
	if m.cur.Exponent == 0 {
		return fmt.Sprintf("%s %s%s", m.cur.Code, sign, whole)
	}
	return fmt.Sprintf("%s %s%s.%0*d", m.cur.Code, sign, whole, m.cur.Exponent, abs%scale)
}

// Parse reads a decimal amount such as "1,200.00" or "-3.334" as cur. Commas
// in the whole part are ignored. Fewer decimals than the currency holds are
// padded with zeros; more are rejected with ErrPrecision, never rounded.
func Parse(s string, cur Currency) (Money, error) {
	text := s
	negative := strings.HasPrefix(text, "-")
	if negative {
		text = text[1:]
	}
	whole, frac, hasPoint := strings.Cut(text, ".")
	whole = strings.ReplaceAll(whole, ",", "")
	if whole == "" || !allDigits(whole) || (hasPoint && !allDigits(frac)) {
		return Money{}, fmt.Errorf("parse %q as %s: %w", s, cur.Code, ErrSyntax)
	}
	if len(frac) > cur.Exponent {
		return Money{}, fmt.Errorf("parse %q as %s (%d decimals): %w", s, cur.Code, cur.Exponent, ErrPrecision)
	}
	digits := whole + frac + strings.Repeat("0", cur.Exponent-len(frac))

	var minor int64
	for _, d := range digits {
		v := int64(d - '0')
		if minor > (math.MaxInt64-v)/10 {
			return Money{}, fmt.Errorf("parse %q as %s: %w", s, cur.Code, ErrOverflow)
		}
		minor = minor*10 + v
	}
	if negative {
		minor = -minor
	}
	return Money{minor: minor, cur: cur}, nil
}

// MustParse is Parse for literals in fixtures and tests. It panics on bad
// input, so it must never be given data from outside the program.
func MustParse(s string, cur Currency) Money {
	m, err := Parse(s, cur)
	if err != nil {
		panic(err)
	}
	return m
}

// DivRoundHalfEven returns num/den rounded to the nearest integer, with exact
// halves going to the even neighbour. den must be positive.
func DivRoundHalfEven(num, den int64) (int64, error) {
	if den <= 0 {
		return 0, fmt.Errorf("%w: got %d", ErrInvalidDivisor, den)
	}
	q := num / den
	r := num % den
	if r < 0 { // turn Go's truncated division into floor division: 0 <= r < den
		q--
		r += den
	}
	switch {
	case r > den-r:
		q++
	case r == den-r && q%2 != 0:
		q++
	}
	return q, nil
}

func allDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func pow10(exp int) uint64 {
	p := uint64(1)
	for range exp {
		p *= 10
	}
	return p
}

// group renders n with a comma between every three digits.
func group(n uint64) string {
	digits := fmt.Sprintf("%d", n)
	var b strings.Builder
	for i, c := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}
