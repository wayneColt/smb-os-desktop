package agents

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/wayneColt/smb-os-desktop/internal/money"
	"github.com/wayneColt/smb-os-desktop/internal/pack"
)

func load(t *testing.T, id string) *pack.Pack {
	t.Helper()
	p, err := pack.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// ---- morning brief ----

func TestMorningBrief(t *testing.T) {
	for _, tc := range []struct {
		pack, worstLoc, worstKey string
		off                      int
		offByLoc                 map[string]int
	}{
		{"auto", "L27", "comebacks", 7, map[string]int{"L14": 4, "L27": 3}},
		{"homecare", "L3", "new_client_starts", 4, map[string]int{"L3": 2, "L9": 2}},
	} {
		t.Run(tc.pack, func(t *testing.T) {
			b := RunMorningBrief(load(t, tc.pack))
			if len(b.Variances) != 12 {
				t.Fatalf("variances %d", len(b.Variances))
			}
			w := b.Variances[0]
			if w.Location != tc.worstLoc || w.Key != tc.worstKey || w.OnTarget {
				t.Fatalf("worst first: got %s/%s", w.Location, w.Key)
			}
			for i := 1; i < len(b.Variances); i++ {
				if b.Variances[i-1].GapPct > b.Variances[i].GapPct {
					t.Fatalf("not sorted worst first at %d: %v > %v", i, b.Variances[i-1].GapPct, b.Variances[i].GapPct)
				}
			}
			off := 0
			for _, l := range b.Locations {
				if l.OffTarget != tc.offByLoc[l.ID] {
					t.Errorf("%s off target %d want %d", l.ID, l.OffTarget, tc.offByLoc[l.ID])
				}
				off += l.OffTarget
				if !strings.HasPrefix(l.Line, l.Name+" ("+l.Manager+")") {
					t.Errorf("line %q", l.Line)
				}
			}
			if off != tc.off || !strings.Contains(b.Headline, "of 12 measures off target") {
				t.Fatalf("headline %q (off %d)", b.Headline, off)
			}
			if b.Alerts.Crit != 1 || len(b.Alerts.Needs) == 0 || b.Alerts.Needs[0].Severity != "crit" {
				t.Fatalf("alerts %+v", b.Alerts)
			}
		})
	}
}

func TestVarianceDirection(t *testing.T) {
	p := load(t, "auto")
	// Labor is lower-is-better: 31.2 against 28 is worse, 27.4 is better.
	got := map[string]Variance{}
	for _, v := range RunMorningBrief(p).Variances {
		got[v.Location+"."+v.Key] = v
	}
	if v := got["L14.labor_pct"]; v.OnTarget || v.GapPct != -11.4 {
		t.Fatalf("L14 labor %+v", v)
	}
	if v := got["L27.labor_pct"]; !v.OnTarget || v.GapPct != 2.1 {
		t.Fatalf("L27 labor %+v", v)
	}
	if v := got["L14.average_repair_order"]; !v.OnTarget || !strings.Contains(v.Text, "$412.30") {
		t.Fatalf("L14 ARO %+v", v)
	}
}

// ---- payroll precheck ----

type issueKey struct{ person, code, date string }

func issueSet(pc Precheck) []issueKey {
	var out []issueKey
	for _, is := range pc.Issues {
		out = append(out, issueKey{is.Person, is.Code, is.Date})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].person+out[i].date+out[i].code < out[j].person+out[j].date+out[j].code
	})
	return out
}

