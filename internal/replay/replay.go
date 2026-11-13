// Package replay drives the ledger with the event stream of the brief, one
// business day at a time, and collects what each day looked like when it
// closed. It holds no ledger logic: every figure in a report is read back
// from the ledger after the fact.
package replay

import (
	"fmt"
	"strings"

	"ledgercore/internal/ledger"
	"ledgercore/internal/money"
)

// EventType is the kind of an input event.
type EventType string

// Event types in the stream.
const (
	Credit        EventType = "CREDIT"
	Debit         EventType = "DEBIT"
	Authorization EventType = "AUTHORIZATION"
	Settlement    EventType = "SETTLEMENT"
	Reversal      EventType = "REVERSAL"
)

// Event is one line of the input stream.
type Event struct {
	ID          string
	Type        EventType
	Account     ledger.AccountID
	Amount      money.Money // not used by REVERSAL
	Booked      ledger.Day
	Value       ledger.Day
	AuthID      string // AUTHORIZATION, SETTLEMENT
	Reverses    string // REVERSAL: id of the event being reversed
	Instalments int    // CREDIT: number of instalments when more than one
}

// Account is an account to open before the replay starts.
type Account struct {
	ID       ledger.AccountID
	Currency money.Currency
}

// LastDay is the last day of the window.
const LastDay ledger.Day = 6

// Accounts returns the two accounts of the brief. Both open at zero.
func Accounts() []Account {
	return []Account{
		{ID: "ACC-001", Currency: money.AED},
		{ID: "ACC-002", Currency: money.BHD},
	}
}

// Stream returns the ten events in the order the brief lists them. That
// order is not booked-day order: E10 is booked on Day 5 but listed after E9,
// which is booked on Day 6.
func Stream() []Event {
	aed := func(s string) money.Money { return money.MustParse(s, money.AED) }
	bhd := func(s string) money.Money { return money.MustParse(s, money.BHD) }
	return []Event{
		{ID: "E1", Type: Credit, Account: "ACC-001", Amount: aed("1,200.00"), Booked: 1, Value: 1},
		{ID: "E2", Type: Debit, Account: "ACC-001", Amount: aed("950.00"), Booked: 1, Value: 1},
		{ID: "E3", Type: Authorization, Account: "ACC-001", Amount: aed("200.00"), Booked: 2, Value: 2, AuthID: "Auth-A"},
		{ID: "E4", Type: Credit, Account: "ACC-001", Amount: aed("400.00"), Booked: 3, Value: 3},
		{ID: "E5", Type: Settlement, Account: "ACC-001", Amount: aed("185.00"), Booked: 4, Value: 4, AuthID: "Auth-A"},
		{ID: "E6", Type: Settlement, Account: "ACC-001", Amount: aed("180.00"), Booked: 4, Value: 4, AuthID: "Auth-Z"},
		{ID: "E7", Type: Debit, Account: "ACC-001", Amount: aed("620.00"), Booked: 5, Value: 2},
		{ID: "E8", Type: Authorization, Account: "ACC-001", Amount: aed("90.00"), Booked: 5, Value: 5, AuthID: "Auth-B"},
		{ID: "E9", Type: Reversal, Account: "ACC-001", Booked: 6, Value: 2, Reverses: "E7"},
		{ID: "E10", Type: Credit, Account: "ACC-002", Amount: bhd("10.000"), Booked: 5, Value: 5, Instalments: 3},
	}
}

