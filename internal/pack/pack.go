// Package pack loads and validates the demo business packs. A pack is the
// whole picture of one fictional franchise business: locations, KPIs, alerts,
// inbox, schedule, receivables, monthly sales, a payroll period with
// timesheets, and a bank export with the matching ledger.
package pack

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/wayneColt/smb-os-desktop/internal/money"
	"github.com/wayneColt/smb-os-desktop/packs"
)

// Layouts used by pack data.
const (
	DateLayout = "2006-01-02"
	TimeLayout = "15:04"
)

// Pack is one business.
type Pack struct {
	ID          string     `json:"pack"`
	Label       string     `json:"label"`
	Brand       string     `json:"brand"`
	Fictional   bool       `json:"fictional"`
	AsOf        string     `json:"as_of"`
	Operator    string     `json:"operator"`
	RoyaltyRate float64    `json:"royalty_rate"`
	AdFundRate  float64    `json:"ad_fund_rate"`
	RoyaltyBase string     `json:"royalty_base"`
	Locations   []Location `json:"locations"`
	KPIs        []KPI      `json:"kpis"`
	Alerts      []Alert    `json:"alerts"`
	Inbox       []Message  `json:"inbox"`
	Schedule    []Shift    `json:"schedule"`
	AR          []Invoice  `json:"ar"`
	Revenue     []Revenue  `json:"revenue"`
	Payroll     Payroll    `json:"payroll"`
	Bank        BankExport `json:"bank"`
	Ledger      []Ledger   `json:"ledger"`
}

// Location is one franchise unit.
type Location struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Manager string `json:"manager"`
}

// KPI is one measure for one location against its target.
type KPI struct {
	Location string  `json:"location"`
	Key      string  `json:"key"`
	Label    string  `json:"label"`
	Value    float64 `json:"value"`
	Target   float64 `json:"target"`
	Unit     string  `json:"unit"`
	Better   string  `json:"better"`
	Period   string  `json:"period"`
}

// Alert is something the operator should see today.
type Alert struct {
	ID       string `json:"id"`
	Severity string `json:"severity"`
	Location string `json:"location"`
	Text     string `json:"text"`
}

// Message is one inbox item.
type Message struct {
	ID       string `json:"id"`
	From     string `json:"from"`
	Subject  string `json:"subject"`
	Received string `json:"received"`
	Tag      string `json:"tag"`
	Location string `json:"location,omitempty"`
	Preview  string `json:"preview,omitempty"`
}

// Shift is one scheduled block of work this week.
type Shift struct {
	Location string `json:"location"`
	Who      string `json:"who"`
	Role     string `json:"role"`
	Start    string `json:"start"`
	End      string `json:"end"`
}

// Invoice is one open receivable.
type Invoice struct {
	ID       string      `json:"id"`
	Customer string      `json:"customer"`
	Location string      `json:"location"`
	Amount   money.Cents `json:"amount"`
	Due      string      `json:"due"`
	DaysLate int         `json:"days_late"`
	Contact  string      `json:"contact,omitempty"`
}

// Revenue is one location's sales for one month.
type Revenue struct {
	Location   string      `json:"location"`
	Period     string      `json:"period"` // YYYY-MM
	GrossSales money.Cents `json:"gross_sales"`
	Allowances money.Cents `json:"allowances"` // discounts, refunds, write-offs
}

// Payroll is one pay period.
type Payroll struct {
	PeriodStart   string   `json:"period_start"`
	PeriodEnd     string   `json:"period_end"`
	PayDate       string   `json:"pay_date"`
	Basis         string   `json:"basis"` // flagged_vs_clocked | evv_visits
	OTWeeklyHours int      `json:"ot_weekly_hours"`
	Note          string   `json:"note,omitempty"`
	People        []Person `json:"people"`
	Entries       []Entry  `json:"entries"`
}

// Person is one employee in the pay period.
type Person struct {
	ID       string      `json:"id"`
	Name     string      `json:"name"`
	Location string      `json:"location"`
	Role     string      `json:"role"`
	Pay      string      `json:"pay"` // hourly | flat_rate
	Rate     money.Cents `json:"rate"`
}