func TestPayrollPrecheckAuto(t *testing.T) {
	pc := RunPayrollPrecheck(load(t, "auto"), "all")
	want := []issueKey{
		{"E-1402", IssueOvertime, "2026-09-28"},
		{"E-1403", IssueMissingPunch, "2026-09-30"},
		{"E-1404", IssueOvertime, "2026-09-21"},
		{"E-2701", IssueFlaggedUnclocked, "2026-10-02"},
		{"E-2704", IssueNoPunches, "2026-10-01"},
	}
	if got := issueSet(pc); !equalIssues(got, want) {
		t.Fatalf("issues:\n got %v\nwant %v", got, want)
	}
	if pc.Totals.OvertimeHours != 970 || pc.Totals.Blocking != 1 || pc.Ready {
		t.Fatalf("totals %+v ready=%v", pc.Totals, pc.Ready)
	}
	if pc.Issues[0].Severity != SeverityBlock {
		t.Fatal("blocking issues come first")
	}
	byID := map[string]PersonSummary{}
	for _, ps := range pc.People {
		byID[ps.ID] = ps
	}
	if m := byID["E-1402"]; m.OvertimeHours != 620 || m.FlaggedHours == nil || m.EfficiencyPct == nil || m.Status != "review" {
		t.Fatalf("Marco: %+v", m)
	}
	if s := byID["E-1404"]; s.OvertimeHours != 350 || s.FlaggedHours != nil {
		t.Fatalf("Sam (hourly, no flagged hours): %+v", s)
	}
	if byID["E-1403"].Status != "blocked" {
		t.Fatal("a missing punch blocks the person")
	}
	if pc.Totals.Visits != nil || pc.Totals.FlaggedHours == nil {
		t.Fatal("auto reports flagged hours, not visits")
	}
}

func TestPayrollPrecheckHomecare(t *testing.T) {
	pc := RunPayrollPrecheck(load(t, "homecare"), "all")
	want := []issueKey{
		{"C-301", IssueOvertime, "2026-09-28"},
		{"C-303", IssueUnverifiedVisit, "2026-10-01"},
		{"C-303", IssueUnverifiedVisit, "2026-10-02"},
		{"C-902", IssueMissedVisit, "2026-09-30"},
		{"C-904", IssueMissingPunch, "2026-10-02"},
	}
	if got := issueSet(pc); !equalIssues(got, want) {
		t.Fatalf("issues:\n got %v\nwant %v", got, want)
	}
	v := pc.Totals.Visits
	if v == nil || v.Manual != 2 || v.Missed != 1 || v.Incomplete != 1 || v.Verified != v.Scheduled-4 {
		t.Fatalf("visits %+v", v)
	}
	if pc.Totals.EVVVerifiedPct == nil || pc.Totals.OvertimeHours != 350 {
		t.Fatalf("totals %+v", pc.Totals)
	}
	if pc.Totals.FlaggedHours != nil {
		t.Fatal("home care has no flagged hours")
	}
}

func TestPayrollPrecheckOneLocation(t *testing.T) {
	pc := RunPayrollPrecheck(load(t, "auto"), "L27")
	for _, ps := range pc.People {
		if ps.Location != "L27" {
			t.Fatalf("person from %s in an L27 check", ps.Location)
		}
	}
	if len(pc.People) != 4 || len(pc.Issues) != 2 {
		t.Fatalf("people %d issues %d", len(pc.People), len(pc.Issues))
	}
}

func equalIssues(a, b []issueKey) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---- reconcile ----

