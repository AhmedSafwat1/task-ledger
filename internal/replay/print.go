package replay

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"ledgercore/internal/ledger"
)

// Print writes the replay as text, one block per day. Every day prints the
// same sections in the same order; a section with nothing in it says "none".
func Print(w io.Writer, r *Result) error {
	var b strings.Builder
	events := 0
	for _, d := range r.Days {
		events += len(d.Events)
	}
	fmt.Fprintf(&b, "Ledger replay: %d events over Days 1 to %d\n", events, len(r.Days))
	b.WriteString("Events are applied on their booked day, in stream order within the day.\n")
	b.WriteString("A balance \"as known at end of Day N\" counts entries value-dated on or before\n")
	b.WriteString("the day in question and booked on or before Day N.\n")

	for _, d := range r.Days {
		printDay(&b, d, r.Config)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func printDay(b *strings.Builder, d DayReport, cfg ledger.Config) {
	fmt.Fprintf(b, "\n=== Day %d ===\n", d.Day)

	var rows [][]string
	for _, o := range d.Events {
		e := o.Event
		subject := e.Amount.String()
		if e.Type == Reversal {
			subject = "reverses " + e.Reverses
		}
		text := o.Text
		if o.Rejected {
			text = "REJECTED, see Errors"
		}
		rows = append(rows, []string{e.ID, string(e.Type), string(e.Account), subject, fmt.Sprintf("value Day %d", e.Value), text})
	}
	section(b, "Events applied", rows)

	rows = nil
	for _, a := range d.Accounts {
		row := []string{string(a.Account), a.Closing.String(), "available " + a.Available.String()}
		if a.BeforeFees != a.Closing {
			row = append(row, "before end-of-day run "+a.BeforeFees.String())
		}
		rows = append(rows, row)
	}
	section(b, fmt.Sprintf("Closing ledger balance (as known at end of Day %d)", d.Day), rows)

	rows = nil
	for _, a := range d.Accounts {
		for _, r := range a.Restated {
			row := []string{string(a.Account), fmt.Sprintf("Day %d", r.Day), "was " + r.Was.String(), "now " + r.Now.String()}
			if r.BeforeRun != r.Now {
				row = append(row, fmt.Sprintf("(%s before this end-of-day run)", r.BeforeRun))
			}
			rows = append(rows, row)
		}
	}
	if len(rows) > 0 {
		section(b, "Earlier days restated", rows)
	}

	rows = nil
	for _, a := range d.Accounts {
		for _, f := range a.Fees {
			why := fmt.Sprintf("day closed at %s before this fee", f.Balance)
			if f.Entry.Kind == ledger.FeeWaiver {
				why = fmt.Sprintf("day now closes at %s, no longer negative", f.Balance)
			}
			rows = append(rows, []string{string(a.Account), string(f.Entry.Kind), f.Entry.Amount.String(), fmt.Sprintf("for Day %d", f.Entry.FeeDay), why})
		}
	}
	section(b, "Fee assessments", rows)

	rows = nil
	for _, a := range d.Accounts {
		for _, line := range a.Accruals {
			acc := line.Accrual
			basis := "on " + acc.Basis.String()
			if line.HasPrevious {
				basis = fmt.Sprintf("basis %s -> %s", line.Previous, acc.Basis)
			}
			rows = append(rows, []string{
				string(a.Account), string(acc.Kind), fmt.Sprintf("for Day %d", acc.ForDay), acc.Amount.String(), basis,
				fmt.Sprintf("carry %d/%d", acc.CarryOut, cfg.InterestDenominator),
			})
		}
		if c := a.Capitalized; c != nil {
			rows = append(rows, []string{
				string(a.Account), "CAPITALIZED", fmt.Sprintf("value Day %d", c.Value), c.Amount.String(),
				fmt.Sprintf("sum of %d accrual lines", a.AccrualLines),
				fmt.Sprintf("carry %d/%d kept", a.Residual, cfg.InterestDenominator),
			})
		}
	}
	section(b, "Interest", rows)

	rows = nil
	for _, a := range d.Accounts {
		for _, x := range a.Authorizations {
			var detail string
			switch x.Status {
			case ledger.StatusApproved:
				detail = fmt.Sprintf("hold %s active since Day %d", x.Hold, x.RequestedOn)
			case ledger.StatusSettled:
				detail = fmt.Sprintf("hold %s released, %s settled on Day %d", x.Hold, x.Settled, x.SettledOn)
			case ledger.StatusDeclined:
				detail = fmt.Sprintf("hold %s refused on Day %d: %s", x.Hold, x.RequestedOn, x.Reason)
			}
			rows = append(rows, []string{string(a.Account), x.AuthID, string(x.Status), detail})
		}
	}
	section(b, "Authorizations", rows)

	rows = nil
	for _, a := range d.Accounts {
		for _, n := range a.Notices {
			rows = append(rows, []string{n})
		}
	}
	if len(rows) > 0 {
		section(b, "Notices", rows)
	}

	rows = nil
	for _, r := range d.Errors {
		rows = append(rows, []string{r.EventID, string(r.Account), "rejected: " + r.Reason})
	}
	section(b, "Errors", rows)
}

// section prints a titled, column-aligned block, or "none" when it is empty.
func section(b *strings.Builder, title string, rows [][]string) {
	fmt.Fprintf(b, "  %s\n", title)
	if len(rows) == 0 {
		b.WriteString("    none\n")
		return
	}
	tw := tabwriter.NewWriter(b, 0, 0, 2, ' ', 0)
	for _, row := range rows {
		fmt.Fprintf(tw, "    %s\n", strings.Join(row, "\t"))
	}
	tw.Flush()
}
