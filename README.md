# Ledger core

An in-memory, append-only account ledger in Go, standard library only. It
covers the brief: two accounts (ACC-001 in AED, ACC-002 in BHD), ten events
over six days, value-dated postings, authorization holds, overdraft fees that
are reassessed retroactively, daily interest, and a replay command. There is
no web layer, no database, no persistence and no UI.

## Run it

```
go test ./...          # the test suite: exactly ONE failure is expected, see below
go run ./cmd/replay    # the replay, Day 1 to Day 6
```

`go.mod` declares `go 1.23`, because the newest library feature used is
`slices.Sorted` from Go 1.23. The code was built and tested with go1.27.1
only; no older toolchain was tried.

### One test fails on purpose

`go test ./...` exits non-zero and reports exactly one failing test:

```
--- FAIL: TestKnownLimitation_NoAtomicCheckAndHold
```

This is expected. The test lives in
[internal/ledger/limitation_test.go](internal/ledger/limitation_test.go) and
its comment explains what it shows: two ledger instances sharing one store can
both approve a hold against the same funds, because the store interface has
no atomic check-and-hold. It is left failing so the limitation stays visible.
Every other test passes:

```
go test -race ./... -skip TestKnownLimitation
```

## Reading the replay output

The replay applies each event on its booked day (stream order within a day),
closes the day, and prints one block per day. Every block has the same
sections; an empty section prints `none`.

| Section | What it shows |
|---|---|
| Events applied | The events booked that day and what the ledger did with each. |
| Closing ledger balance | Per account, the ledger balance of that day as known at the end of that day, after the end-of-day run (fees, waivers, interest capitalization). `available` is that balance minus active holds. `before end-of-day run` appears when the run changed the balance: it is the figure after the day's events and before any fee. |
| Earlier days restated | Only printed when an earlier day's closing balance changed today. `was` is what was known at the previous close, `now` is what is known at this close, and the bracket is the figure before today's end-of-day run. On Day 5 the Day 2 line reads `AED -370.00 before this end-of-day run`: that is criterion 1. |
| Fee assessments | Fee and waiver entries appended by this close. `for Day N` is the day whose closing balance is the trigger, and also the value date of the entry. All of them are booked today. |
| Interest | Accrual lines appended by this close. `DAILY` is today's line. `ADJUSTMENT` corrects an earlier day whose balance has changed. `carry x/10000` is the rounding residual left for the next line, in ten-thousandths of a minor unit. `CAPITALIZED` is the single interest credit on Day 6. |
| Authorizations | Every authorization so far with its current state. |
| Notices | Only printed when something needs a human, such as a negative balance in a currency with no fee defined. Never printed for this stream. |
| Errors | Events the ledger refused that day. A refused event moves no funds. |

Negative amounts are printed with an ASCII minus, for example `AED -370.00`.

Output of `go run ./cmd/replay`:

