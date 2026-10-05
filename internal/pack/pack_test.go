package pack

import (
	"encoding/json"
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/wayneColt/smb-os-desktop/packs"
)

func TestBothPacksLoadAndValidate(t *testing.T) {
	want := map[string]struct {
		brand string
		locs  []string
	}{
		"auto":     {"Kestrel Auto Care", []string{"L14:Riverside", "L27:Hillcrest"}},
		"homecare": {"Larkmoor Home Care", []string{"L3:North County", "L9:Lakeside"}},
	}
	for id, w := range want {
		p, err := Load(id)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if !p.Fictional || p.Brand != w.brand || p.ID != id {
			t.Fatalf("%s: fictional=%v brand=%q id=%q", id, p.Fictional, p.Brand, p.ID)
		}
		var got []string
		for _, l := range p.Locations {
			got = append(got, l.ID+":"+l.Name)
		}
		if strings.Join(got, ",") != strings.Join(w.locs, ",") {
			t.Fatalf("%s locations %v", id, got)
		}
		if len(p.KPIs) != 12 || len(p.Alerts) < 5 || len(p.Inbox) < 8 || len(p.Schedule) < 40 ||
			len(p.AR) != 6 || len(p.Payroll.Entries) < 80 || len(p.Bank.Lines) < 14 || len(p.Ledger) < 13 {
			t.Fatalf("%s is thinner than expected: kpis=%d alerts=%d inbox=%d schedule=%d ar=%d entries=%d bank=%d ledger=%d",
				id, len(p.KPIs), len(p.Alerts), len(p.Inbox), len(p.Schedule), len(p.AR), len(p.Payroll.Entries), len(p.Bank.Lines), len(p.Ledger))
		}
	}
}

func TestKPIKeysMatchTheContract(t *testing.T) {
	want := map[string][]string{
		"auto":     {"car_count", "average_repair_order", "gross_profit_pct", "labor_pct", "bay_utilization", "comebacks"},
		"homecare": {"billable_hours", "evv_verified_pct", "caregiver_utilization", "missed_visits", "overtime_hours", "new_client_starts"},
	}
	for id, keys := range want {
		p, _ := Load(id)
		for _, l := range p.Locations {
			for _, k := range keys {
				found := false
				for _, kpi := range p.KPIs {
					found = found || (kpi.Location == l.ID && kpi.Key == k)
				}
				if !found {
					t.Errorf("%s: %s has no %s", id, l.ID, k)
				}
			}
		}
	}
}

func TestLoadReturnsIndependentCopies(t *testing.T) {
	a, _ := Load("auto")
	a.Brand = "changed"
	b, _ := Load("auto")
	if b.Brand == "changed" {
		t.Fatal("Load must not share state between callers")
	}
}

func TestValidationCatchesBadPacks(t *testing.T) {
	raw, err := fs.ReadFile(packs.FS, "auto.json")
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(map[string]any){
		"not fictional": func(m map[string]any) { m["fictional"] = false },
		"days_late wrong": func(m map[string]any) {
			m["ar"].([]any)[0].(map[string]any)["days_late"] = 3
		},
		"unknown location": func(m map[string]any) {
			m["kpis"].([]any)[0].(map[string]any)["location"] = "L99"
		},
		"zero target": func(m map[string]any) {
			m["kpis"].([]any)[0].(map[string]any)["target"] = 0
		},
		"unknown field": func(m map[string]any) { m["surprise"] = 1 },
		"entry outside period": func(m map[string]any) {
			p := m["payroll"].(map[string]any)
			p["entries"].([]any)[0].(map[string]any)["date"] = "2026-11-01"
		},
	} {
		var m map[string]any
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.UseNumber()
		if err := dec.Decode(&m); err != nil {
			t.Fatal(err)
		}
		mutate(m)
		b, _ := json.Marshal(m)
		if _, err := Decode(b); err == nil {
			t.Errorf("%s: validation passed a bad pack", name)
		}
	}
}

// The public-surface rule: demo data carries no phone numbers, email
// addresses, street addresses, web addresses or private network addresses.
func TestPacksCarryNoContactDetailsOrNetworkAddresses(t *testing.T) {
	bad := []*regexp.Regexp{
		regexp.MustCompile(`\(?\b\d{3}\)?[-. ]\d{3}[-. ]\d{4}\b`),                                    // phone
		regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`),                         // email
		regexp.MustCompile(`(?i)\b\d+\s+\w+\s+(street|st\.|avenue|ave|road|rd\.|blvd|lane|drive)\b`), // street
		regexp.MustCompile(`(?i)https?://|www\.`),                                                    // web
		regexp.MustCompile(`\b(10|127|192\.168|172\.(1[6-9]|2\d|3[01]))\.\d+\.\d+`),                  // private IPs
	}
	for _, id := range []string{"auto", "homecare"} {
		raw, _ := fs.ReadFile(packs.FS, id+".json")
		for _, re := range bad {
			if m := re.Find(raw); m != nil {
				t.Errorf("%s contains %q (%s)", id, m, re)
			}
		}
	}
}
