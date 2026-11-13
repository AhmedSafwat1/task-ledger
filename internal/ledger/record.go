package ledger

import "ledgercore/internal/money"

// Day is a business day number. Day 1 is the first day of the window. The
// ledger has no calendar, time zone or cut-off time: a day is just an ordinal.
type Day int

// AccountID identifies an account, for example "ACC-001".
type AccountID string

// EntryKind says why an entry exists. Every kind moves the ledger balance.
type EntryKind string

// Entry kinds.
const (
	Credit     EntryKind = "CREDIT"
	Debit      EntryKind = "DEBIT"
	Settlement EntryKind = "SETTLEMENT" // debit that also releases the hold of AuthID
	Reversal   EntryKind = "REVERSAL"   // equal and opposite to the entry in Reverses
	Fee        EntryKind = "FEE"        // overdraft fee for FeeDay
	FeeWaiver  EntryKind = "FEE_WAIVER" // compensates the fee in Reverses
	Interest   EntryKind = "INTEREST"   // capitalization of recorded accruals
)

// Entry is one posting on an account. Entries are the only thing a ledger
// balance is made of: a balance is always the sum of the Amounts of the
// entries selected by value day and booked day, never a stored number.
type Entry struct {
	Seq      uint64      // store-wide append order, assigned by the store
	Account  AccountID   //
	Kind     EntryKind   //
	Amount   money.Money // signed: positive raises the ledger balance
	Booked   Day         // the day the ledger learned of the entry
	Value    Day         // the day from which the entry counts in the balance
	EventID  string      // source event ("E7"); empty for entries the ledger generates
	AuthID   string      // Settlement: the authorization being settled
	Reverses uint64      // Reversal: Seq of the reversed entry. FeeWaiver: Seq of the waived fee
	FeeDay   Day         // Fee, FeeWaiver: the day whose closing balance is the trigger
	Part     int         // instalment number, 1-based; zero when not an instalment
	Parts    int         // number of instalments; zero when not an instalment
}

// AuthEventKind is the outcome of an authorization request.
type AuthEventKind string

// Authorization outcomes. Settlement is not an auth event: a hold is released
// by the Settlement entry itself, so the debit and the release are one append.
const (
	AuthApproved AuthEventKind = "APPROVED"
	AuthDeclined AuthEventKind = "DECLINED"
)

// AuthEvent records the decision on one authorization request. An approved
// request places a hold; holds never touch the ledger balance.
type AuthEvent struct {
	Seq     uint64
	Account AccountID
	AuthID  string
	Kind    AuthEventKind
	Hold    money.Money // amount requested, positive
	Booked  Day
	Value   Day
	EventID string
	Reason  string // why a request was declined
}

// AccrualKind distinguishes the day's own accrual from a later correction.
type AccrualKind string

// Accrual kinds.
const (
	AccrualDaily      AccrualKind = "DAILY"      // interest for ForDay, written when ForDay closed
	AccrualAdjustment AccrualKind = "ADJUSTMENT" // correction for ForDay, written on a later day
)

// Accrual is one line of the interest accrual log. The log is separate from
// the entries: accrued interest is not part of the ledger balance until it is
// capitalized by an Interest entry.
type Accrual struct {
	Seq      uint64
	Account  AccountID
	Kind     AccrualKind
	ForDay   Day         // the day whose closing balance earned this interest
	Booked   Day         // the day this line was written
	Basis    money.Money // closing balance of ForDay as known when the line was written
	Amount   money.Money // rounded interest; negative for a downward adjustment
	CarryIn  int64       // rounding residual brought in, in 1/RateDenominator of a minor unit
	CarryOut int64       // rounding residual left for the next line
}

// Rejection records an event the ledger refused. A refused event moves no
// funds; the record exists so the refusal itself is part of the history.
type Rejection struct {
	Seq     uint64
	Account AccountID
	EventID string
	Booked  Day
	Reason  string
}
