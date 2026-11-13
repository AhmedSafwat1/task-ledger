# Worklog

Each heading below is the unmodified output of `date`, captured by the shell at
the moment the entry was appended, immediately before the commit for that step.
No timestamp is typed by hand, edited afterwards, or backfilled. Entries are
only ever appended.

## Fri Nov 13 17:38:20 EET 2026

Step 1. `git init`, Go module `ledgercore` (go 1.27.1, standard library only), and this worklog. Before writing code I derived the expected numbers by hand: ACC-001 closes 250.00, 250.00, 650.00, 465.00 on Days 1 to 4 as known on the day; Day 2 as known at end of Day 5 before fees is -370.00; Auth-A is approved then settled; Auth-B is declined. These agree with the sanity checkpoints in the brief.

## Fri Nov 13 17:41:20 EET 2026

Step 2. Money type: int64 minor units plus a currency tag (AED 2 decimals, BHD 3). Tests were written first and failed to build, then passed. Add, Sub and Cmp return an error across currencies; overflow is an error, not a wrap; Parse rejects more decimals than the currency holds instead of rounding; Split puts the whole remainder on the last part (10.000 BHD in three gives 3.333, 3.333, 3.334); DivRoundHalfEven is the single rounding primitive. `go vet` clean, `go test -race ./...` ok.

## Fri Nov 13 17:44:20 EET 2026

Step 3. Store interface with only Append and read methods, and MemStore as its single implementation. Record types: Entry, AuthEvent, Accrual, Rejection. One store-wide sequence number orders every record. Tests written first (build failure), then green: reads are copies, a record that already has a sequence number cannot be appended again, a reflection test fails if a non-append, non-read method is ever added to Store, and 8 goroutines appending under `-race` get gapless ordered sequence numbers. No database implementation, by decision.

## Fri Nov 13 17:47:20 EET 2026

Step 4. Credit and Debit postings, and Balance(account, value day, booked day) computed by summing entries; no balance is stored anywhere. BalanceAtSeq gives the same view bounded by sequence number, needed later for the before-fees figure. Tests first (build failure), then green. Checked by hand against the brief: Day 1 250.00, Day 2 250.00, Day 3 650.00 as known on the day; after the backdated E7, Day 2 is 250.00 as known on Day 4 and -370.00 as known on Day 5, Day 3 becomes 30.00. A refused posting appends a Rejection record and no entry. Debits are not balance-checked: the overdraft rule needs accounts to be able to go negative.

## Fri Nov 13 17:50:20 EET 2026

Step 5. Authorize approves only if ledger balance minus active holds stays at or above zero after the new hold; a decline is a recorded outcome, not an error. Settle debits the ledger and releases the whole hold in a single append (the Settlement entry carries the AuthID, so debit and release cannot come apart). Authorization state is folded from auth events and settlement entries; nothing is updated in place. Tests first, then green: Auth-A (200.00 against 250.00) approved with available 50.00 and ledger still 250.00; settling 185.00 of the 200.00 hold returns the other 15.00 to available; Auth-B after the backdated E7 is declined with available -245.00; settlement for unknown Auth-Z, a declined auth, an already settled auth, or above the hold is refused, appends no entry and records a Rejection.

## Fri Nov 13 17:53:20 EET 2026

Step 6. CloseDay runs end of day; reassessFees looks at every day up to today, not only today, and appends a fee for each day whose closing balance is negative and has no fee yet. A fee is value-dated on the day it is for, and is itself an entry, so it can push a later day negative; the loop repeats until a pass appends nothing. Tests first, then green, and the numbers match my hand derivation: after E7 the before-fee closings are Day 2 -370.00, Day 3 30.00, Day 4 -155.00, Day 5 -155.00; fees land on Days 2, 4 and 5 (three, not one); Day 3 stays at 5.00 after the Day 2 fee and gets none; after fees Day 2 -395.00, Day 4 -205.00, Day 5 -230.00. Decisions taken here: the fee trigger excludes the day's own fee; BHD has no fee defined, so a negative BHD day gets a notice and no charge; a posting booked on an already closed day is refused (ErrDayClosed) so as-known-on-day-Y views cannot change later.