func TestReconcileFindsExactlyThePlantedMismatches(t *testing.T) {
	for _, tc := range []struct {
		pack            string
		matched         int
		unmatchedBank   []string
		unmatchedLedger []string
		exceptions      map[string]string // type -> "bank/ledger"
		pairs           map[string]string // bank -> ledger that must pair
	}{
		{"auto", 12, []string{"B-08", "B-09", "B-11", "B-12"}, []string{"GL-08", "GL-09", "GL-11"},
			map[string]string{ExTiming: "B-08/GL-08", ExTransposition: "B-09/GL-09", ExDuplicate: "/GL-11", ExBankFee: "B-11/", ExInterest: "B-12/"},
			map[string]string{"B-04": "GL-04", "B-07": "GL-05", "B-06": "GL-07", "B-10": "GL-10"}},
		{"homecare", 10, []string{"B-06", "B-07", "B-08", "B-09"}, []string{"GL-07", "GL-08", "GL-10"},
			map[string]string{ExTransposition: "B-06/GL-07", ExTiming: "B-07/GL-08", ExDuplicate: "/GL-10", ExBankFee: "B-08/", ExInterest: "B-09/"},
			map[string]string{"B-05": "GL-06", "B-10": "GL-05", "B-11": "GL-09"}},
	} {
		t.Run(tc.pack, func(t *testing.T) {
			r := RunReconcile(load(t, tc.pack))
			if len(r.Matched) != tc.matched {
				t.Fatalf("matched %d want %d", len(r.Matched), tc.matched)
			}
			var ub, ul []string
			for _, b := range r.UnmatchedBank {
				ub = append(ub, b.ID)
			}
			for _, l := range r.UnmatchedLedger {
				ul = append(ul, l.ID)
			}
			sort.Strings(ub)
			sort.Strings(ul)
			if strings.Join(ub, ",") != strings.Join(tc.unmatchedBank, ",") || strings.Join(ul, ",") != strings.Join(tc.unmatchedLedger, ",") {
				t.Fatalf("unmatched bank %v ledger %v", ub, ul)
			}
			if len(r.Exceptions) != len(tc.exceptions) {
				t.Fatalf("exceptions %+v", r.Exceptions)
			}
			for _, ex := range r.Exceptions {
				if got := ex.BankID + "/" + ex.LedgerID; tc.exceptions[ex.Type] != got {
					t.Errorf("%s: %s want %s", ex.Type, got, tc.exceptions[ex.Type])
				}
			}
			if len(r.Suggested) != 2 {
				t.Fatalf("suggested %+v", r.Suggested)
			}
			for _, s := range r.Suggested {
				if s.Amount <= 0 || s.Debit == "" || s.Credit == "" {
					t.Errorf("suggestion %+v", s)
				}
			}
			got := map[string]Match{}
			for _, m := range r.Matched {
				got[m.BankID] = m
				if m.DaysApart > MatchWindowDays {
					t.Errorf("%s matched %d days apart", m.BankID, m.DaysApart)
				}
			}
			for b, l := range tc.pairs {
				if got[b].LedgerID != l {
					t.Errorf("%s paired with %q, want %s", b, got[b].LedgerID, l)
				}
			}
			tot := r.Totals
			if tot.BankNet-tot.UnmatchedBank != tot.LedgerNet-tot.UnmatchedLedger {
				t.Errorf("matched sides do not net to the same amount: %+v", tot)
			}
		})
	}
}

func TestReconcileRulesAreEachNecessary(t *testing.T) {
	base := func() *pack.Pack {
		return &pack.Pack{Bank: pack.BankExport{Lines: []pack.BankLine{
			{ID: "B1", Date: "2026-09-10", Amount: 10000, Description: "DEPOSIT REF 42"},
		}}}
	}
	cases := map[string]pack.Ledger{
		"match":        {ID: "G1", Date: "2026-09-13", Amount: 10000, Ref: "REF 42"},
		"amount off":   {ID: "G1", Date: "2026-09-10", Amount: 10001, Ref: "REF 42"},
		"date too far": {ID: "G1", Date: "2026-09-14", Amount: 10000, Ref: "REF 42"},
		"wrong ref":    {ID: "G1", Date: "2026-09-10", Amount: 10000, Ref: "REF 4"},
		"empty ref":    {ID: "G1", Date: "2026-09-10", Amount: 10000, Ref: ""},
	}
	for name, l := range cases {
		p := base()
		p.Ledger = []pack.Ledger{l}
		n := len(RunReconcile(p).Matched)
		if (name == "match") != (n == 1) {
			t.Errorf("%s: matched %d", name, n)
		}
	}
}

func TestTransposed(t *testing.T) {
	for _, tc := range []struct {
		a, b money.Cents
		want bool
	}{
		{-124850, -128450, true},
		{213700, 231700, true},
		{124850, 124805, true},
		{124850, 128540, false}, // two swaps
		{124850, -128450, false},
		{100, 1000, false},
	} {
		if got := transposed(tc.a, tc.b); got != tc.want {
			t.Errorf("transposed(%d,%d)=%v", tc.a, tc.b, got)
		}
	}
}

// ---- AR reminders and send ----