// Apply sends one event to the ledger and says in a few words what happened.
// A refused event comes back as an error; the ledger has already recorded it.
func Apply(l *ledger.Ledger, e Event) (string, error) {
	switch e.Type {
	case Credit:
		p := ledger.Posting{EventID: e.ID, Account: e.Account, Amount: e.Amount, Booked: e.Booked, Value: e.Value}
		if e.Instalments > 1 {
			entries, err := l.CreditInstalments(p, e.Instalments)
			if err != nil {
				return "", err
			}
			parts := make([]string, len(entries))
			for i, x := range entries {
				parts[i] = x.Amount.String()
			}
			return fmt.Sprintf("posted as %d instalments: %s", len(entries), strings.Join(parts, ", ")), nil
		}
		if _, err := l.Credit(p); err != nil {
			return "", err
		}
		return "posted", nil

	case Debit:
		p := ledger.Posting{EventID: e.ID, Account: e.Account, Amount: e.Amount, Booked: e.Booked, Value: e.Value}
		if _, err := l.Debit(p); err != nil {
			return "", err
		}
		return "posted", nil

	case Authorization:
		a, err := l.Authorize(ledger.AuthRequest{EventID: e.ID, Account: e.Account, AuthID: e.AuthID, Amount: e.Amount, Booked: e.Booked, Value: e.Value})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s %s", a.AuthID, a.Status), nil

	case Settlement:
		_, err := l.Settle(ledger.SettleRequest{EventID: e.ID, Account: e.Account, AuthID: e.AuthID, Amount: e.Amount, Booked: e.Booked, Value: e.Value})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s settled, hold released", e.AuthID), nil

	case Reversal:
		entries, err := l.Reverse(ledger.ReverseRequest{EventID: e.ID, Account: e.Account, Target: e.Reverses, Booked: e.Booked, Value: e.Value})
		if err != nil {
			return "", err
		}
		amounts := make([]string, len(entries))
		for i, x := range entries {
			amounts[i] = x.Amount.String()
		}
		return fmt.Sprintf("reversal of %s posted: %s", e.Reverses, strings.Join(amounts, ", ")), nil
	}
	return "", fmt.Errorf("event %s: unknown type %q", e.ID, e.Type)
}

// Result is a finished replay: the ledger and store it ran on, for further
// queries, and one report per day.
type Result struct {
	Ledger *ledger.Ledger
	Store  *ledger.MemStore
	Config ledger.Config
	Days   []DayReport
}

// Day returns the report of day d, or an empty report if d was not replayed.
func (r *Result) Day(d ledger.Day) DayReport {
	for _, report := range r.Days {
		if report.Day == d {
			return report
		}
	}
	return DayReport{}
}

// DayReport is the state of the ledger as known when the day closed.
type DayReport struct {
	Day      ledger.Day
	Events   []Outcome
	Accounts []AccountReport
	Errors   []ledger.Rejection // events refused on this day
}

// Account returns the section for one account, or an empty section.
func (d DayReport) Account(id ledger.AccountID) AccountReport {
	for _, a := range d.Accounts {
		if a.Account == id {
			return a
		}
	}
	return AccountReport{}
}

// Outcome is an event together with what the ledger did with it.
type Outcome struct {
	Event    Event
	Text     string
	Rejected bool
}

// AccountReport is one account at the close of a day.
type AccountReport struct {
	Account ledger.AccountID
	// Closing is the ledger balance of the day as known at its close: after
	// fees, waivers and any interest capitalization of that close.
	Closing money.Money
	// BeforeFees is the same balance just before the end-of-day run started.
	BeforeFees money.Money
	// Available is Closing minus the holds still active.
	Available money.Money
	// PreFeeSeq is the store sequence number at which BeforeFees was taken.
	PreFeeSeq uint64

	Restated       []Restatement // earlier days whose closing balance changed today
	Fees           []FeeLine
	Accruals       []AccrualLine
	Capitalized    *ledger.Entry
	AccrualLines   int   // lines in the accrual log so far
	Residual       int64 // rounding carry after the last line, in 1/denominator of a minor unit
	Authorizations []ledger.Authorization
	Notices        []string
}

// Restatement is an earlier day whose closing balance is different now from
// what it was when the previous day closed.
type Restatement struct {
	Day       ledger.Day
	Was       money.Money // as known at the previous close
	BeforeRun money.Money // now, before today's end-of-day run (before fees)
	Now       money.Money // now, after today's end-of-day run
}

// FeeLine is a fee or waiver entry with the balance that explains it: for a
// fee, the closing balance of its day just before the fee; for a waiver, the
// closing balance of its day once the waiver is applied.
type FeeLine struct {
	Entry   ledger.Entry
	Balance money.Money
}

// AccrualLine is an accrual record with, for adjustments, the basis the day
// had on its previous line.
type AccrualLine struct {
	Accrual     ledger.Accrual
	Previous    money.Money
	HasPrevious bool
}

