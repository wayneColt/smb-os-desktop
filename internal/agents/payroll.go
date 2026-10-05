package agents

import (
	"fmt"
	"sort"
	"time"

	"github.com/wayneColt/smb-os-desktop/internal/money"
	"github.com/wayneColt/smb-os-desktop/internal/pack"
)

// Issue codes found by the payroll precheck.
const (
	IssueOvertime         = "overtime"
	IssueMissingPunch     = "missing_punch"
	IssueNoPunches        = "no_punches"
	IssueFlaggedUnclocked = "flagged_unclocked"
	IssueUnverifiedVisit  = "unverified_visit"
	IssueMissedVisit      = "missed_visit"
)

// Severities: a blocking issue means some hours cannot be computed at all.
const (
	SeverityBlock  = "block"
	SeverityReview = "review"
)

// Issue is one thing to resolve before payroll goes out.
type Issue struct {
	Person   string `json:"person"`
	Name     string `json:"name"`
	Location string `json:"location"`
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Date     string `json:"date"`
	Text     string `json:"text"`
}

// VisitCounts summarizes home care visits.
type VisitCounts struct {
	Scheduled  int `json:"scheduled"`
	Verified   int `json:"verified"`
	Manual     int `json:"manual"`
	Missed     int `json:"missed"`
	Incomplete int `json:"incomplete"`
}

// PersonSummary is one person's pay-period picture.
type PersonSummary struct {
	ID             string       `json:"id"`
	Name           string       `json:"name"`
	Location       string       `json:"location"`
	Role           string       `json:"role"`
	Pay            string       `json:"pay"`
	ScheduledHours money.Hours  `json:"scheduled_hours"`
	WorkedHours    money.Hours  `json:"worked_hours"`
	VarianceHours  money.Hours  `json:"variance_hours"`
	RegularHours   money.Hours  `json:"regular_hours"`
	OvertimeHours  money.Hours  `json:"overtime_hours"`
	FlaggedHours   *money.Hours `json:"flagged_hours,omitempty"`
	EfficiencyPct  *float64     `json:"efficiency_pct,omitempty"`
	Visits         *VisitCounts `json:"visits,omitempty"`
	Issues         int          `json:"issues"`
	Status         string       `json:"status"` // ok | review | blocked
}

// PrecheckTotals sums the period.
type PrecheckTotals struct {
	People           int          `json:"people"`
	PeopleWithIssues int          `json:"people_with_issues"`
	Issues           int          `json:"issues"`
	Blocking         int          `json:"blocking"`
	ScheduledHours   money.Hours  `json:"scheduled_hours"`
	WorkedHours      money.Hours  `json:"worked_hours"`
	RegularHours     money.Hours  `json:"regular_hours"`
	OvertimeHours    money.Hours  `json:"overtime_hours"`
	FlaggedHours     *money.Hours `json:"flagged_hours,omitempty"`
	Visits           *VisitCounts `json:"visits,omitempty"`
	EVVVerifiedPct   *float64     `json:"evv_verified_pct,omitempty"`
}

// Period names the pay period.
type Period struct {
	Start   string `json:"start"`
	End     string `json:"end"`
	PayDate string `json:"pay_date"`
}

// Precheck is the payroll precheck output.
type Precheck struct {
	Period   Period          `json:"period"`
	Basis    string          `json:"basis"`
	Location string          `json:"location"`
	People   []PersonSummary `json:"people"`
	Issues   []Issue         `json:"issues"`
	Totals   PrecheckTotals  `json:"totals"`
	Ready    bool            `json:"ready"`
	Summary  string          `json:"summary"`
	Note     string          `json:"note,omitempty"`
}

