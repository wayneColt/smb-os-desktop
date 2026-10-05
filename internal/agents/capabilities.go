package agents

import "github.com/wayneColt/smb-os-desktop/internal/pack"

// Capability says what one agent may read, may write, and never touches. It
// is a statement of what the code does, kept next to it; the tests check it
// against the agents' behavior where that can be checked mechanically.
type Capability struct {
	Agent   string   `json:"agent"`
	Name    string   `json:"name"`
	Tier    int      `json:"tier"`
	Reads   []string `json:"reads"`
	Writes  []string `json:"writes"`
	Never   []string `json:"never"`
	Network string   `json:"network"`
}

// Things no agent ever does in this build.
var neverOutward = []string{
	"the internet or any other computer",
	"your bank, payroll, scheduling or accounting systems",
	"files outside the data folder",
}

// Capabilities lists every agent plus Ask, in catalog order. llmOn reports
// whether ANTHROPIC_API_KEY was set, which is the only thing that lets any
// data leave the computer (and only through Ask).
func Capabilities(p *pack.Pack, llmOn bool) []Capability {
	timesheets := "timesheets and clock punches"
	if p.Payroll.Basis == "evv_visits" {
		timesheets = "visit schedule and EVV records"
	}
	receipt := "a receipt"
	never := func(extra ...string) []string { return append(append([]string{}, extra...), neverOutward...) }
	caps := map[string]Capability{
		MorningBrief: {Reads: []string{"KPIs and targets", "alerts", "open invoices"}, Writes: []string{receipt},
			Never: never("any business data (read only)")},
		PayrollPrecheck: {Reads: []string{"pay-period " + timesheets, "staff list and pay types"}, Writes: []string{receipt},
			Never: never("timesheets (read only)", "the payroll export")},
		Reconcile: {Reads: []string{"bank export", "ledger"}, Writes: []string{receipt, "saved matches (only with apply, undoable)"},
			Never: never("ledger entries (it suggests entries, it does not post them)", "the bank export")},
		ARReminders: {Reads: []string{"open invoices", "reminders already sent"}, Writes: []string{receipt, "reminder drafts (undoable)"},
			Never: never("customers (drafts are not sent)", "invoice amounts")},
		SendReminders: {Reads: []string{"saved reminder drafts"}, Writes: []string{"a proposal and its receipt", "after your typed APPROVE: one outbox file, once"},
			Never: never("anything before you approve", "an instruction that changed after it was proposed")},
		PayrollSubmit: {Reads: []string{"payroll precheck results"}, Writes: []string{"a proposal and its receipt", "after your typed APPROVE: the payroll export in the outbox, once"},
			Never: never("anything before you approve", "an instruction that changed after it was proposed")},
		FranchisorRollup: {Reads: []string{"monthly sales", "royalty and ad fund rates"}, Writes: []string{receipt},
			Never: never("any business data (read only)")},
	}
	var out []Capability
	for _, s := range Specs(p) {
		c := caps[s.ID]
		c.Agent, c.Name, c.Tier, c.Network = s.ID, s.Name, s.Tier, "none"
		out = append(out, c)
	}
	ask := Capability{Agent: "ask", Name: "Ask", Tier: 0,
		Reads:  []string{"the same data the agents read"},
		Writes: []string{receipt},
		Never:  []string{"any business data (read only)", "files outside the data folder"},
	}
	if llmOn {
		ask.Network = "the AI model provider: your question and a summary of the business data, because ANTHROPIC_API_KEY is set"
	} else {
		ask.Network = "none (ANTHROPIC_API_KEY is not set, so answers come straight from your data)"
		ask.Never = append(ask.Never, "the internet or any other computer")
	}
	return append(out, ask)
}