func TestARRemindersDraftsOnlyOver14Days(t *testing.T) {
	for _, tc := range []struct {
		pack   string
		ids    []string
		stages []string
		total  money.Cents
	}{
		{"auto", []string{"inv-1047", "inv-1031", "inv-1049", "inv-1043"}, []string{StageFinal, StageFirm, StageFriendly, StageFriendly}, 411820},
		{"homecare", []string{"inv-9037", "inv-3115", "inv-3108", "inv-9050"}, []string{StageFinal, StageFirm, StageFriendly, StageFriendly}, 584450},
	} {
		t.Run(tc.pack, func(t *testing.T) {
			p := load(t, tc.pack)
			d := RunARReminders(p, 14, nil)
			if len(d.Drafts) != len(tc.ids) || d.Total != tc.total {
				t.Fatalf("drafts %d total %d", len(d.Drafts), d.Total)
			}
			for i, dr := range d.Drafts {
				if dr.Invoice != tc.ids[i] || dr.Stage != tc.stages[i] {
					t.Errorf("draft %d: %s %s", i, dr.Invoice, dr.Stage)
				}
				if dr.DaysLate <= 14 || !strings.Contains(dr.Body, dr.Invoice) || !strings.Contains(dr.Body, dr.Amount.String()) ||
					!strings.Contains(dr.Body, p.Brand) {
					t.Errorf("draft %s body:\n%s", dr.Invoice, dr.Body)
				}
			}
			if len(d.Skipped) != 2 {
				t.Errorf("skipped %v", d.Skipped)
			}
			again := RunARReminders(p, 14, map[string]bool{tc.ids[0]: true})
			if len(again.Drafts) != 3 || len(again.AlreadySent) != 1 {
				t.Errorf("already-sent invoices must not be drafted again: %+v", again.AlreadySent)
			}
		})
	}
}

func TestSendRemindersInstruction(t *testing.T) {
	p := load(t, "auto")
	drafts := RunARReminders(p, 14, nil).Drafts
	all, summary, err := BuildSendReminders(p, drafts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Messages) != 4 || all.Total != 411820 || !strings.Contains(summary, "4 payment reminders") {
		t.Fatalf("%d messages, total %d, %q", len(all.Messages), all.Total, summary)
	}
	one, _, err := BuildSendReminders(p, drafts, []string{"inv-1043"})
	if err != nil || len(one.Messages) != 1 || one.Messages[0].Invoice != "inv-1043" {
		t.Fatalf("subset: %+v %v", one, err)
	}
	if _, _, err := BuildSendReminders(p, drafts, []string{"inv-1055"}); err == nil {
		t.Fatal("an invoice without a draft must be refused")
	}
}

// ---- payroll submit ----

func TestPayrollSubmitInstruction(t *testing.T) {
	for _, id := range []string{"auto", "homecare"} {
		p := load(t, id)
		in, summary := BuildPayrollSubmit(p, "all")
		pc := RunPayrollPrecheck(p, "all")
		if len(in.Rows) != len(pc.People) || in.OvertimeHours != pc.Totals.OvertimeHours || in.OpenIssues != 5 || in.BlockingIssues != 1 {
			t.Fatalf("%s: %+v", id, in)
		}
		if !strings.Contains(summary, "5 issues are still open") {
			t.Fatalf("%s summary %q", id, summary)
		}
		b, _ := json.Marshal(in)
		if !strings.Contains(string(b), `"destination":"outbox`) {
			t.Fatalf("%s: instruction must say where it goes", id)
		}
	}
}

// ---- rollup ----

func TestFranchisorRollup(t *testing.T) {
	for _, tc := range []struct {
		pack                      string
		net, royalty, adFund, due money.Cents
	}{
		{"auto", 14875795, 892548, 297516, 1190064},
		{"homecare", 4837000, 241850, 72555, 314405},
	} {
		p := load(t, tc.pack)
		r, err := RunFranchisorRollup(p, "2026-09")
		if err != nil {
			t.Fatal(err)
		}
		if r.Total.NetSales != tc.net || r.Total.Royalty != tc.royalty || r.Total.AdFund != tc.adFund || r.Total.TotalDue != tc.due {
			t.Fatalf("%s: %+v", tc.pack, r.Total)
		}
		var sum money.Cents
		for _, l := range r.Locations {
			if l.NetSales != l.GrossSales-l.Allowances || l.TotalDue != l.Royalty+l.AdFund || l.ChangePct == nil {
				t.Fatalf("%s line %+v", tc.pack, l)
			}
			sum += l.Royalty
		}
		if sum != r.Total.Royalty || r.PriorPeriod != "2026-08" || r.Total.ChangePct == nil {
			t.Fatalf("%s: total is not the sum of the rounded lines", tc.pack)
		}
		if _, err := RunFranchisorRollup(p, "2025-01"); err == nil {
			t.Fatal("a month with no sales must be an error")
		}
	}
}

// ---- params ----

