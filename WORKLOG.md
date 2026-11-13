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
