# Numbers

Every constant in the code, where it lives, whether the brief gave it or I
chose it, why it has that value, and what would happen at half that value.

Amounts are written as the replay prints them (`AED -370.00`). "Fil" means one
minor unit: 1/100 of a dirham, 1/1000 of a Bahraini dinar.

## Given by the brief

These are not mine to choose. The "half" column says what halving would do,
which is mostly a way of showing what the value is load-bearing for.

| Constant | Value | Where | At half the value |
|---|---|---|---|
| Overdraft fee | AED 25.00 (2,500 fils) | `DefaultConfig().OverdraftFee` | AED 12.50 changes the amounts and nothing else: still three fees after E7. The value matters in the other direction. Day 3 closes at 30.00 before fees and 5.00 after the Day 2 fee; any fee above 30.00 would push Day 3 negative and make it four fees. |
| Interest rate | 0.04% per day, stored as 4 / 10,000 | `InterestNumerator`, `InterestDenominator` | 0.02% halves the interest; the rounding carry works the same way. |
| AED precision | 2 decimals | `money.AED` | Not meaningful to halve. One decimal could not hold the 0.10 accruals. |
| BHD precision | 3 decimals | `money.BHD` | With 2 decimals, BHD 10.000 in three would be 3.33, 3.33, 3.34, and the daily interest of 0.004 would be below one minor unit. |
| Capitalization day | Day 6 | `CapitalizationDay` | Day 3 would credit 0.46 (0.10 + 0.10 + 0.26) and the rest would never be credited inside the window, because only one capitalization day is configured. |
| Window | Days 1 to 6 | `replay.LastDay` | Days 4 to 6 would not be replayed: no E7, no fees, no reversal. |
| Instalments of E10 | 3 | `Stream()` | Not meaningful. Two instalments of 5.000 would have no remainder and criterion 7 would have no trap in it. |

## Chosen by me

### Rate denominator and carry resolution: 10,000

`InterestDenominator = 10_000`, with numerator 4.

Why this value: 0.04% is exactly 4/10,000. Ten thousand is the basis-point
scale, so the configured numerator reads directly as "4 basis points a day"
and any rate quoted in whole basis points fits without touching the
denominator.

The same number is the resolution of the rounding carry. Interest is computed
as `balance in fils x 4`, which is the exact interest in units of 1/10,000 of
a fil. Nothing is lost before the single rounding step, and what the rounding
leaves over is carried in those same units. There is no second, hidden
rounding.

Why not half (5,000): 0.04% is also 2/5,000, and the results would be
identical. But 5,000 cannot express an odd number of basis points (1 bp is
0.5/5,000), so the first rate change to, say, 0.03% would force a change of
denominator and of the meaning of every stored carry. Doubling it to 20,000
buys nothing until rates are quoted in half basis points.

Overflow: `balance x 4` must fit in `int64`. That allows balances up to about
2.3 x 10^18 fils. The multiplication is checked anyway and returns
`ErrOverflow` rather than wrapping.

### Rounding mode: half to even, applied once, with the residual carried

`money.DivRoundHalfEven` is the only rounding function in the code and
interest is the only place that rounds.

The mode decides less than it seems to. Because the residual is carried, the
recorded lines always satisfy

```
sum(recorded amounts) x 10,000 + carry = sum(exact interest)
```

whatever the mode. The mode only decides how far the recorded total may sit
from the exact total, and in whose favour:

| Mode | Recorded total vs exact | In this stream (exact AED 1.018) |
|---|---|---|
| Half to even (chosen) | Within half a fil, either side | AED 1.02, carry -0.2 fil |
| Half up | Within half a fil, either side | AED 1.02, carry -0.2 fil |
| Truncate | Up to one fil below, never above | AED 1.01, carry +0.8 fil |

Why half to even and not truncation: truncation is always in the bank's
favour. The customer is under-credited by up to a fil for the whole period,
every period. Nearest rounding halves the worst case and has no direction.

Why half to even and not half up: they differ only on exact ties, and a tie
always going up is a small, systematic transfer to the customer. Ties are not
exotic: any balance that is an odd multiple of 12.50 earns exactly half a fil
a day (12.50 x 0.04% = 0.005). Half to even sends 0.5 to 0 and 1.5 to 2, so
ties cancel over time. No line in this stream is an exact tie, so the printed
output is the same under both; `TestExactHalfRoundsToEven` covers the tie.