```
Ledger replay: 10 events over Days 1 to 6
Events are applied on their booked day, in stream order within the day.
A balance "as known at end of Day N" counts entries value-dated on or before
the day in question and booked on or before Day N.

=== Day 1 ===
  Events applied
    E1  CREDIT  ACC-001  AED 1,200.00  value Day 1  posted
    E2  DEBIT   ACC-001  AED 950.00    value Day 1  posted
  Closing ledger balance (as known at end of Day 1)
    ACC-001  AED 250.00  available AED 250.00
    ACC-002  BHD 0.000   available BHD 0.000
  Fee assessments
    none
  Interest
    ACC-001  DAILY  for Day 1  AED 0.10   on AED 250.00  carry 0/10000
    ACC-002  DAILY  for Day 1  BHD 0.000  on BHD 0.000   carry 0/10000
  Authorizations
    none
  Errors
    none

=== Day 2 ===
  Events applied
    E3  AUTHORIZATION  ACC-001  AED 200.00  value Day 2  Auth-A APPROVED
  Closing ledger balance (as known at end of Day 2)
    ACC-001  AED 250.00  available AED 50.00
    ACC-002  BHD 0.000   available BHD 0.000
  Fee assessments
    none
  Interest
    ACC-001  DAILY  for Day 2  AED 0.10   on AED 250.00  carry 0/10000
    ACC-002  DAILY  for Day 2  BHD 0.000  on BHD 0.000   carry 0/10000
  Authorizations
    ACC-001  Auth-A  APPROVED  hold AED 200.00 active since Day 2
  Errors
    none

=== Day 3 ===
  Events applied
    E4  CREDIT  ACC-001  AED 400.00  value Day 3  posted
  Closing ledger balance (as known at end of Day 3)
    ACC-001  AED 650.00  available AED 450.00
    ACC-002  BHD 0.000   available BHD 0.000
  Fee assessments
    none
  Interest
    ACC-001  DAILY  for Day 3  AED 0.26   on AED 650.00  carry 0/10000
    ACC-002  DAILY  for Day 3  BHD 0.000  on BHD 0.000   carry 0/10000
  Authorizations
    ACC-001  Auth-A  APPROVED  hold AED 200.00 active since Day 2
  Errors
    none

=== Day 4 ===
  Events applied
    E5  SETTLEMENT  ACC-001  AED 185.00  value Day 4  Auth-A settled, hold released
    E6  SETTLEMENT  ACC-001  AED 180.00  value Day 4  REJECTED, see Errors
  Closing ledger balance (as known at end of Day 4)
    ACC-001  AED 465.00  available AED 465.00
    ACC-002  BHD 0.000   available BHD 0.000
  Fee assessments
    none
  Interest
    ACC-001  DAILY  for Day 4  AED 0.19   on AED 465.00  carry -4000/10000
    ACC-002  DAILY  for Day 4  BHD 0.000  on BHD 0.000   carry 0/10000
  Authorizations
    ACC-001  Auth-A  SETTLED  hold AED 200.00 released, AED 185.00 settled on Day 4
  Errors
    E6  ACC-001  rejected: unknown authorization: Auth-Z on ACC-001

=== Day 5 ===
  Events applied
    E7   DEBIT          ACC-001  AED 620.00  value Day 2  posted
    E8   AUTHORIZATION  ACC-001  AED 90.00   value Day 5  Auth-B DECLINED
    E10  CREDIT         ACC-002  BHD 10.000  value Day 5  posted as 3 instalments: BHD 3.333, BHD 3.333, BHD 3.334
  Closing ledger balance (as known at end of Day 5)
    ACC-001  AED -230.00  available AED -230.00  before end-of-day run AED -155.00
    ACC-002  BHD 10.000   available BHD 10.000
  Earlier days restated
    ACC-001  Day 2  was AED 250.00  now AED -395.00  (AED -370.00 before this end-of-day run)
    ACC-001  Day 3  was AED 650.00  now AED 5.00     (AED 30.00 before this end-of-day run)
    ACC-001  Day 4  was AED 465.00  now AED -205.00  (AED -155.00 before this end-of-day run)
  Fee assessments
    ACC-001  FEE  AED -25.00  for Day 2  day closed at AED -370.00 before this fee
    ACC-001  FEE  AED -25.00  for Day 4  day closed at AED -180.00 before this fee
    ACC-001  FEE  AED -25.00  for Day 5  day closed at AED -205.00 before this fee
  Interest
    ACC-001  ADJUSTMENT  for Day 2  AED -0.10  basis AED 250.00 -> AED -395.00  carry -4000/10000
    ACC-001  ADJUSTMENT  for Day 3  AED -0.26  basis AED 650.00 -> AED 5.00     carry -2000/10000
    ACC-001  ADJUSTMENT  for Day 4  AED -0.19  basis AED 465.00 -> AED -205.00  carry 2000/10000
    ACC-001  DAILY       for Day 5  AED 0.00   on AED -230.00                   carry 2000/10000
    ACC-002  DAILY       for Day 5  BHD 0.004  on BHD 10.000                    carry 0/10000
  Authorizations
    ACC-001  Auth-A  SETTLED   hold AED 200.00 released, AED 185.00 settled on Day 4
    ACC-001  Auth-B  DECLINED  hold AED 90.00 refused on Day 5: available balance would be AED -245.00 (available AED -155.00, requested AED 90.00)
  Errors
    none

=== Day 6 ===
  Events applied
    E9  REVERSAL  ACC-001  reverses E7  value Day 2  reversal of E7 posted: AED 620.00
  Closing ledger balance (as known at end of Day 6)
    ACC-001  AED 466.02  available AED 466.02  before end-of-day run AED 390.00
    ACC-002  BHD 10.008  available BHD 10.008  before end-of-day run BHD 10.000
  Earlier days restated
    ACC-001  Day 2  was AED -395.00  now AED 250.00  (AED 225.00 before this end-of-day run)
    ACC-001  Day 3  was AED 5.00     now AED 650.00  (AED 625.00 before this end-of-day run)
    ACC-001  Day 4  was AED -205.00  now AED 465.00  (AED 415.00 before this end-of-day run)
    ACC-001  Day 5  was AED -230.00  now AED 465.00  (AED 390.00 before this end-of-day run)
  Fee assessments
    ACC-001  FEE_WAIVER  AED 25.00  for Day 2  day now closes at AED 250.00, no longer negative
    ACC-001  FEE_WAIVER  AED 25.00  for Day 4  day now closes at AED 465.00, no longer negative
    ACC-001  FEE_WAIVER  AED 25.00  for Day 5  day now closes at AED 465.00, no longer negative
  Interest
    ACC-001  ADJUSTMENT   for Day 2    AED 0.10   basis AED -395.00 -> AED 250.00  carry 2000/10000
    ACC-001  ADJUSTMENT   for Day 3    AED 0.26   basis AED 5.00 -> AED 650.00     carry 0/10000
    ACC-001  ADJUSTMENT   for Day 4    AED 0.19   basis AED -205.00 -> AED 465.00  carry -4000/10000
    ACC-001  ADJUSTMENT   for Day 5    AED 0.18   basis AED -230.00 -> AED 465.00  carry 2000/10000
    ACC-001  DAILY        for Day 6    AED 0.19   on AED 465.00                    carry -2000/10000
    ACC-001  CAPITALIZED  value Day 6  AED 1.02   sum of 13 accrual lines          carry -2000/10000 kept
    ACC-002  DAILY        for Day 6    BHD 0.004  on BHD 10.000                    carry 0/10000
    ACC-002  CAPITALIZED  value Day 6  BHD 0.008  sum of 6 accrual lines           carry 0/10000 kept
  Authorizations
    ACC-001  Auth-A  SETTLED   hold AED 200.00 released, AED 185.00 settled on Day 4
    ACC-001  Auth-B  DECLINED  hold AED 90.00 refused on Day 5: available balance would be AED -245.00 (available AED -155.00, requested AED 90.00)
  Errors
    none
```

