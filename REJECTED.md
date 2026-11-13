# Rejected

Two parts: the acceptance criteria I refused, with the numbers that refuse
them, and the approaches that were dropped while building.

All figures are for ACC-001 in AED unless stated, written as the replay
prints them.

## Verdicts

| # | Verdict | In one line |
|---|---|---|
| 1 | Accepted | Day 2 at end of Day 5 before fees is -370.00. |
| 2 | **Rejected** | E7 causes three fees (Days 2, 4, 5), not one. |
| 3 | Accepted | Auth-A is approved and its settlement of 185.00 is within the 200.00 hold. |
| 4 | Accepted | E6 is refused and recorded; no funds move. The rules do not strictly require this, see AMBIGUITIES.md D1. |
| 5 | Accepted | True as a conditional. Auth-B itself is declined, because E7 comes first. |
| 6 | **Rejected** | A reversal neutralises an amount; it does not rewind the ledger. |
| 7 | **Rejected** | 3 x 3.334 = 10.002, not 10.000. |
| 8 | **Rejected** | Rule 2 requires an exact sum, so there is never a remainder to discard. |

Each criterion has a test of the same number in
`internal/replay/criteria_test.go`. For a rejected criterion the test asserts
the facts that contradict it.

## Criterion 2: "E7 causes exactly one overdraft fee to be assessed, on Day 2"

E7 is a debit of 620.00 booked on Day 5 and value-dated Day 2. It lowers the
closing balance of Day 2 and of every day after it.

| Day | As known on Day 4 | After E7, before fees | Negative? | Fee | After fees |
|---|---|---|---|---|---|
| 1 | 250.00 | 250.00 | no | | 250.00 |
| 2 | 250.00 | -370.00 | yes | 25.00 | -395.00 |
| 3 | 650.00 | 30.00 | no | | 5.00 |
| 4 | 465.00 | -155.00 | yes | 25.00 | -205.00 |
| 5 | | -155.00 | yes | 25.00 | -230.00 |

Rule 1 charges the fee "once per day per account when that day's closing
ledger balance (all entries with value_date <= that day) is negative". Days
2, 4 and 5 are three different days and each closes negative. Three fees.

The fees are assessed in day order and each is an entry, so each one sees the
ones before it. The balances that triggered them were -370.00 for Day 2,
-180.00 for Day 4 (after the Day 2 fee) and -205.00 for Day 5 (after both).

Day 3 is the trap inside the trap. It has +30.00 before fees. The Day 2 fee
takes it to +5.00, which is still not negative, so it owes nothing. A fee of
more than 30.00 would have made it four.

Without E7 there is no fee on any day, so all three are caused by E7.

The criterion does not survive any other reading of rule 1 either:

| Reading | Result | Matches "exactly one, on Day 2"? |
|---|---|---|
| Fee value-dated on the negative day (implemented) | Three fees: Days 2, 4, 5 | No |
| Fee value-dated on the day of discovery | Three fees, all dated Day 5 | No |
| No retroactive assessment at all, only the day being closed | One fee, for Day 5 | No: one fee, but not on Day 2 |
| Retroactive only for the backdated entry's own value date | Two fees: Day 2, and Day 5 which closes negative in its own right | No |

## Criterion 6: "After E9, all balances and fees return to their pre-E7 values"

E9 is booked on Day 6 and reverses E7 with value date Day 2. What it does is
append +620.00 pointing at E7. The two amounts cancel from Day 2 onward.
Everything else that happened because of E7 is a separate fact with its own
records.

**Straight after E9 the balances have not returned.** The three fees still
stand until the end of Day 6:

| Day | Before E7 | After E9, before the Day 6 close |
|---|---|---|
| 2 | 250.00 | 225.00 |
| 3 | 650.00 | 625.00 |
| 4 | 465.00 | 415.00 |
| 5 | | 390.00 |
| 6 | | 390.00 |

**The fees do not "return"; they are cancelled later, by new entries.** The
Day 6 close finds that Days 2, 4 and 5 no longer close negative and appends
three waivers of +25.00. That is a policy this implementation chose
(AMBIGUITIES.md C6), not a consequence of the reversal. The three fee entries
are still in the ledger and always will be.

**Compare the end of Day 6 with the world in which E7 never happened:**

| | Without E7 and E9 | Real stream |
|---|---|---|
| Closing ledger balance, Day 6 | 466.02 | 466.02 |
| Interest capitalized | 1.02 | 1.02 |
| Auth-B | APPROVED, 90.00 held | DECLINED |
| Available balance, Day 6 | 376.02 | 466.02 |
| Entries on the account | 5 | 13 |
| Fee entries / waiver entries | 0 / 0 | 3 / 3 |
| Accrual lines | 6 | 13 |
| Day 2 as known on Day 5 | 250.00 | -395.00 |

The first two rows are what is true in the criterion: the amounts net out.
The rest is what it gets wrong.

- Auth-B was declined on Day 5 because E7 had made the balance -155.00. The
  merchant was told no. E9 does not turn that into a yes, so the available
  balance is 90.00 higher than it would have been.
