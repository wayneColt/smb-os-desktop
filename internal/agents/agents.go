// Package agents holds the seven deterministic operator agents. Every agent is
// a pure function of a pack (plus, for the ones that act, the operator's saved
// drafts): the same inputs always give the same output, and nothing here
// touches the disk, the network or the clock. The engine decides tiers,
// writes receipts and applies effects.
package agents

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/wayneColt/smb-os-desktop/internal/pack"
)

// Agent ids.
const (
	MorningBrief     = "morning-brief"
	PayrollPrecheck  = "payroll-precheck"
	Reconcile        = "reconcile"
	ARReminders      = "ar-reminders"
	SendReminders    = "send-reminders"
	PayrollSubmit    = "payroll-submit"
	FranchisorRollup = "franchisor-rollup"
)

// Spec describes an agent for GET /api/agents.
type Spec struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Tier        int     `json:"tier"`
	Description string  `json:"description"`
	Params      []Param `json:"params"`
}

// Param describes one agent parameter.
type Param struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"` // bool | int | string | string_list
	Default     any      `json:"default"`
	Description string   `json:"description"`
	Enum        []string `json:"enum,omitempty"`
	// RaisesTierTo is set when a true value moves the run to a higher tier
	// (reconcile's apply moves it from 0 to 1).
	RaisesTierTo int `json:"raises_tier_to,omitempty"`
}

// Params is a validated parameter set with defaults filled in.
type Params map[string]any

// Bool, Int, String and List read typed values from validated params.
func (p Params) Bool(k string) bool     { v, _ := p[k].(bool); return v }
func (p Params) Int(k string) int       { v, _ := p[k].(int); return v }
func (p Params) String(k string) string { v, _ := p[k].(string); return v }
func (p Params) List(k string) []string { v, _ := p[k].([]string); return v }

// Specs returns the agent catalog. Location enums depend on the active pack.
func Specs(p *pack.Pack) []Spec {
	locEnum := []string{"all"}
	for _, l := range p.Locations {
		locEnum = append(locEnum, l.ID)
	}
	periods := p.RevenuePeriods()
	latest := ""
	if len(periods) > 0 {
		latest = periods[len(periods)-1]
	}
	return []Spec{
		{ID: MorningBrief, Name: "Morning brief", Tier: 0,
			Description: "Headline, one line per location, and every measure against its target, worst first.",
			Params:      []Param{}},
		{ID: PayrollPrecheck, Name: "Payroll precheck", Tier: 0,
			Description: precheckDescription(p),
			Params: []Param{{Name: "location", Type: "string", Default: "all", Enum: locEnum,
				Description: "Check one location or all of them."}}},
		{ID: Reconcile, Name: "Bank reconciliation", Tier: 0,
			Description: "Match bank lines to ledger entries (exact amount, dates within 3 days, reference on the bank line), list what is unmatched on both sides, and suggest entries for bank fees and interest.",
			Params: []Param{{Name: "apply", Type: "bool", Default: false, RaisesTierTo: 1,
				Description: "Save the matches to your books (a reversible local change)."}}},
		{ID: ARReminders, Name: "Receivables reminders", Tier: 1,
			Description: "Draft a reminder note for every invoice more than 14 days late and save them as drafts. Nothing is sent.",
			Params: []Param{{Name: "over_days", Type: "int", Default: 14,
				Description: "Draft for invoices more than this many days late."}}},
		{ID: SendReminders, Name: "Send reminders", Tier: 2,
			Description: "Send the saved reminder drafts. Needs your approval; in this demo the messages are written to the outbox folder and nothing leaves the computer.",
			Params: []Param{{Name: "invoices", Type: "string_list", Default: []string{},
				Description: "Only these invoice ids (default: every saved draft)."}}},
		{ID: PayrollSubmit, Name: "Submit payroll", Tier: 2,
			Description: "Write the pay-period hours export for your payroll system. Needs your approval; in this demo the export is written to the outbox folder.",
			Params: []Param{{Name: "location", Type: "string", Default: "all", Enum: locEnum,
				Description: "Submit one location or all of them."}}},
		{ID: FranchisorRollup, Name: "Franchise rollup", Tier: 0,
			Description: "Net sales, royalty and ad fund per location for a month, the franchise total, and the change from the month before.",
			Params: []Param{{Name: "period", Type: "string", Default: latest, Enum: periods,
				Description: "Month (YYYY-MM)."}}},
	}
}