A test (`TestReadmeShowsTheCurrentOutput`) fails if the block above stops
matching what the command prints.

## Acceptance criteria

Four of the eight criteria are wrong. Each has one test in
[internal/replay/criteria_test.go](internal/replay/criteria_test.go); the
reasoning with worked numbers is in [REJECTED.md](REJECTED.md).

| # | Criterion | Verdict | Test |
|---|---|---|---|
| 1 | Day 2 closing, at end of Day 5 before fees, is AED -370.00 | Accepted | `TestCriterion1_Accepted_Day2AsOfDay5BeforeFeesIsMinus370` |
| 2 | E7 causes exactly one overdraft fee, on Day 2 | **Rejected**: three fees, for Days 2, 4 and 5 | `TestCriterion2_Rejected_E7CausesThreeFeesNotOne` |
| 3 | The Day 4 settlement of Auth-A is accepted | Accepted | `TestCriterion3_Accepted_AuthASettlementIsAccepted` |
| 4 | A settlement naming an unknown authorization is rejected, no funds leave | Accepted, ambiguity recorded | `TestCriterion4_Accepted_SettlementWithoutAuthorizationIsRejected` |
| 5 | If Auth-B is approved, its hold reduces available but not ledger balance | Accepted as a conditional; Auth-B is in fact declined | `TestCriterion5_Accepted_ApprovedHoldReducesAvailableNotLedger` |
| 6 | After E9, all balances and fees return to their pre-E7 values | **Rejected**: only the amount is neutralised | `TestCriterion6_Rejected_ReversalDoesNotRestoreEverything` |
| 7 | The three BHD instalments are each BHD 3.334 | **Rejected**: 3 x 3.334 = 10.002 | `TestCriterion7_Rejected_InstalmentsSumToTheAmountCredited` |
| 8 | If accruals do not sum to the capitalized total, discard the remainder | **Rejected**: contradicts rule 2 | `TestCriterion8_Rejected_ResidualIsCarriedNotDiscarded` |