"Why not half it" for a mode: the nearest equivalent is rounding to half the
precision (to 0.5 of a fil). Rule 3 forbids that: amounts are stored to their
own precision.

### Where the instalment remainder goes: the last instalment

`Money.Split` gives every part the truncated quotient and puts the whole
remainder on the last part: BHD 10.000 in three is 3.333, 3.333, 3.334.

This was decided in the brief's design notes. The alternative, spreading the
remainder one fil at a time over the last parts, gives the same answer
whenever the remainder is a single fil, as it is here. They differ when the
remainder is two or more: AED 0.10 in four is 0.02, 0.02, 0.02, 0.04 here and
would be 0.02, 0.02, 0.03, 0.03 under spreading.

### Smallest instalment: 1 fil

An instalment credit is refused if any part would be zero (BHD 0.002 in
three). Half of one fil does not exist.

### Settlement tolerance above the hold: 0

A settlement is accepted for any amount up to the hold and refused above it.
Card schemes allow some overage for tips and currency movement, but the brief
gives no figure, and inventing a percentage would be inventing a rule. Zero is
the only value that needs no justification. Half of zero is zero.

### Hold lifetime: unlimited

There is no expiry constant because there is no expiry: an approved hold
stays until it is settled. This is a cut, not a judgement that holds should
live for ever. See ARCHITECTURE.md section 3.

### Overdraft fee in BHD: none

`OverdraftFee` has an entry for AED only. The brief defines the fee as "AED
25.00" and gives no BHD figure and no exchange rate. A BHD account that closes
negative is charged nothing and the end-of-day report carries a notice. I did
not use BHD 25.000 (same digits, roughly ten times the value) or convert at a
rate I would have had to make up.

### Fee reassessment pass limit: number of days closed, plus one

`reassessFees` repeats its pass over the days until a pass appends nothing,
and gives up with an error after `today + 1` passes.

Why this value: a fee only affects its own day and later days, and the days
are walked in ascending order, so one pass settles everything and a second
confirms it. The limit is a guard against a future rule change that breaks
that argument, not a tuning knob. It is tied to the number of days because a
pass that changes anything settles at least one more day.

Why not half: half the days could be fewer than two when the window is short
(Day 1 needs two passes: one to work, one to confirm), and the guard would
fire on a correct run.

### First day and first sequence number: 1

Day 0 means "nothing closed yet" and sequence 0 means "not stored yet", which
lets the zero value of both types mean "absent" without a separate flag.

### Integer width: 64 bits

`int64` minor units hold about 9.2 x 10^18 fils, or AED 92 quadrillion. Half
the width, `int32`, tops out at AED 21,474,836.47, which a single corporate
account exceeds.

### Go language version: 1.23

The lowest version that has everything the code uses (`slices.Sorted` and
`maps.Keys` as iterators). Tested with go1.27.1 only.

### Test sizes

`TestMemStoreConcurrentAppendsGetUniqueSequences` uses 8 goroutines of 200
appends. That is enough for the race detector to see overlapping appends and
finishes in milliseconds. Half of either still works; there is nothing
special about the values.

### Report layout

The printer pads columns with two spaces. It affects no number.

## Worked interest numbers

ACC-001, in fils. This is the final state of the six days (after E9), which
is also the whole history of the stream without E7.

| Day | Closing balance | Exact interest | Rounded on its own | With carry | Carry after |
|---|---|---|---|---|---|
| 1 | 250.00 | 10.0 | 10 | 10 | 0 |
| 2 | 250.00 | 10.0 | 10 | 10 | 0 |
| 3 | 650.00 | 26.0 | 26 | 26 | 0 |
| 4 | 465.00 | 18.6 | 19 | 19 | -0.4 |
| 5 | 465.00 | 18.6 | 19 | 18 | +0.2 |
| 6 | 465.00 | 18.6 | 19 | 19 | -0.2 |
| Total | | 101.8 | 103 | 102 | |

Rounded on its own, the days add up to 1.03 while the exact total rounds to
1.02: a fil appears from nowhere. With the carry, the days add up to 1.02 and
0.2 of a fil is owed back by the next line.

In the real stream ACC-001 reaches the same 1.02 by a longer road: 13 lines
instead of 6, because E7 and then E9 each force adjustments. They are printed
under `Interest` on Days 5 and 6 of the replay.

ACC-002: BHD 10.000 x 0.04% = 0.004 exactly, on Days 5 and 6. Capitalized
BHD 0.008, carry 0.
