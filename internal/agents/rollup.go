package agents

import (
	"fmt"
	"time"

	"github.com/wayneColt/smb-os-desktop/internal/money"
	"github.com/wayneColt/smb-os-desktop/internal/pack"
)

// RollupLine is one location's month.
type RollupLine struct {
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	GrossSales    money.Cents  `json:"gross_sales"`
	Allowances    money.Cents  `json:"allowances"`
	NetSales      money.Cents  `json:"net_sales"`
	Royalty       money.Cents  `json:"royalty"`
	AdFund        money.Cents  `json:"ad_fund"`
	TotalDue      money.Cents  `json:"total_due"`
	PriorNetSales *money.Cents `json:"prior_net_sales,omitempty"`
	ChangePct     *float64     `json:"change_pct,omitempty"`
}

// RollupTotal is the franchise total.
type RollupTotal struct {
	GrossSales    money.Cents  `json:"gross_sales"`
	Allowances    money.Cents  `json:"allowances"`
	NetSales      money.Cents  `json:"net_sales"`
	Royalty       money.Cents  `json:"royalty"`
	AdFund        money.Cents  `json:"ad_fund"`
	TotalDue      money.Cents  `json:"total_due"`
	PriorNetSales *money.Cents `json:"prior_net_sales,omitempty"`
	ChangePct     *float64     `json:"change_pct,omitempty"`
}

// Rollup is the franchisor rollup output.
type Rollup struct {
	Period      string       `json:"period"`
	PeriodLabel string       `json:"period_label"`
	PriorPeriod string       `json:"prior_period,omitempty"`
	RoyaltyRate float64      `json:"royalty_rate"`
	AdFundRate  float64      `json:"ad_fund_rate"`
	Basis       string       `json:"basis"`
	Locations   []RollupLine `json:"locations"`
	Total       RollupTotal  `json:"total"`
	Summary     string       `json:"summary"`
}

// RunFranchisorRollup computes royalty and ad fund per location for a month.
// Each location's fees are rounded to the cent (half away from zero) and the
// franchise total is the sum of the rounded location figures, as a
// franchisor invoices them.
func RunFranchisorRollup(p *pack.Pack, period string) (Rollup, error) {
	if _, err := time.Parse("2006-01", period); err != nil {
		return Rollup{}, &ParamError{fmt.Sprintf("period %q must be YYYY-MM", period)}
	}
	prior := ""
	for _, per := range p.RevenuePeriods() {
		if per < period {
			prior = per
		}
	}
	basis := p.RoyaltyBase
	if basis == "" {
		basis = "net sales (gross sales less discounts, refunds and write-offs)"
	}
	r := Rollup{Period: period, PeriodLabel: monthLabel(period), PriorPeriod: prior,
		RoyaltyRate: p.RoyaltyRate, AdFundRate: p.AdFundRate, Basis: basis, Locations: []RollupLine{}}
	roy, ad := money.RatePPM(p.RoyaltyRate), money.RatePPM(p.AdFundRate)
	var priorTotal money.Cents
	havePrior := prior != ""
	found := false
	for _, l := range p.Locations {
		cur, ok := revenue(p, l.ID, period)
		if !ok {
			continue
		}
		found = true
		line := RollupLine{ID: l.ID, Name: l.Name, GrossSales: cur.GrossSales, Allowances: cur.Allowances}
		line.NetSales = cur.GrossSales - cur.Allowances
		line.Royalty = line.NetSales.MulRate(roy)
		line.AdFund = line.NetSales.MulRate(ad)
		line.TotalDue = line.Royalty + line.AdFund
		if pr, ok := revenue(p, l.ID, prior); ok && havePrior {
			net := pr.GrossSales - pr.Allowances
			line.PriorNetSales = &net
			if net != 0 {
				c := round1(float64(line.NetSales-net) / float64(net) * 100)
				line.ChangePct = &c
			}
			priorTotal += net
		} else {
			havePrior = false
		}
		r.Locations = append(r.Locations, line)
		t := &r.Total
		t.GrossSales += line.GrossSales
		t.Allowances += line.Allowances
		t.NetSales += line.NetSales
		t.Royalty += line.Royalty
		t.AdFund += line.AdFund
		t.TotalDue += line.TotalDue
	}
	if !found {
		return Rollup{}, &ParamError{fmt.Sprintf("no sales recorded for %s", period)}
	}
	if havePrior && priorTotal != 0 {
		r.Total.PriorNetSales = &priorTotal
		c := round1(float64(r.Total.NetSales-priorTotal) / float64(priorTotal) * 100)
		r.Total.ChangePct = &c
	}
	r.Summary = fmt.Sprintf("%s: net sales %s across %s; royalty %s and ad fund %s, %s due to the franchisor.",
		r.PeriodLabel, r.Total.NetSales.String(), plural(len(r.Locations), "location", "locations"),
		r.Total.Royalty.String(), r.Total.AdFund.String(), r.Total.TotalDue.String())
	return r, nil
}

func revenue(p *pack.Pack, loc, period string) (pack.Revenue, bool) {
	for _, r := range p.Revenue {
		if r.Location == loc && r.Period == period {
			return r, true
		}
	}
	return pack.Revenue{}, false
}

func monthLabel(period string) string {
	t, err := time.Parse("2006-01", period)
	if err != nil {
		return period
	}
	return t.Format("January 2006")
}
