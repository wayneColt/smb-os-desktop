package agents

import (
	"fmt"
	"sort"
	"strings"

	"github.com/wayneColt/smb-os-desktop/internal/money"
	"github.com/wayneColt/smb-os-desktop/internal/pack"
)

// Reminder stages by lateness.
const (
	StageFriendly = "friendly" // 15–30 days
	StageFirm     = "firm"     // 31–60 days
	StageFinal    = "final"    // 61+ days
)

// Draft is a saved, unsent reminder note.
type Draft struct {
	ID           string      `json:"id"`
	Invoice      string      `json:"invoice"`
	Customer     string      `json:"customer"`
	Location     string      `json:"location"`
	LocationName string      `json:"location_name"`
	Amount       money.Cents `json:"amount"`
	Due          string      `json:"due"`
	DaysLate     int         `json:"days_late"`
	Stage        string      `json:"stage"`
	Subject      string      `json:"subject"`
	Body         string      `json:"body"`
}

// Drafting is the ar-reminders output.
type Drafting struct {
	OverDays    int         `json:"over_days"`
	Drafts      []Draft     `json:"drafts"`
	Total       money.Cents `json:"total"`
	Skipped     []string    `json:"skipped"`      // open invoices not late enough
	AlreadySent []string    `json:"already_sent"` // reminded already; not drafted again
	Summary     string      `json:"summary"`
}

// RunARReminders drafts a reminder for every invoice more than overDays late,
// most overdue first, leaving out invoices that were already reminded.
func RunARReminders(p *pack.Pack, overDays int, sent map[string]bool) Drafting {
	out := Drafting{OverDays: overDays, Drafts: []Draft{}, Skipped: []string{}, AlreadySent: []string{}}
	invs := append([]pack.Invoice(nil), p.AR...)
	sort.SliceStable(invs, func(i, j int) bool {
		di, dj := p.DaysLate(invs[i]), p.DaysLate(invs[j])
		if di != dj {
			return di > dj
		}
		return invs[i].ID < invs[j].ID
	})
	for _, inv := range invs {
		late := p.DaysLate(inv)
		if late <= overDays {
			out.Skipped = append(out.Skipped, inv.ID)
			continue
		}
		if sent[inv.ID] {
			out.AlreadySent = append(out.AlreadySent, inv.ID)
			continue
		}
		d := draftFor(p, inv, late)
		out.Drafts = append(out.Drafts, d)
		out.Total += d.Amount
	}
	switch {
	case len(out.Drafts) == 0 && len(out.AlreadySent) > 0:
		out.Summary = fmt.Sprintf("Every invoice more than %d days late was already reminded (%s); no new drafts.",
			overDays, strings.Join(out.AlreadySent, ", "))
	case len(out.Drafts) == 0:
		out.Summary = fmt.Sprintf("No invoices are more than %d days late; no drafts saved.", overDays)
	default:
		out.Summary = fmt.Sprintf("%s saved for %s more than %d days late (%s). Nothing has been sent.",
			plural(len(out.Drafts), "reminder draft", "reminder drafts"), plural(len(out.Drafts), "invoice", "invoices"),
			overDays, out.Total.String())
	}
	return out
}

func draftFor(p *pack.Pack, inv pack.Invoice, late int) Draft {
	loc := p.LocationName(inv.Location)
	stage := StageFriendly
	switch {
	case late > 60:
		stage = StageFinal
	case late > 30:
		stage = StageFirm
	}
	to := inv.Contact
	if to == "" {
		to = inv.Customer
	}
	by := p.AsOfDate().AddDate(0, 0, 7).Format("Monday, Jan 2")
	var subject, ask string
	switch stage {
	case StageFriendly:
		subject = fmt.Sprintf("Friendly reminder: invoice %s", inv.ID)
		ask = "If you've already sent payment, thank you, and please disregard this note. Otherwise you can pay by replying to this message or by calling the " + loc + " office."
	case StageFirm:
		subject = fmt.Sprintf("Second reminder: invoice %s is %d days past due", inv.ID, late)
		ask = fmt.Sprintf("Please arrange payment by %s, or tell us if there's a question about the charges so we can sort it out.", by)
	default:
		subject = fmt.Sprintf("Final reminder: invoice %s is %d days past due", inv.ID, late)
		ask = fmt.Sprintf("We've sent earlier reminders and the balance is still open. Please contact us by %s to arrange payment or a payment plan, so we can keep the account in good standing.", by)
	}
	body := strings.Join([]string{
		"Hello " + to + ",",
		"",
		fmt.Sprintf("Our records show invoice %s for %s, due %s, is %d days past due. %s", inv.ID, inv.Amount.String(), longDate(inv.Due), late, ask),
		"",
		"Thank you,",
		fmt.Sprintf("The %s team, %s", loc, p.Brand),
	}, "\n")
	return Draft{ID: "d-" + inv.ID, Invoice: inv.ID, Customer: inv.Customer, Location: inv.Location, LocationName: loc,
		Amount: inv.Amount, Due: inv.Due, DaysLate: late, Stage: stage, Subject: subject, Body: body}
}

// ReminderMessage is one message in a send-reminders instruction.
type ReminderMessage struct {
	Draft    string      `json:"draft"`
	Invoice  string      `json:"invoice"`
	To       string      `json:"to"`
	Location string      `json:"location"`
	Amount   money.Cents `json:"amount"`
	Subject  string      `json:"subject"`
	Body     string      `json:"body"`
}

// ReminderInstruction is what send-reminders asks approval for.
type ReminderInstruction struct {
	Action   string            `json:"action"`
	Agent    string            `json:"agent"`
	Pack     string            `json:"pack"`
	Brand    string            `json:"brand"`
	AsOf     string            `json:"as_of"`
	Channel  string            `json:"channel"`
	Messages []ReminderMessage `json:"messages"`
	Total    money.Cents       `json:"total"`
}

// BuildSendReminders builds the instruction for sending saved drafts. With
// only empty, every draft is included; otherwise only drafts for those
// invoice ids, each of which must exist.
func BuildSendReminders(p *pack.Pack, drafts []Draft, only []string) (ReminderInstruction, string, error) {
	in := ReminderInstruction{Action: "send_reminders", Agent: SendReminders, Pack: p.ID, Brand: p.Brand, AsOf: p.AsOf,
		Channel: "outbox (demo: written to a file on this computer; nothing is sent)", Messages: []ReminderMessage{}}
	byInv := map[string]Draft{}
	for _, d := range drafts {
		byInv[d.Invoice] = d
	}
	pick := drafts
	if len(only) > 0 {
		pick = nil
		for _, id := range only {
			d, ok := byInv[id]
			if !ok {
				return in, "", &ParamError{fmt.Sprintf("there is no saved draft for invoice %q", id)}
			}
			pick = append(pick, d)
		}
	}
	sort.SliceStable(pick, func(i, j int) bool { return pick[i].Invoice < pick[j].Invoice })
	locs := map[string]int{}
	for _, d := range pick {
		in.Messages = append(in.Messages, ReminderMessage{Draft: d.ID, Invoice: d.Invoice, To: d.Customer,
			Location: d.Location, Amount: d.Amount, Subject: d.Subject, Body: d.Body})
		in.Total += d.Amount
		locs[d.LocationName]++
	}
	var parts []string
	for _, l := range p.Locations {
		if n := locs[l.Name]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", l.Name, n))
		}
	}
	summary := fmt.Sprintf("Send %s totaling %s (%s).", plural(len(in.Messages), "payment reminder", "payment reminders"),
		in.Total.String(), strings.Join(parts, ", "))
	return in, summary, nil
}