// RunPayrollPrecheck checks every person's hours in the pay period.
func RunPayrollPrecheck(p *pack.Pack, location string) Precheck {
	pr := p.Payroll
	out := Precheck{
		Period:   Period{Start: pr.PeriodStart, End: pr.PeriodEnd, PayDate: pr.PayDate},
		Basis:    pr.Basis,
		Location: location,
		Note:     pr.Note,
		People:   []PersonSummary{},
		Issues:   []Issue{},
	}
	evv := pr.Basis == "evv_visits"
	start := date(pr.PeriodStart)
	otLimit := int64(pr.OTWeeklyHours) * 60
	var totFlagged money.Hours
	var totVisits VisitCounts
	var schedMin, workMin, regMin, otMin int64

	for _, person := range pr.People {
		if location != "all" && location != "" && person.Location != location {
			continue
		}
		ps := PersonSummary{ID: person.ID, Name: person.Name, Location: person.Location, Role: person.Role, Pay: person.Pay}
		var issues []Issue
		issue := func(code, sev, day, text string) {
			issues = append(issues, Issue{Person: person.ID, Name: person.Name, Location: person.Location,
				Code: code, Severity: sev, Date: day, Text: text})
		}
		weeks := map[int]int64{}
		var pSched, pWork int64
		var flagged money.Hours
		var visits VisitCounts
		for _, e := range pr.Entries {
			if e.Person != person.ID {
				continue
			}
			brk := int64(e.BreakMin)
			if e.SchedStart != "" {
				pSched += max(span(e.SchedStart, e.SchedEnd)-brk, 0)
			}
			if e.Flagged != nil {
				flagged += *e.Flagged
			}
			hasIn, hasOut := e.In != "", e.Out != ""
			sched := ""
			if e.SchedStart != "" {
				sched = e.SchedStart + "–" + e.SchedEnd
			}
			if evv {
				visits.Scheduled++
			}
			switch {
			case hasIn && hasOut:
				w := max(span(e.In, e.Out)-brk, 0)
				pWork += w
				week := int(date(e.Date).Sub(start).Hours() / 24 / 7)
				weeks[week] += w
				if evv {
					switch e.EVV {
					case "verified":
						visits.Verified++
					default:
						visits.Manual++
						issue(IssueUnverifiedVisit, SeverityReview, e.Date, fmt.Sprintf(
							"%s visit with %s (%s–%s) was entered by hand, not EVV-verified; needs supervisor sign-off before billing.",
							shortDate(e.Date), e.Client, e.In, e.Out))
					}
				}
			case hasIn != hasOut:
				what := "clock-out"
				if !hasIn {
					what = "clock-in"
				}
				if evv {
					visits.Incomplete++
					issue(IssueMissingPunch, SeverityBlock, e.Date, fmt.Sprintf(
						"%s visit with %s has no EVV %s; the visit's hours can't be counted until it is fixed.",
						shortDate(e.Date), e.Client, what))
				} else {
					issue(IssueMissingPunch, SeverityBlock, e.Date, fmt.Sprintf(
						"%s has no %s (scheduled %s); that day's hours can't be counted until it is fixed.",
						shortDate(e.Date), what, sched))
				}
			default: // no punches at all
				switch {
				case evv:
					visits.Missed++
					issue(IssueMissedVisit, SeverityReview, e.Date, fmt.Sprintf(
						"%s visit with %s scheduled %s has no EVV record and no time entered; missed visit or missing record?",
						shortDate(e.Date), e.Client, sched))
				case e.Flagged != nil && *e.Flagged > 0:
					issue(IssueFlaggedUnclocked, SeverityReview, e.Date, fmt.Sprintf(
						"%s shows %s flagged hours but no clock punches; check the repair orders and the time clock.",
						shortDate(e.Date), e.Flagged.String()))
				case e.SchedStart != "":
					issue(IssueNoPunches, SeverityReview, e.Date, fmt.Sprintf(
						"%s was scheduled %s with no punches; an absence, or punches that were never recorded?",
						shortDate(e.Date), sched))
				}
			}
		}
		var pReg, pOT int64
		weekIdx := make([]int, 0, len(weeks))
		for w := range weeks {
			weekIdx = append(weekIdx, w)
		}
		sort.Ints(weekIdx)
		for _, w := range weekIdx {
			m := weeks[w]
			if m > otLimit {
				pOT += m - otLimit
				pReg += otLimit
				weekStart := start.AddDate(0, 0, 7*w)
				issue(IssueOvertime, SeverityReview, weekStart.Format(pack.DateLayout), fmt.Sprintf(
					"worked %s h in the week of %s: %s h of overtime.",
					money.HoursFromMinutes(m).String(), weekStart.Format("Jan 2"), money.HoursFromMinutes(m-otLimit).String()))
			} else {
				pReg += m
			}
		}
		ps.ScheduledHours = money.HoursFromMinutes(pSched)
		ps.WorkedHours = money.HoursFromMinutes(pWork)
		ps.VarianceHours = ps.WorkedHours - ps.ScheduledHours
		ps.RegularHours = money.HoursFromMinutes(pReg)
		ps.OvertimeHours = money.HoursFromMinutes(pOT)
		if evv {
			v := visits
			ps.Visits = &v
			totVisits.Scheduled += v.Scheduled
			totVisits.Verified += v.Verified
			totVisits.Manual += v.Manual
			totVisits.Missed += v.Missed
			totVisits.Incomplete += v.Incomplete
		} else if person.Pay == "flat_rate" {
			f := flagged
			ps.FlaggedHours = &f
			totFlagged += flagged
			if ps.WorkedHours > 0 {
				eff := round1(float64(flagged) / float64(ps.WorkedHours) * 100)
				ps.EfficiencyPct = &eff
			}
		}
		sortIssues(issues)
		ps.Issues = len(issues)
		ps.Status = "ok"
		for _, is := range issues {
			if is.Severity == SeverityBlock {
				ps.Status = "blocked"
				break
			}
			ps.Status = "review"
		}
		out.People = append(out.People, ps)
		out.Issues = append(out.Issues, issues...)
		if len(issues) > 0 {
			out.Totals.PeopleWithIssues++
		}
		schedMin += pSched
		workMin += pWork
		regMin += pReg
		otMin += pOT
	}
	sortIssues(out.Issues)
	t := &out.Totals
	t.People = len(out.People)
	t.Issues = len(out.Issues)
	for _, is := range out.Issues {
		if is.Severity == SeverityBlock {
			t.Blocking++
		}
	}
	t.ScheduledHours = money.HoursFromMinutes(schedMin)
	t.WorkedHours = money.HoursFromMinutes(workMin)
	t.RegularHours = money.HoursFromMinutes(regMin)
	t.OvertimeHours = money.HoursFromMinutes(otMin)
	if evv {
		t.Visits = &totVisits
		delivered := totVisits.Scheduled - totVisits.Missed
		if delivered > 0 {
			pct := round1(float64(totVisits.Verified) / float64(delivered) * 100)
			t.EVVVerifiedPct = &pct
		}
	} else {
		t.FlaggedHours = &totFlagged
	}
	out.Ready = t.Blocking == 0
	where := "all locations"
	if location != "all" && location != "" {
		where = p.LocationName(location)
	}
	out.Summary = fmt.Sprintf("Pay period %s to %s, %s: %s, %s h worked (%s h overtime). %s to resolve",
		longDate(pr.PeriodStart), longDate(pr.PeriodEnd), where, plural(t.People, "person", "people"),
		t.WorkedHours.String(), t.OvertimeHours.String(), plural(t.Issues, "issue", "issues"))
	if t.Blocking > 0 {
		out.Summary += fmt.Sprintf(", %d blocking.", t.Blocking)
	} else {
		out.Summary += "."
	}
	return out
}