// Entry is one day (auto: a shift with punches and flagged hours) or one
// visit (homecare: a scheduled visit with its EVV record).
type Entry struct {
	Person     string       `json:"person"`
	Date       string       `json:"date"`
	SchedStart string       `json:"sched_start,omitempty"`
	SchedEnd   string       `json:"sched_end,omitempty"`
	In         string       `json:"in,omitempty"`
	Out        string       `json:"out,omitempty"`
	BreakMin   int          `json:"break_min,omitempty"`
	Flagged    *money.Hours `json:"flagged,omitempty"`
	Client     string       `json:"client,omitempty"`
	EVV        string       `json:"evv,omitempty"` // verified | manual | none
}

// BankExport is the bank's view of the operating account.
type BankExport struct {
	Account string     `json:"account"`
	From    string     `json:"from"`
	To      string     `json:"to"`
	Lines   []BankLine `json:"lines"`
}

// BankLine is one bank transaction (deposits positive, withdrawals negative).
type BankLine struct {
	ID          string      `json:"id"`
	Date        string      `json:"date"`
	Amount      money.Cents `json:"amount"`
	Description string      `json:"description"`
}

// Ledger is one cash entry in the books.
type Ledger struct {
	ID      string      `json:"id"`
	Date    string      `json:"date"`
	Amount  money.Cents `json:"amount"`
	Memo    string      `json:"memo"`
	Ref     string      `json:"ref"`
	Account string      `json:"account"`
}

// Info is the short form used by GET /api/packs.
type Info struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

var order = []string{"auto", "homecare"}

// List returns the bundled packs in display order.
func List() []Info {
	out := make([]Info, 0, len(order))
	for _, id := range order {
		p, err := Load(id)
		if err != nil {
			continue
		}
		out = append(out, Info{ID: p.ID, Label: p.Label})
	}
	return out
}

// Exists reports whether id names a bundled pack.
func Exists(id string) bool {
	for _, o := range order {
		if o == id {
			return true
		}
	}
	return false
}

// Load decodes and validates a bundled pack. Every call returns a fresh copy,
// so callers may not corrupt each other's view.
func Load(id string) (*Pack, error) {
	if !Exists(id) {
		return nil, fmt.Errorf("unknown pack %q", id)
	}
	raw, err := fs.ReadFile(packs.FS, id+".json")
	if err != nil {
		return nil, fmt.Errorf("pack %s: %w", id, err)
	}
	return Decode(raw)
}

// Decode parses and validates pack JSON.
func Decode(raw []byte) (*Pack, error) {
	var p Pack
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("pack: %w", err)
	}
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("pack %s: %w", p.ID, err)
	}
	return &p, nil
}

// AsOfDate returns the pack's as-of date.
func (p *Pack) AsOfDate() time.Time {
	t, _ := time.Parse(DateLayout, p.AsOf)
	return t
}

// Location returns the location with the given id.
func (p *Pack) Location(id string) (Location, bool) {
	for _, l := range p.Locations {
		if l.ID == id {
			return l, true
		}
	}
	return Location{}, false
}

// LocationName returns the location's name, or the id if unknown.
func (p *Pack) LocationName(id string) string {
	if l, ok := p.Location(id); ok {
		return l.Name
	}
	return id
}

// Person returns the payroll person with the given id.
func (p *Pack) Person(id string) (Person, bool) {
	for _, x := range p.Payroll.People {
		if x.ID == id {
			return x, true
		}
	}
	return Person{}, false
}

// DaysLate returns how many days past due an invoice is on the as-of date.
func (p *Pack) DaysLate(inv Invoice) int {
	due, err := time.Parse(DateLayout, inv.Due)
	if err != nil {
		return 0
	}
	d := int(p.AsOfDate().Sub(due).Hours() / 24)
	if d < 0 {
		return 0
	}
	return d
}

// RevenuePeriods returns the months present in the revenue data, oldest first.
func (p *Pack) RevenuePeriods() []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range p.Revenue {
		if !seen[r.Period] {
			seen[r.Period] = true
			out = append(out, r.Period)
		}
	}
	sort.Strings(out)
	return out
}

