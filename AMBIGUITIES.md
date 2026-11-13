# Ambiguities

Everything in the brief that can be read more than one way, the readings I
saw, and the one the code implements. Where a design note in the brief had
already settled the point, that is said. Each item names the test or file
that pins the behaviour.

Amounts are written as the replay prints them (`AED -370.00`).

## A. Time and ordering

**A1. E10 is listed after E9 but is booked a day earlier.**
E9 is booked Day 6; E10, listed after it, is booked Day 5. "Replayed in this
order" and "booked Day 5" cannot both be taken literally if days are closed
in sequence.

- Reading 1 (chosen): the booked day says which day an event belongs to; the
  listed order only orders events inside a day. The replay groups events by
  booked day, keeping stream order within the day, so Day 5 applies E7, E8,
  E10. E10 is on a different account from every other event, so moving it
  ahead of E9 changes nothing for ACC-001.
- Reading 2: follow the listed order and treat E10 as late news. It would
  have to be re-stamped as booked Day 6 with value date Day 5, because Day 5
  is already closed. The final numbers are the same (BHD 10.008), but the
  history differs: ACC-002 would show 0.000 as known on Day 5, and Day 5's
  interest of 0.004 would arrive as an adjustment on Day 6.

I chose reading 1 because it changes none of the given data. Reading 2
overwrites a booked day the brief states explicitly. The ledger itself refuses
an event booked into a closed day (`ErrDayClosed`), so following the listed
order literally fails loudly instead of quietly changing what was known on
Day 5. Tests: `TestE10IsAppliedOnItsBookedDay`,
`TestE10InLiteralStreamOrderIsRefusedByTheLedger`.

**A2. What "booked" means.**
I read it as the day the ledger learned of the event. That makes it the
second time axis: `Balance(account, valueDay, bookedDay)` answers "what was
the balance of day X as we knew it on day Y". Once a day is closed, nothing
more can be booked into it; late news is booked today and value-dated back,
which is exactly what E7 does. Test:
`TestPostingIntoAClosedBookedDayIsRejected`.

**A3. What a "day" is.**
The brief numbers days 1 to 6 and gives no dates. A `Day` is an integer.
There is no calendar, no time zone, no cut-off time, no weekend or holiday.
End of day is an explicit call (`CloseDay`), not a clock.

**A4. Events versus end of day.**
All of a day's events are applied first, then end of day runs. An
authorization is decided at the moment its event is applied, on what is known
then. E8 is decided after E7 and before Day 5's fees: ledger balance -155.00,
no holds, so the hold of 90.00 would leave -245.00. Deciding it after the fees
would give the same answer with a lower number.

**A5. Order inside end of day.**
Fees, then interest, then capitalization. Fees come first because a fee is
part of the closing balance that earns interest. Capitalization comes last
because it credits what the interest step recorded. File:
`internal/ledger/fees.go`, `CloseDay`.

## B. Balances

**B1. "Closing ledger balance" as known when?**
Rule 1 defines it as "all entries with value_date <= that day" and does not
say as of when. I always mean "as known at the moment of asking", and the
query takes the second date explicitly so both are available. The replay
prints each day as known at its own close, and prints a restatement when a
later day changes it.

**B2. "Evaluated at end of Day 5 and before any fee is assessed" (criterion 1).**
That is a point in time inside a day, which a booked-day filter cannot
express: fees assessed on Day 5 are also booked Day 5. Every record has a
store-wide sequence number, and `BalanceAtSeq` filters on it. The end-of-day
run reports the sequence number at which it started. Test:
`TestCriterion1_Accepted_Day2AsOfDay5BeforeFeesIsMinus370`.

**B3. Do holds count in the ledger balance?**
No. Rule 5 defines available balance as ledger balance minus active holds, so
they are different things. Fees and interest look at the ledger balance only.

**B4. Opening balance of 0.00.**
Read as "no opening entry". An account starts with no entries and its balance
sums to zero in its own currency.

**B5. Which ledger balance does an authorization check use?**
The balance of the request's value day as known now: every entry
value-dated on or before that day. A credit value-dated in the future is not
spendable yet. Test:
`TestAuthorizationIgnoresCreditsValueDatedInTheFuture`.

**B6. Can a debit overdraw?**
Rule 5 constrains authorizations only. A plain debit is posted without a
balance check; otherwise E7 could not happen and rule 1 would have nothing to
charge. Test: `TestDebitMayOverdraw`.