- The ledger has eight more entries: E7, three fees, E9, three waivers.
- The record of what was known on Day 5 still says Day 2 closed at -395.00.
  A statement produced on Day 5 would have shown that figure.

To satisfy the criterion literally the ledger would have to delete or edit
the fee entries, which rule 4 forbids, and re-decide Auth-B after the fact.

## Criterion 7: "The three BHD instalments in E10 must each be BHD 3.334"

```
3 x 3.334 = 10.002
```

E10 credits BHD 10.000. Three instalments of 3.334 would credit 10.002: two
fils that nobody paid in. Three of 3.333 would credit 9.999 and lose one.
10.000 has no equal three-way split at three decimals, so one instalment has
to differ:

```
3.333 + 3.333 + 3.334 = 10.000
```

The remainder goes on the last instalment.

## Criterion 8: "If the rounded daily interest accruals do not sum to the capitalized total, the remainder is discarded"

Rule 2 says: "The rounded daily accruals must sum exactly to the capitalized
total." A rule that says the sum must be exact leaves no case in which there
is a remainder to discard. The criterion describes handling for a situation
the rule prohibits.

It is also the wrong handling. Take the six days as they finally stand, in
fils:

| Day | Closing balance | Exact interest | Rounded on its own | With residual carried |
|---|---|---|---|---|
| 1 | 250.00 | 10.0 | 10 | 10 |
| 2 | 250.00 | 10.0 | 10 | 10 |
| 3 | 650.00 | 26.0 | 26 | 26 |
| 4 | 465.00 | 18.6 | 19 | 19 |
| 5 | 465.00 | 18.6 | 19 | 18 |
| 6 | 465.00 | 18.6 | 19 | 19 |
| Total | | 101.8 | 103 | 102 |

Rounding each day on its own gives lines that add up to 1.03, while the
exact total of 1.018 rounds to 1.02. "Discard the remainder" would mean
recording 1.03 of accruals and crediting 1.02: the accrual log and the ledger
would disagree by a fil for ever, and every account would have its own such
fil.

What the code does instead: the part of each day's interest that rounding
cuts off is carried into the next line before that line is rounded, and the
capitalized total is defined as the sum of the recorded lines. The lines add
up to the credit because the credit is their sum. After Day 6, 0.2 of a fil
has been credited ahead of being earned and sits in the carry (`carry
-2000/10000 kept` in the replay); the next line starts from it.

The real stream reaches the same AED 1.02 through 13 lines, because E7 and E9
each force adjustment lines. The invariant holds through all of them.

## Approaches dropped during the build

The status column says how far each one got. Only one was actually written
and then changed; the rest were dropped at the design stage of the step they
belong to, before any code for them existed.

| Approach | Status | Why dropped |
|---|---|---|
| A stored running balance per account, updated by each posting | Ruled out before the postings step. Never coded. | Backdating. E7 arrives on Day 5 with value date Day 2. A running balance has one number, and that number is "now"; there is nothing to subtract 620.00 from for Day 2, Day 3 or Day 4. Keeping one stored balance per day instead means E7 has to rewrite four of them, which is mutation, and still cannot answer "what did we believe on Day 4". Summing entries filtered by value day and booked day answers every one of these questions with no stored balance at all. |
| An `AUTH_SETTLED` event appended next to the settlement entry | Designed, not coded. Dropped while designing the authorization step. | Two appends for one fact. If the process stopped between them, the account would have a debit with the hold still active, or a released hold with no debit. The settlement entry carries the authorization id, and a hold counts as released exactly when such an entry exists: one append. |
| A field on each fee entry holding the balance that triggered it | Designed, not coded. | It is derivable: the day's balance at the sequence number just before the fee. Storing it would be storing a balance. |
| Charging and waiving fees at the moment of posting | Considered, not coded. | Rule 1 is about closing balances. Deciding at end of day keeps one place where fees are decided, and makes the decision independent of the order of events inside a day. |
| Fee value-dated on the day of discovery | Considered, not coded. | The design notes settle it the other way. Kept as the documented alternative in AMBIGUITIES.md C1. |
| Replaying in the listed order and re-dating E10 to Day 6 | Considered, not coded. | It overwrites a booked day the brief gives. AMBIGUITIES.md A1. |
| Rounding each day on its own and posting a balancing amount at capitalization | Considered, not coded. | The balancing amount is the "remainder" of criterion 8 under another name, and the daily lines would still not sum to the credit. |
| Persisting the "no BHD fee defined" notice as a record | Considered, not coded. | A notice describes a condition that still holds or does not. It is recomputed at every close, so it disappears when the balance is cured. |
| Using "an unsettled hold never expires" as the one failing test | Considered, not coded. | Real limitation, but it is a missing feature. The missing atomic check-and-hold is a property of the design, and it is the one that would cause damage first on a move to a database. Hold expiry is documented as a cut instead. |
| A test expecting the Day 1 accrual line to carry the store's last sequence number | Coded, then changed. | The test was wrong, not the code: ACC-002's accrual line is appended after ACC-001's in the same close, so the last sequence number belongs to ACC-002. The test now compares against the line the close reports. |
