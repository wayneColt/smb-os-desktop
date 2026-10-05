package agents

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/wayneColt/smb-os-desktop/internal/money"
	"github.com/wayneColt/smb-os-desktop/internal/pack"
)

// Variance is one measure against its target. GapPct is positive when the
// measure beats its target and negative when it misses, whichever direction
// "better" is.
type Variance struct {
	Location     string  `json:"location"`
	LocationName string  `json:"location_name"`
	Key          string  `json:"key"`
	Label        string  `json:"label"`
	Value        float64 `json:"value"`
	Target       float64 `json:"target"`
	Unit         string  `json:"unit"`
	Better       string  `json:"better"`
	Period       string  `json:"period"`
	GapPct       float64 `json:"gap_pct"`
	OnTarget     bool    `json:"on_target"`
	Text         string  `json:"text"`
	gap          float64
}

// LocationLine is the one-line summary for a location.
type LocationLine struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Manager   string `json:"manager"`
	OnTarget  int    `json:"on_target"`
	OffTarget int    `json:"off_target"`
	Line      string `json:"line"`
}

// AlertSummary counts alerts by severity and lists the ones that need action.
type AlertSummary struct {
	Crit  int          `json:"crit"`
	Warn  int          `json:"warn"`
	Info  int          `json:"info"`
	Needs []pack.Alert `json:"needs_action"`
}

// ARSummary is the receivables picture.
type ARSummary struct {
	Open        int         `json:"open"`
	OpenTotal   money.Cents `json:"open_total"`
	Over14      int         `json:"over_14_days"`
	Over14Total money.Cents `json:"over_14_days_total"`
}

// Brief is the morning brief.
type Brief struct {
	Headline  string         `json:"headline"`
	AsOf      string         `json:"as_of"`
	Period    string         `json:"period"`
	Locations []LocationLine `json:"locations"`
	Variances []Variance     `json:"variances"`
	Alerts    AlertSummary   `json:"alerts"`
	AR        ARSummary      `json:"ar"`
}

// RunMorningBrief builds the brief.
func RunMorningBrief(p *pack.Pack) Brief {
	b := Brief{AsOf: p.AsOf}
	for _, k := range p.KPIs {
		b.Variances = append(b.Variances, variance(p, k))
		if b.Period == "" {
			b.Period = k.Period
		}
	}
	sort.SliceStable(b.Variances, func(i, j int) bool {
		a, c := b.Variances[i], b.Variances[j]
		if a.gap != c.gap {
			return a.gap < c.gap
		}
		if a.Location != c.Location {
			return a.Location < c.Location
		}
		return a.Key < c.Key
	})
	off := 0
	for _, l := range p.Locations {
		line := LocationLine{ID: l.ID, Name: l.Name, Manager: l.Manager}
		var misses []string
		for _, v := range b.Variances {
			if v.Location != l.ID {
				continue
			}
			if v.OnTarget {
				line.OnTarget++
			} else {
				line.OffTarget++
				misses = append(misses, v.Text)
			}
		}
		total := line.OnTarget + line.OffTarget
		off += line.OffTarget
		if line.OffTarget == 0 {
			line.Line = fmt.Sprintf("%s (%s): all %d measures on target.", l.Name, l.Manager, total)
		} else {
			line.Line = fmt.Sprintf("%s (%s): %d of %d off target — %s.", l.Name, l.Manager, line.OffTarget, total, strings.Join(misses, "; "))
		}
		b.Locations = append(b.Locations, line)
	}
	for _, a := range p.Alerts {
		switch a.Severity {
		case "crit":
			b.Alerts.Crit++
		case "warn":
			b.Alerts.Warn++
		default:
			b.Alerts.Info++
		}
		if a.Severity != "info" {
			b.Alerts.Needs = append(b.Alerts.Needs, a)
		}
	}
	sort.SliceStable(b.Alerts.Needs, func(i, j int) bool {
		return b.Alerts.Needs[i].Severity == "crit" && b.Alerts.Needs[j].Severity != "crit"
	})
	if b.Alerts.Needs == nil {
		b.Alerts.Needs = []pack.Alert{}
	}
	for _, inv := range p.AR {
		b.AR.Open++
		b.AR.OpenTotal += inv.Amount
		if p.DaysLate(inv) > 14 {
			b.AR.Over14++
			b.AR.Over14Total += inv.Amount
		}
	}
	n := len(b.Variances)
	period := b.Period
	if period == "" {
		period = "latest period"
	}
	switch {
	case n == 0:
		b.Headline = fmt.Sprintf("%s: no measures in this pack.", p.Brand)
	case off == 0:
		b.Headline = fmt.Sprintf("%s, %s: all %d measures on target.", p.Brand, period, n)
	default:
		w := b.Variances[0]
		b.Headline = fmt.Sprintf("%s, %s: %d of %d measures off target. Worst: %s %s %s against a target of %s.",
			p.Brand, period, off, n, w.LocationName, LowerFirst(w.Label), FormatValue(w.Value, w.Unit), FormatValue(w.Target, w.Unit))
	}
	if b.Alerts.Crit > 0 {
		b.Headline += " " + plural(b.Alerts.Crit, "critical alert", "critical alerts") + "."
	}
	return b
}

func variance(p *pack.Pack, k pack.KPI) Variance {
	gap := (k.Value - k.Target) / math.Abs(k.Target) * 100
	if k.Better == "lower" {
		gap = -gap
	}
	v := Variance{
		Location: k.Location, LocationName: p.LocationName(k.Location),
		Key: k.Key, Label: k.Label, Value: k.Value, Target: k.Target, Unit: k.Unit,
		Better: k.Better, Period: k.Period, GapPct: round1(gap), OnTarget: gap >= 0, gap: gap,
	}
	word := "better"
	if gap < 0 {
		word = "worse"
	}
	v.Text = fmt.Sprintf("%s %s vs target %s (%s%% %s)", LowerFirst(k.Label), FormatValue(k.Value, k.Unit),
		FormatValue(k.Target, k.Unit), strconv.FormatFloat(math.Abs(round1(gap)), 'f', 1, 64), word)
	return v
}

// FormatValue renders a KPI value with its unit for sentences.
func FormatValue(v float64, unit string) string {
	num := strconv.FormatFloat(v, 'f', -1, 64)
	if v != math.Trunc(v) {
		num = strconv.FormatFloat(round1(v), 'f', -1, 64)
	}
	switch unit {
	case "$":
		return money.Cents(math.Round(v * 100)).String()
	case "%":
		return num + "%"
	case "":
		return num
	default:
		return num + " " + unit
	}
}

// LowerFirst lowercases the first letter of a label for use mid-sentence,
// leaving acronyms such as EVV alone.
func LowerFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if len(r) > 1 && unicode.IsUpper(r[1]) {
		return s
	}
	r[0] = unicode.ToLower(r[0])
	return string(r)
}