## C. Overdraft fees

**C1. Value date of a fee: "booked with value_date equal to the day assessed".**
"The day assessed" can mean the day the fee is for, or the day on which the
assessment runs. They differ only for retroactive fees.

- Reading 1 (chosen, and settled in the brief's design notes): the day whose
  closing balance was negative. The Day 2 fee found on Day 5 is value-dated
  Day 2 and booked Day 5.
- Reading 2: the discovery day. All three fees found on Day 5 would be
  value-dated Day 5.

With numbers, as known at the end of Day 5:

| Day | Before fees | After fees, reading 1 | After fees, reading 2 |
|---|---|---|---|
| 2 | -370.00 | -395.00 | -370.00 |
| 3 | 30.00 | 5.00 | 30.00 |
| 4 | -155.00 | -205.00 | -155.00 |
| 5 | -155.00 | -230.00 | -230.00 |

Both readings charge three fees and end Day 5 at -230.00. Reading 1 makes
the fee part of the day it belongs to, which is what lets a fee push a later
day negative; under reading 2 a retroactive fee can never affect the days in
between. Reading 1 is also the reason Day 3 is a near miss at 5.00.

**C2. Does a fee count towards its own trigger?**
If the fee is value-dated on its own day and the trigger were simply "closing
balance negative", a fee could keep itself alive: a day at +5.00 before fees
reads -20.00 with its fee on. The trigger is the day's closing balance
excluding that day's own fee and waiver. Fees of earlier days do count.
Test: `TestFeeDoesNotKeepItselfAlive`.

**C3. Can a fee cause another fee?**
Yes, settled in the design notes: a fee is an entry, so the Day 2 fee lowers
Days 3, 4 and 5. In this stream it takes Day 3 from 30.00 to 5.00, which is
still positive, so no fee. Tests: `TestFeeCanPushALaterDayNegative`,
`TestRetroactiveFeeCascadesThroughLaterDays`.

**C4. "Assessed once per day per account".**
Read as: at most one fee stands for a given account and day. Closing later
days never charges an earlier day twice. A fee that was waived can be charged
again if the day turns negative again; that is a new entry. Tests:
`TestFeeIsAssessedOncePerDayPerAccount`,
`TestWaivedFeeIsChargedAgainIfTheDayTurnsNegativeAgain`.

**C5. When are retroactive fees assessed?**
At the end of the day on which the backdated entry is booked, not at the
moment it is posted. Every end of day re-examines every earlier day. So E7's
three fees are all booked Day 5.

**C6. After E9, are the fees refunded?**
The rules do not say. Settled in the design notes: yes, by new compensating
entries. A fee whose day no longer closes negative is waived with a
`FEE_WAIVER` entry of +25.00, value-dated on the fee's day, pointing at the
fee. The fee entry stays.

The alternative was not to refund: the fees were correctly assessed on what
was known on Day 5, and a later correction does not make that assessment
wrong. I chose waiving because rule 1 ties the fee to a day's closing
balance, and after E9 that balance is positive for all three days; a fee left
standing would contradict rule 1 in the same way a missing fee did after E7.
The cost of this choice is that it does not ask why the reversal happened. A
bank might waive when the debit was its own error and keep the fee when the
customer caused it. Test: `TestReversalWaivesFeesWhoseTriggerNoLongerHolds`.

**C7. When are the waivers booked?**
At the end of Day 6, by the same reassessment that charges fees. Between E9
and that close, the three fees still stand and the balance is 390.00, not
465.00. The replay shows this as `before end-of-day run AED 390.00`.

**C8. Is a reversal value-dated later still a cure?**
Only from its value date. If E9 were value-dated Day 6 instead of Day 2,
Days 2, 4 and 5 would still close negative and all three fees would stand.
Test: `TestReversalValueDatedLaterLeavesEarlierFeesStanding`.

**C9. The fee is AED 25.00. What about a BHD account?**
No BHD amount and no exchange rate are given. Options: charge BHD 25.000
(same digits, about ten times the value), convert at an invented rate, or
charge nothing. I charge nothing and raise a notice in the end-of-day report
so the gap is visible. The fee schedule is keyed by currency and has an AED
entry only. ACC-002 never goes negative in this stream. Test:
`TestCurrencyWithoutAFeeDefinedGetsNoFeeAndANotice`.

**C10. Is a closing balance of exactly zero negative?**
No. Test: `TestNoFeeWhenClosingBalanceIsZeroOrPositive`.

**C11. Can a fee be refused for lack of funds?**
No. A fee posts unconditionally and deepens the overdraft, which may trigger
the next day's fee.

**C12. Fee and interest capitalization on the same day.**
The capitalization credit is value-dated on the capitalization day but is
appended after that day's fee decision. So that the decision does not flip
when the day is looked at again later, the fee trigger for a day ignores an
interest credit dated that same day. Test:
`TestCapitalizationDoesNotUndoTheSameDaysFee`.

## D. Authorizations and settlements

**D1. Criterion 4: must a settlement have a prior authorization?**
Nothing in rules 1 to 5 says so. Rule 5 says when an authorization is
approved; it does not say a debit needs one. Real card schemes have
settlements with no matching authorization: offline and below-floor-limit
transactions, force-posts, and clearing records that arrive after the
authorization has aged out. An issuer usually cannot simply refuse those.

The criterion is accepted as written, as decided in the brief: E6 names
Auth-Z, no such authorization exists on the account, the settlement is
refused, no entry is appended, a rejection record is kept, and the replay
prints it under `Errors`. In production this refusal would more likely be a
posting to an exception queue than a silent drop. Test:
`TestCriterion4_Accepted_SettlementWithoutAuthorizationIsRejected`.

**D2. Auth-A holds 200.00 and settles for 185.00.**
Does the other 15.00 stay held? I release the whole hold on settlement: one
settlement closes the authorization. The alternative is partial capture,
where the remainder stays held for a further settlement. The brief has a
single settlement per authorization and no later event for Auth-A. Test:
`TestSettlementDebitsTheLedgerAndReleasesTheWholeHold`.

**D3. Settlement for more than the hold.**
Not in the stream. I refuse it: the amount above the hold was never
authorized, which is the same principle as criterion 4. Real schemes allow
some overage; no tolerance is given, so the tolerance is zero. The
authorization stays approved with its hold active. Test:
`TestRejectedSettlementMovesNoFunds`.

**D4. Settlement when the balance has since gone negative.**
A settlement of an approved hold is accepted whatever the balance is now. The
hold was the promise. It can overdraw the account and lead to a fee. Test:
`TestSettlementIsHonouredAfterTheBalanceHasGoneNegative`.

**D5. Is a declined authorization an error?**
No. A decline is a decision the ledger made and recorded. It appears under
`Authorizations` as `DECLINED` with the reason, not under `Errors`. `Errors`
lists events the ledger could not accept at all.

**D6. Are earlier decisions revisited when history changes?**
No, in either direction. Auth-A was approved on Day 2 against 250.00; after
E7, Day 2 is known to have been -370.00, and Auth-A is not retroactively
declined. Auth-B was declined on Day 5 because of E7; after E9 it is not
retroactively approved. A decision is an event that happened. Test:
`TestReversalDoesNotReviveADeclinedAuthorization`.

**D7. "Auth-B is never settled inside the window."**
Since Auth-B is declined, there is nothing to settle and no hold to release.
In the stream without E7, Auth-B is approved and its hold of 90.00 stays
active to the end of the window and beyond: there is no expiry. See
ARCHITECTURE.md section 3.

**D8. Scope of an authorization id.**
Per account. A settlement on one account cannot use another account's
authorization, and a second request with an id already used on the account
is refused. Tests: `TestSettlementCannotUseAnotherAccountsAuthorization`,
`TestDuplicateAuthorizationIDIsRejected`.

**D9. The value date on an authorization.**
Used only to pick the ledger balance for the check (B5). A hold has no value
date effect: it is either active or not.

## E. Reversal

**E1. What does "reverses E7" do?**
It appends an equal and opposite entry that points at E7. That is all. Fees
are reconsidered by the next end of day (C6, C7); authorization decisions
are not reconsidered (D6); nothing is removed. This is the substance of
rejecting criterion 6.

**E2. The reversal's value date.**
E9 gives Day 2, the same as E7, so the two cancel from Day 2 on. I take the
value date from the event and do not force it to match the original (C8
shows what a different date does).

**E3. What can be reversed?**
Credits and debits, once. Reversing a reversal, a settlement, or a fee is
refused; a fee is undone only by a waiver. Reversing an event posted as
instalments reverses every instalment. Tests:
`TestRejectedReversalAppendsNothing`,
`TestReversingAnInstalmentEventReversesEveryInstalment`.

**E4. "Pre-E7 values" in criterion 6.**
Could mean the snapshot before E7 arrived, or the world in which E7 never
happened. Against either, something fails to return. See REJECTED.md.

## F. Instalments

**F1. "Three equal instalments" of BHD 10.000.**
10.000 does not divide by three at three decimals. Settled in the design
notes: 3.333, 3.333, 3.334, remainder on the last. See NUMBERS.md for the
alternative placement.

**F2. When is each instalment posted?**
The brief gives one booked day and one value date for E10. "Instalments"
usually suggests different dates, but none are given, so all three entries
are booked and value-dated Day 5.

**F3. One event or three?**
One event, three entries. Each carries the event id E10 and its part number
(1 of 3, 2 of 3, 3 of 3).

## G. Interest

**G1. Which accounts earn interest?**
Rule 2 names no currency, so both. ACC-002 earns BHD 0.004 a day once it
holds 10.000.

**G2. Interest on which balance?**
The closing ledger balance after that day's fees, as known when the line is
written. Not the available balance: holds do not reduce interest. Zero and
negative balances earn nothing and there is no debit interest.

**G3. Does accrued interest earn interest before Day 6?**
No. Accruals live in their own log and are not part of the ledger balance
until capitalized.

**G4. What happens to the rounding remainder?**
Rule 2 says the rounded accruals must sum exactly to the capitalized total.
Settled in the design notes: the residual is carried from line to line, and
the capitalized total is defined as the sum of the recorded lines. After
Day 6 the carry for ACC-001 is -0.2 of a fil: 1.02 was credited against an
exact 1.018. It is kept for the next line, not written off. See NUMBERS.md.

**G5. A past day's balance changes after its interest was recorded.**
Settled in the design notes: append an adjustment line; never edit. An
adjustment is written when the day's closing balance differs from the basis
on the latest line for that day. Its amount is the change in exact interest,
passed through the same carry. Test:
`TestBackdatedChangeAppendsAdjustmentsAndCapitalizationMatchesTheLog`.

**G6. "Capitalize as a single credit at end of Day 6".**
One `INTEREST` entry per account, booked and value-dated Day 6, appended
last in that day's close. If nothing accrued, no entry is written.

**G7. Does the capitalization credit earn interest on Day 6?**
It is value-dated Day 6, so it is in Day 6's closing ledger balance (466.02).
But it is the result of Day 6's interest, so it cannot also be its input.
Day 6 accrues on 465.00 and the credit earns from Day 7. Test:
`TestCapitalizedInterestStartsEarningTheNextDay`.

**G8. Day count.**
0.04% per day number, flat. No year basis, no actual/365.

**G9. Corrections after Day 6.**
An adjustment written after the capitalization day would wait for the next
capitalization. Only one capitalization day is configured, so inside this
model it would wait for ever. Outside the six-day window; not exercised.

## H. Money

**H1. "Amounts stored and rounded to their own precision".**
Stored: every amount is a whole number of that currency's minor units. There
is no shared scale. Rounded: only computed amounts are ever rounded, and the
only computation that needs it is interest. Input is never rounded: an amount
written with more decimals than the currency holds is refused. Test:
`TestParseRejectsBadInput`.

**H2. The minus sign.**
The brief writes negative amounts with a typographic minus. The output uses
an ASCII hyphen so it can be compared and pasted.

**H3. Mixing currencies.**
Any arithmetic or comparison across currencies is an error. There is no
exchange rate anywhere. Test: `TestCrossCurrencyArithmeticFails`.

## I. Output and errors

**I1. What goes under "errors"?**
Events the ledger refused: in this stream, E6 only. Declines are outcomes
(D5). A negative balance in a currency with no fee is a notice.

**I2. "Closing ledger balance" for a day in the output.**
As known at that day's close, after the whole end-of-day run. Day 6
therefore prints 466.02 for ACC-001, including the interest credit; the
465.00 before it is derivable from the `CAPITALIZED` line.

**I3. Scope of rule 4.**
"No event record is ever mutated or deleted" is applied to every log:
entries, authorization decisions, accrual lines, and rejections. A refused
event is itself a record.
