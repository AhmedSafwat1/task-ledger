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