func sortIssues(is []Issue) {
	sort.SliceStable(is, func(i, j int) bool {
		a, b := is[i], is[j]
		if a.Severity != b.Severity {
			return a.Severity == SeverityBlock
		}
		if a.Location != b.Location {
			return a.Location < b.Location
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if a.Date != b.Date {
			return a.Date < b.Date
		}
		return a.Code < b.Code
	})
}

// span returns minutes from a to b (HH:MM), wrapping past midnight.
func span(a, b string) int64 {
	ta, err1 := time.Parse(pack.TimeLayout, a)
	tb, err2 := time.Parse(pack.TimeLayout, b)
	if err1 != nil || err2 != nil {
		return 0
	}
	m := int64(tb.Sub(ta).Minutes())
	if m < 0 {
		m += 24 * 60
	}
	return m
}

// PayrollRow is one line of the payroll export.
type PayrollRow struct {
	Person         string       `json:"person"`
	Name           string       `json:"name"`
	Location       string       `json:"location"`
	Pay            string       `json:"pay"`
	RegularHours   money.Hours  `json:"regular_hours"`
	OvertimeHours  money.Hours  `json:"overtime_hours"`
	FlaggedHours   *money.Hours `json:"flagged_hours,omitempty"`
	VisitsVerified *int         `json:"visits_verified,omitempty"`
	OpenIssues     int          `json:"open_issues"`
}

// PayrollInstruction is what payroll-submit asks approval for. Its canonical
// JSON is what the proposal hash covers.
type PayrollInstruction struct {
	Action         string       `json:"action"`
	Agent          string       `json:"agent"`
	Pack           string       `json:"pack"`
	Brand          string       `json:"brand"`
	Period         Period       `json:"period"`
	Location       string       `json:"location"`
	Rows           []PayrollRow `json:"rows"`
	RegularHours   money.Hours  `json:"regular_hours"`
	OvertimeHours  money.Hours  `json:"overtime_hours"`
	OpenIssues     int          `json:"open_issues"`
	BlockingIssues int          `json:"blocking_issues"`
	Destination    string       `json:"destination"`
}

// BuildPayrollSubmit builds the payroll export instruction and its summary.
func BuildPayrollSubmit(p *pack.Pack, location string) (PayrollInstruction, string) {
	pc := RunPayrollPrecheck(p, location)
	in := PayrollInstruction{
		Action: "submit_payroll", Agent: PayrollSubmit, Pack: p.ID, Brand: p.Brand,
		Period: pc.Period, Location: location, Rows: []PayrollRow{},
		RegularHours: pc.Totals.RegularHours, OvertimeHours: pc.Totals.OvertimeHours,
		OpenIssues: pc.Totals.Issues, BlockingIssues: pc.Totals.Blocking,
		Destination: "outbox (demo: written to a file on this computer; nothing is sent)",
	}
	for _, ps := range pc.People {
		row := PayrollRow{Person: ps.ID, Name: ps.Name, Location: ps.Location, Pay: ps.Pay,
			RegularHours: ps.RegularHours, OvertimeHours: ps.OvertimeHours, FlaggedHours: ps.FlaggedHours,
			OpenIssues: ps.Issues}
		if ps.Visits != nil {
			v := ps.Visits.Verified
			row.VisitsVerified = &v
		}
		in.Rows = append(in.Rows, row)
	}
	where := "all locations"
	if location != "all" {
		where = p.LocationName(location)
	}
	summary := fmt.Sprintf("Submit payroll for %s to %s (%s, pay date %s): %s, %s regular h and %s overtime h.",
		longDate(pc.Period.Start), longDate(pc.Period.End), where, longDate(pc.Period.PayDate),
		plural(len(in.Rows), "person", "people"), in.RegularHours.String(), in.OvertimeHours.String())
	if in.OpenIssues > 0 {
		summary += fmt.Sprintf(" %s still open from the precheck", plural(in.OpenIssues, "issue is", "issues are"))
		if in.BlockingIssues > 0 {
			summary += fmt.Sprintf(" (%d with hours that cannot be counted)", in.BlockingIssues)
		}
		summary += "."
	}
	return in, summary
}