func TestParamsValidationAndTier(t *testing.T) {
	p := load(t, "auto")
	rec, _ := Find(p, Reconcile)
	prm, err := rec.ParseParams(nil)
	if err != nil || rec.EffectiveTier(prm) != 0 {
		t.Fatalf("default reconcile: %v tier %d", err, rec.EffectiveTier(prm))
	}
	prm, err = rec.ParseParams(map[string]json.RawMessage{"apply": json.RawMessage("true")})
	if err != nil || rec.EffectiveTier(prm) != 1 {
		t.Fatalf("apply:true must be tier 1 (%v)", err)
	}
	for _, bad := range []map[string]json.RawMessage{
		{"apply": json.RawMessage(`"yes"`)},
		{"nope": json.RawMessage("1")},
	} {
		if _, err := rec.ParseParams(bad); err == nil {
			t.Errorf("accepted %v", bad)
		}
	}
	pay, _ := Find(p, PayrollSubmit)
	if _, err := pay.ParseParams(map[string]json.RawMessage{"location": json.RawMessage(`"L99"`)}); err == nil {
		t.Error("location outside the enum accepted")
	}
	send, _ := Find(p, SendReminders)
	a, _ := send.ParseParams(map[string]json.RawMessage{"invoices": json.RawMessage(`["inv-2","inv-1","inv-2"]`)})
	if strings.Join(a.List("invoices"), ",") != "inv-1,inv-2" {
		t.Errorf("lists are normalized so order cannot change a hash: %v", a.List("invoices"))
	}
	tiers := map[string]int{}
	for _, s := range Specs(p) {
		tiers[s.ID] = s.Tier
	}
	want := map[string]int{MorningBrief: 0, PayrollPrecheck: 0, Reconcile: 0, ARReminders: 1, SendReminders: 2, PayrollSubmit: 2, FranchisorRollup: 0}
	for id, tier := range want {
		if tiers[id] != tier {
			t.Errorf("%s tier %d want %d", id, tiers[id], tier)
		}
	}
	if len(tiers) != 7 {
		t.Errorf("%d agents", len(tiers))
	}
}

func TestCapabilitiesCoverEveryAgent(t *testing.T) {
	for _, id := range []string{"auto", "homecare"} {
		p := load(t, id)
		caps := Capabilities(p, false)
		specs := Specs(p)
		if len(caps) != len(specs)+1 || caps[len(caps)-1].Agent != "ask" {
			t.Fatalf("%s: %d capabilities for %d agents", id, len(caps), len(specs))
		}
		for i, s := range specs {
			c := caps[i]
			if c.Agent != s.ID || c.Tier != s.Tier || c.Name != s.Name || len(c.Reads) == 0 || len(c.Writes) == 0 || len(c.Never) == 0 {
				t.Errorf("%s: capability %+v does not describe %s", id, c, s.ID)
			}
			if c.Network != "none" {
				t.Errorf("%s: agent %s claims network %q", id, c.Agent, c.Network)
			}
		}
		if !strings.HasPrefix(caps[len(caps)-1].Network, "none") {
			t.Errorf("ask with the model off: %q", caps[len(caps)-1].Network)
		}
		if on := Capabilities(p, true); !strings.Contains(on[len(on)-1].Network, "ANTHROPIC_API_KEY") {
			t.Errorf("ask with the model on must say data leaves: %q", on[len(on)-1].Network)
		}
	}
}

// The agents are pure functions of the pack: their package cannot reach the
// disk, the network, other processes, the receipt log or the approval gate,
// because it does not import anything that could. Approving and writing the
// outbox live in the engine, behind the typed phrase.
func TestAgentsCannotTouchDiskNetworkOrApprove(t *testing.T) {
	allowed := map[string]bool{
		"encoding/json": true, "fmt": true, "math": true, "sort": true, "strconv": true,
		"strings": true, "time": true, "unicode": true,
		"github.com/wayneColt/smb-os-desktop/internal/money": true,
		"github.com/wayneColt/smb-os-desktop/internal/pack":  true,
	}
	files, _ := filepath.Glob("*.go")
	checked := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		af, err := parser.ParseFile(token.NewFileSet(), f, src, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, im := range af.Imports {
			path := strings.Trim(im.Path.Value, `"`)
			if !allowed[path] {
				t.Errorf("%s imports %s; agents must stay pure (no disk, network, processes, receipts or approvals)", f, path)
			}
		}
		checked++
	}
	if checked < 7 {
		t.Fatalf("only %d source files checked", checked)
	}
}