func precheckDescription(p *pack.Pack) string {
	if p.Payroll.Basis == "evv_visits" {
		return "Hours against the schedule for each caregiver: overtime, missing EVV punches, visits not EVV-verified, missed visits, and totals."
	}
	return "Hours against the schedule for each person: overtime, missing punches, flagged versus clocked hours for technicians, and totals."
}

// Find returns the spec with the given id.
func Find(p *pack.Pack, id string) (Spec, bool) {
	for _, s := range Specs(p) {
		if s.ID == id {
			return s, true
		}
	}
	return Spec{}, false
}

// ParamError is a problem with the caller's parameters.
type ParamError struct{ Msg string }

func (e *ParamError) Error() string { return e.Msg }

// ParseParams validates raw parameters against a spec and fills defaults.
func (s Spec) ParseParams(raw map[string]json.RawMessage) (Params, error) {
	out := Params{}
	known := map[string]Param{}
	for _, prm := range s.Params {
		known[prm.Name] = prm
		out[prm.Name] = prm.Default
		if prm.Type == "string_list" {
			out[prm.Name] = []string{}
		}
	}
	names := make([]string, 0, len(raw))
	for k := range raw {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		prm, ok := known[k]
		if !ok {
			return nil, &ParamError{fmt.Sprintf("%s has no parameter %q", s.ID, k)}
		}
		v := raw[k]
		bad := func() error {
			return &ParamError{fmt.Sprintf("parameter %q must be a %s", k, strings.ReplaceAll(prm.Type, "_", " "))}
		}
		switch prm.Type {
		case "bool":
			var b bool
			if json.Unmarshal(v, &b) != nil {
				return nil, bad()
			}
			out[k] = b
		case "int":
			var f float64
			if json.Unmarshal(v, &f) != nil || f != math.Trunc(f) || f < 0 || f > 10000 {
				return nil, bad()
			}
			out[k] = int(f)
		case "string":
			var str string
			if json.Unmarshal(v, &str) != nil {
				return nil, bad()
			}
			if len(prm.Enum) > 0 && !contains(prm.Enum, str) {
				return nil, &ParamError{fmt.Sprintf("parameter %q must be one of %s", k, strings.Join(prm.Enum, ", "))}
			}
			out[k] = str
		case "string_list":
			var list []string
			if json.Unmarshal(v, &list) != nil {
				return nil, bad()
			}
			// Order and repeats do not change what the instruction means, so
			// they must not change its hash either.
			list = dedupeSorted(list)
			out[k] = list
		}
	}
	return out, nil
}

// EffectiveTier is the tier a run with these params executes at.
func (s Spec) EffectiveTier(prm Params) int {
	t := s.Tier
	for _, p := range s.Params {
		if p.RaisesTierTo > t && p.Type == "bool" && prm.Bool(p.Name) {
			t = p.RaisesTierTo
		}
	}
	return t
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func dedupeSorted(in []string) []string {
	set := map[string]bool{}
	out := []string{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" && !set[s] {
			set[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// ---- shared helpers ----

func date(s string) time.Time {
	t, _ := time.Parse(pack.DateLayout, s)
	return t
}

func longDate(s string) string {
	t, err := time.Parse(pack.DateLayout, s)
	if err != nil {
		return s
	}
	return t.Format("Jan 2, 2006")
}

func shortDate(s string) string {
	t, err := time.Parse(pack.DateLayout, s)
	if err != nil {
		return s
	}
	return t.Format("Mon Jan 2")
}

func daysApart(a, b string) int {
	d := int(date(a).Sub(date(b)).Hours() / 24)
	if d < 0 {
		return -d
	}
	return d
}

func round1(x float64) float64 { return math.Round(x*10) / 10 }

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