## How it is built

- **Money** is an `int64` count of minor units plus a currency tag
  ([internal/money](internal/money/money.go)). No floating point; arithmetic
  across currencies returns an error.
- **Storage** is a small `Store` interface with append and read methods only,
  and one in-memory implementation
  ([internal/ledger/store.go](internal/ledger/store.go)). There is nothing to
  call that updates or deletes a record.
- **Balances are never stored.** `Balance(account, valueDay, bookedDay)` sums
  the entries value-dated on or before `valueDay` that were booked on or
  before `bookedDay`. That one query answers both "what is the balance" and
  "what did we think it was on Day 4".
- **End of day** ([internal/ledger/fees.go](internal/ledger/fees.go)) runs in
  a fixed order: reassess fees for every day up to today until nothing
  changes, then accrue interest and append adjustments for earlier days whose
  balance changed, then capitalize on Day 6.
- **Authorization state** is folded from append-only records each time it is
  asked for ([internal/ledger/auth.go](internal/ledger/auth.go)).

```
cmd/replay/              the replay command
internal/money/          Money, currencies, Split, half-even rounding
internal/ledger/         store, postings, balances, authorizations, fees,
                         reversal, instalments, interest
internal/replay/         the event stream, the day driver, the printer,
                         one test per acceptance criterion
```

## Documents

| File | Contents |
|---|---|
| [NUMBERS.md](NUMBERS.md) | Every constant, why that value, and why not half of it. |
| [AMBIGUITIES.md](AMBIGUITIES.md) | Every ambiguity found in the brief and how it was resolved. |
| [REJECTED.md](REJECTED.md) | The refused criteria with worked numbers, and approaches dropped during the build. |
| [ARCHITECTURE.md](ARCHITECTURE.md) | Scale, value dating in a UAE bank, the authorization lifecycle, and what was cut. |
| [WORKLOG.md](WORKLOG.md) | Timestamped log of the build. |

## Why Go

- The brief is standard library only, and Go's standard library is enough:
  `testing` with table-driven tests, a race detector, `text/tabwriter` for
  the report, and nothing else.
- Integer arithmetic is explicit. There is no implicit conversion between
  numeric types, so a float cannot slip into a balance; a test also fails if
  `float32` or `float64` appears in non-test code.
- Records are plain value structs, so "a read returns a copy" costs nothing
  and needs no defensive cloning.
- Interfaces are small and satisfied implicitly, which is what the
  append-only `Store` relies on: the rule is carried by the methods that do
  not exist.
- Errors are values. A refused event is an ordinary return value that the
  caller must look at, which suits a ledger where "rejected" is a normal
  outcome.

What Go does not give: there are no sum types, so an entry kind is a string
constant and the compiler does not check that a `switch` covers every kind;
and there is no decimal type, which is why amounts are minor units in an
`int64` and overflow is checked by hand.