// Validate checks the internal consistency of a pack. Any error here is a bug
// in the pack file, and the packs' tests fail on it.
func (p *Pack) Validate() error {
	var errs []string
	add := func(f string, a ...any) { errs = append(errs, fmt.Sprintf(f, a...)) }
	if !Exists(p.ID) {
		add("unknown pack id %q", p.ID)
	}
	if !p.Fictional {
		add("fictional must be true: every bundled pack is demo data")
	}
	if p.Label == "" || p.Brand == "" || p.Operator == "" {
		add("label, brand and operator are required")
	}
	if _, err := time.Parse(DateLayout, p.AsOf); err != nil {
		add("as_of %q: %v", p.AsOf, err)
	}
	for _, r := range []struct {
		name string
		v    float64
	}{{"royalty_rate", p.RoyaltyRate}, {"ad_fund_rate", p.AdFundRate}} {
		if r.v < 0 || r.v >= 1 || math.Abs(r.v*1e6-math.Round(r.v*1e6)) > 1e-6 {
			add("%s %v must be a fraction in [0,1) with at most six decimals", r.name, r.v)
		}
	}
	locs := map[string]bool{}
	for _, l := range p.Locations {
		if l.ID == "" || l.Name == "" || locs[l.ID] {
			add("location %q: id and name required, ids unique", l.ID)
		}
		locs[l.ID] = true
	}
	if len(locs) == 0 {
		add("at least one location is required")
	}
	needLoc := func(where, id string) {
		if !locs[id] {
			add("%s: unknown location %q", where, id)
		}
	}
	kpiKeys := map[string]bool{}
	for _, k := range p.KPIs {
		needLoc("kpi "+k.Key, k.Location)
		if k.Better != "higher" && k.Better != "lower" {
			add("kpi %s/%s: better must be higher or lower", k.Location, k.Key)
		}
		if k.Target == 0 {
			add("kpi %s/%s: target must be non-zero", k.Location, k.Key)
		}
		id := k.Location + "." + k.Key
		if kpiKeys[id] {
			add("kpi %s duplicated", id)
		}
		kpiKeys[id] = true
	}
	ids := map[string]bool{}
	uniq := func(kind, id string) {
		if id == "" || ids[kind+":"+id] {
			add("%s id %q: required and unique", kind, id)
		}
		ids[kind+":"+id] = true
	}
	for _, a := range p.Alerts {
		uniq("alert", a.ID)
		needLoc("alert "+a.ID, a.Location)
		if a.Severity != "info" && a.Severity != "warn" && a.Severity != "crit" {
			add("alert %s: severity must be info, warn or crit", a.ID)
		}
	}
	tags := map[string]bool{"customer": true, "vendor": true, "franchisor": true, "staff": true}
	for _, m := range p.Inbox {
		uniq("message", m.ID)
		if !tags[m.Tag] {
			add("message %s: unknown tag %q", m.ID, m.Tag)
		}
		if _, err := time.Parse(time.RFC3339, m.Received); err != nil {
			add("message %s: received: %v", m.ID, err)
		}
		if m.Location != "" {
			needLoc("message "+m.ID, m.Location)
		}
	}
	for i, s := range p.Schedule {
		needLoc(fmt.Sprintf("schedule[%d]", i), s.Location)
		st, err1 := time.Parse(time.RFC3339, s.Start)
		en, err2 := time.Parse(time.RFC3339, s.End)
		if err1 != nil || err2 != nil || !en.After(st) {
			add("schedule[%d]: start/end must be RFC3339 with end after start", i)
		}
	}
	for _, inv := range p.AR {
		uniq("invoice", inv.ID)
		needLoc("invoice "+inv.ID, inv.Location)
		if inv.Amount <= 0 {
			add("invoice %s: amount must be positive", inv.ID)
		}
		if _, err := time.Parse(DateLayout, inv.Due); err != nil {
			add("invoice %s: due: %v", inv.ID, err)
		} else if got := p.DaysLate(inv); got != inv.DaysLate {
			add("invoice %s: days_late %d but due %s is %d days before as_of", inv.ID, inv.DaysLate, inv.Due, got)
		}
	}
	revSeen := map[string]bool{}
	for _, r := range p.Revenue {
		needLoc("revenue "+r.Period, r.Location)
		if _, err := time.Parse("2006-01", r.Period); err != nil {
			add("revenue %s/%s: period must be YYYY-MM", r.Location, r.Period)
		}
		if revSeen[r.Location+r.Period] {
			add("revenue %s/%s duplicated", r.Location, r.Period)
		}
		revSeen[r.Location+r.Period] = true
		if r.Allowances < 0 || r.Allowances > r.GrossSales {
			add("revenue %s/%s: allowances out of range", r.Location, r.Period)
		}
	}
	p.validatePayroll(add, locs)
	for _, b := range p.Bank.Lines {
		uniq("bank", b.ID)
		if _, err := time.Parse(DateLayout, b.Date); err != nil {
			add("bank %s: date: %v", b.ID, err)
		}
		if b.Amount == 0 || b.Description == "" {
			add("bank %s: amount and description required", b.ID)
		}
	}
	for _, g := range p.Ledger {
		uniq("ledger", g.ID)
		if _, err := time.Parse(DateLayout, g.Date); err != nil {
			add("ledger %s: date: %v", g.ID, err)
		}
		if g.Amount == 0 {
			add("ledger %s: amount required", g.ID)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%d problem(s): %s", len(errs), strings.Join(errs, "; "))
	}
	return nil
}

func (p *Pack) validatePayroll(add func(string, ...any), locs map[string]bool) {
	pr := p.Payroll
	start, err1 := time.Parse(DateLayout, pr.PeriodStart)
	end, err2 := time.Parse(DateLayout, pr.PeriodEnd)
	if err1 != nil || err2 != nil || end.Before(start) {
		add("payroll: period_start/period_end invalid")
		return
	}
	if start.Weekday() != time.Monday || end.Weekday() != time.Sunday {
		add("payroll: period must run Monday to Sunday so overtime weeks are whole")
	}
	if _, err := time.Parse(DateLayout, pr.PayDate); err != nil {
		add("payroll: pay_date: %v", err)
	}
	if pr.Basis != "flagged_vs_clocked" && pr.Basis != "evv_visits" {
		add("payroll: basis must be flagged_vs_clocked or evv_visits")
	}
	if pr.OTWeeklyHours <= 0 {
		add("payroll: ot_weekly_hours must be positive")
	}
	people := map[string]bool{}
	for _, x := range pr.People {
		if x.ID == "" || people[x.ID] {
			add("payroll person %q: id required and unique", x.ID)
		}
		people[x.ID] = true
		if !locs[x.Location] {
			add("payroll person %s: unknown location %q", x.ID, x.Location)
		}
		if x.Pay != "hourly" && x.Pay != "flat_rate" {
			add("payroll person %s: pay must be hourly or flat_rate", x.ID)
		}
	}
	for i, e := range pr.Entries {
		where := fmt.Sprintf("payroll entry %d (%s %s)", i, e.Person, e.Date)
		if !people[e.Person] {
			add("%s: unknown person", where)
		}
		d, err := time.Parse(DateLayout, e.Date)
		if err != nil || d.Before(start) || d.After(end) {
			add("%s: date outside the pay period", where)
		}
		for _, t := range []string{e.SchedStart, e.SchedEnd, e.In, e.Out} {
			if t != "" {
				if _, err := time.Parse(TimeLayout, t); err != nil {
					add("%s: time %q must be HH:MM", where, t)
				}
			}
		}
		if (e.SchedStart == "") != (e.SchedEnd == "") {
			add("%s: sched_start and sched_end go together", where)
		}
		switch pr.Basis {
		case "evv_visits":
			if e.Client == "" || e.SchedStart == "" {
				add("%s: a visit needs a client and a scheduled time", where)
			}
			if e.EVV != "verified" && e.EVV != "manual" && e.EVV != "none" {
				add("%s: evv must be verified, manual or none", where)
			}
			if e.Flagged != nil {
				add("%s: flagged hours do not apply to visits", where)
			}
		case "flagged_vs_clocked":
			if e.EVV != "" || e.Client != "" {
				add("%s: evv/client do not apply to shop shifts", where)
			}
		}
	}
}