// Run replays events over Days 1 to LastDay on a fresh ledger with the
// default schedule.
//
// Events are applied on their booked day, in stream order within the day,
// and each day is closed before the next begins. Grouping by booked day
// rather than following the listed order is what places E10 on Day 5; no
// event is dropped or re-dated.
func Run(events []Event) (*Result, error) {
	store := ledger.NewMemStore()
	cfg := ledger.DefaultConfig()
	l := ledger.New(store, cfg)
	for _, a := range Accounts() {
		if err := l.OpenAccount(a.ID, a.Currency); err != nil {
			return nil, err
		}
	}

	byDay := make(map[ledger.Day][]Event)
	for _, e := range events {
		if e.Booked < 1 || e.Booked > LastDay {
			return nil, fmt.Errorf("event %s is booked on day %d, outside days 1 to %d", e.ID, e.Booked, LastDay)
		}
		byDay[e.Booked] = append(byDay[e.Booked], e)
	}

	res := &Result{Ledger: l, Store: store, Config: cfg}
	for day := ledger.Day(1); day <= LastDay; day++ {
		report := DayReport{Day: day}
		for _, e := range byDay[day] {
			refusedBefore := len(store.Rejections())
			text, err := Apply(l, e)
			if err != nil {
				// A refusal the ledger recorded is a normal outcome of a
				// replay. Any other error means the run itself is broken.
				if len(store.Rejections()) == refusedBefore {
					return nil, fmt.Errorf("day %d: apply %s: %w", day, e.ID, err)
				}
				report.Events = append(report.Events, Outcome{Event: e, Text: "REJECTED", Rejected: true})
				continue
			}
			report.Events = append(report.Events, Outcome{Event: e, Text: text})
		}

		closed, err := l.CloseDay(day)
		if err != nil {
			return nil, err
		}
		for _, section := range closed.Accounts {
			a, err := accountReport(l, store, day, section)
			if err != nil {
				return nil, fmt.Errorf("day %d: report for %s: %w", day, section.Account, err)
			}
			report.Accounts = append(report.Accounts, a)
		}
		for _, r := range store.Rejections() {
			if r.Booked == day {
				report.Errors = append(report.Errors, r)
			}
		}
		res.Days = append(res.Days, report)
	}
	return res, nil
}

func accountReport(l *ledger.Ledger, store *ledger.MemStore, day ledger.Day, section ledger.AccountClose) (AccountReport, error) {
	id := section.Account
	a := AccountReport{
		Account:     id,
		PreFeeSeq:   section.PreFeeSeq,
		Capitalized: section.Capitalized,
		Notices:     section.Notices,
	}
	var err error
	if a.Closing, err = l.Balance(id, day, day); err != nil {
		return a, err
	}
	if a.BeforeFees, err = l.BalanceAtSeq(id, day, section.PreFeeSeq); err != nil {
		return a, err
	}
	if a.Available, err = l.Available(id, day); err != nil {
		return a, err
	}
	if a.Authorizations, err = l.Authorizations(id); err != nil {
		return a, err
	}

	for d := ledger.Day(1); d < day; d++ {
		was, err := l.Balance(id, d, day-1)
		if err != nil {
			return a, err
		}
		now, err := l.Balance(id, d, day)
		if err != nil {
			return a, err
		}
		if was == now {
			continue
		}
		before, err := l.BalanceAtSeq(id, d, section.PreFeeSeq)
		if err != nil {
			return a, err
		}
		a.Restated = append(a.Restated, Restatement{Day: d, Was: was, BeforeRun: before, Now: now})
	}

	for _, e := range section.Fees {
		seq := e.Seq // waiver: the day with the waiver applied
		if e.Kind == ledger.Fee {
			seq = e.Seq - 1 // fee: the day just before this fee
		}
		balance, err := l.BalanceAtSeq(id, e.FeeDay, seq)
		if err != nil {
			return a, err
		}
		a.Fees = append(a.Fees, FeeLine{Entry: e, Balance: balance})
	}

	log := store.Accruals(id)
	a.AccrualLines = len(log)
	if len(log) > 0 {
		a.Residual = log[len(log)-1].CarryOut
	}
	for _, acc := range section.Accruals {
		line := AccrualLine{Accrual: acc}
		for _, earlier := range log {
			if earlier.ForDay == acc.ForDay && earlier.Seq < acc.Seq {
				line.Previous, line.HasPrevious = earlier.Basis, true
			}
		}
		a.Accruals = append(a.Accruals, line)
	}
	return a, nil
}
