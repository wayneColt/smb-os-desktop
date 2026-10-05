package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/wayneColt/smb-os-desktop/internal/agents"
	"github.com/wayneColt/smb-os-desktop/internal/llm"
	"github.com/wayneColt/smb-os-desktop/internal/pack"
	"github.com/wayneColt/smb-os-desktop/internal/receipts"
)

// MaxQuestion is the longest question /api/ask accepts, in bytes.
const MaxQuestion = 2000

// AskResult is POST /api/ask.
type AskResult struct {
	Mode    string          `json:"mode"` // deterministic | llm
	Answer  string          `json:"answer"`
	Sources []string        `json:"sources"`
	Note    string          `json:"note,omitempty"`
	Receipt receipts.Record `json:"receipt"`
}

// Ask answers a question about the active pack. With no AI model configured
// the answer comes straight from the data. With a model, the question and a
// summary of the data are sent to it; if that fails, the deterministic
// answer is returned with a note saying so.
func (e *Engine) Ask(ctx context.Context, q string) (*AskResult, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, errf(http.StatusUnprocessableEntity, "bad_question", "ask a question in q")
	}
	if len(q) > MaxQuestion {
		return nil, errf(http.StatusUnprocessableEntity, "bad_question", "questions are limited to %d characters", MaxQuestion)
	}
	e.mu.Lock()
	if err := e.refuseIfLocked(); err != nil {
		e.mu.Unlock()
		return nil, err
	}
	p := e.packs[e.st.Pack]
	pending := e.pendingList()
	answer, sources := deterministicAnswer(p, q, pending)
	var dataJSON []byte
	client := e.opts.LLM
	if client != nil {
		dataJSON, _ = json.Marshal(contextFor(p, pending))
	}
	e.mu.Unlock()

	res := &AskResult{Mode: "deterministic", Answer: answer, Sources: sources}
	summary := "Answered from the data: " + clip(q, 120)
	if client != nil {
		cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		text, err := client.Ask(cctx, systemPrompt(p), "QUESTION:\n"+q+"\n\nDATA (JSON):\n"+string(dataJSON))
		cancel()
		switch {
		case err == nil:
			res.Mode, res.Answer = "llm", text
			res.Sources = append(res.Sources, "data:pack")
			summary = fmt.Sprintf("Answered by the AI model %s (the question and a summary of the business data were sent to the model provider): %s",
				client.Model, clip(q, 120))
		case errors.Is(err, llm.ErrRefused):
			res.Note = "The AI model declined this question, so this answer comes straight from your data."
			summary = "AI model declined; answered from the data: " + clip(q, 120)
		default:
			res.Note = "The AI model could not be reached (" + clip(err.Error(), 160) + "), so this answer comes straight from your data."
			summary = "AI model unavailable; answered from the data: " + clip(q, 120)
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	rec, err := e.log.Append(receipts.Record{Kind: receipts.KindRun, Agent: "ask", Tier: 0, Summary: summary})
	if err != nil {
		return nil, err
	}
	res.Receipt = rec
	return res, nil
}

func (e *Engine) pendingList() []Proposal {
	var out []Proposal
	for _, p := range e.st.Proposals {
		if p.Status == StatusPending {
			out = append(out, *p)
		}
	}
	return out
}

func systemPrompt(p *pack.Pack) string {
	return fmt.Sprintf(`You are the operator's assistant inside SMB OS Desktop for %s, a multi-location franchise business (fictional demo data).
Answer the operator's question using only the DATA provided with it. Quote figures exactly as they appear in the data, with their location.
If the data does not answer the question, say plainly that it is not in the data; never estimate or invent a number.
You cannot take actions; if the operator wants something done, name the agent that does it (for example payroll-submit or send-reminders), and note that money and outward actions need their approval.
Be brief: at most about 120 words, plain sentences, no markdown headings.`, p.Brand)
}

// contextFor is the data summary sent to the AI model with a question.
func contextFor(p *pack.Pack, pending []Proposal) map[string]any {
	brief := agents.RunMorningBrief(p)
	pc := agents.RunPayrollPrecheck(p, "all")
	rc := agents.RunReconcile(p)
	ctx := map[string]any{
		"business":  map[string]any{"brand": p.Brand, "as_of": p.AsOf, "royalty_rate": p.RoyaltyRate, "ad_fund_rate": p.AdFundRate},
		"locations": p.Locations,
		"morning_brief": map[string]any{
			"headline": brief.Headline, "locations": brief.Locations, "variances": brief.Variances,
		},
		"alerts":        p.Alerts,
		"open_invoices": p.AR,
		"payroll_precheck": map[string]any{
			"summary": pc.Summary, "period": pc.Period, "totals": pc.Totals, "issues": pc.Issues,
		},
		"reconciliation": map[string]any{
			"summary": rc.Summary, "exceptions": rc.Exceptions, "suggested_entries": rc.Suggested,
		},
		"pending_approvals": summaries(pending),
	}
	if periods := p.RevenuePeriods(); len(periods) > 0 {
		if r, err := agents.RunFranchisorRollup(p, periods[len(periods)-1]); err == nil {
			ctx["franchise_rollup"] = r
		}
	}
	return ctx
}

func summaries(ps []Proposal) []string {
	out := []string{}
	for _, p := range ps {
		out = append(out, p.Summary)
	}
	return out
}

// deterministicAnswer routes a question to the agents that answer it, by
// keyword, and composes at most two short sections.
func deterministicAnswer(p *pack.Pack, q string, pending []Proposal) (string, []string) {
	words := tokenize(q)
	text := " " + strings.Join(words, " ") + " "
	has := func(stems ...string) bool {
		for _, s := range stems {
			if strings.Contains(text, " "+s) {
				return true
			}
		}
		return false
	}
	loc := ""
	for _, l := range p.Locations {
		if strings.Contains(text, " "+strings.ToLower(l.Name)+" ") || strings.Contains(text, " "+strings.ToLower(l.ID)+" ") {
			loc = l.ID
		}
	}
	var parts, sources []string
	add := func(answer string, src ...string) {
		if len(parts) < 2 && answer != "" {
			parts = append(parts, answer)
			sources = append(sources, src...)
		}
	}

	if has("approv", "pending", "proposal", "waiting for me") {
		if len(pending) == 0 {
			add("Nothing is waiting for your approval.", "approvals")
		} else {
			add(fmt.Sprintf("%s waiting for your approval: %s", plural(len(pending), "act is", "acts are"), strings.Join(summaries(pending), " ")), "approvals")
		}
	}
	if kpiAns, kpiSrc := kpiAnswer(p, text, loc); kpiAns != "" {
		add(kpiAns, kpiSrc...)
	}
	if has("royalt", "ad fund", "franchisor", "rollup", "roll up", "revenue", "sales", "fees") {
		if periods := p.RevenuePeriods(); len(periods) > 0 {
			if r, err := agents.RunFranchisorRollup(p, periods[len(periods)-1]); err == nil {
				var lines []string
				for _, l := range r.Locations {
					lines = append(lines, fmt.Sprintf("%s net sales %s, royalty %s, ad fund %s", l.Name, l.NetSales.String(), l.Royalty.String(), l.AdFund.String()))
				}
				add(r.Summary+" "+strings.Join(lines, "; ")+".", "agent:"+agents.FranchisorRollup)
			}
		}
	}
	if has("payroll", "overtime", "timesheet", "punch", "clock", "hours", "evv", "visit", "paycheck", "pay period") {
		pc := agents.RunPayrollPrecheck(p, orAll(loc))
		ans := pc.Summary
		for i, is := range pc.Issues {
			if i == 3 {
				ans += fmt.Sprintf(" …and %d more in the payroll precheck.", len(pc.Issues)-3)
				break
			}
			ans += fmt.Sprintf(" %s (%s): %s", is.Name, p.LocationName(is.Location), is.Text)
		}
		add(ans, "agent:"+agents.PayrollPrecheck)
	}
	if has("bank", "reconcil", "unmatched", "ledger", "deposit", "statement", "books") {
		rc := agents.RunReconcile(p)
		ans := rc.Summary
		for _, ex := range rc.Exceptions {
			ans += " " + ex.Text
		}
		add(ans, "agent:"+agents.Reconcile)
	}
	if has("late", "owe", "invoice", "receivable", "collect", "overdue", "unpaid", "reminder") || hasWord(words, "ar") {
		var late []string
		var src []string
		for _, inv := range p.AR {
			if loc != "" && inv.Location != loc {
				continue
			}
			if d := p.DaysLate(inv); d > 14 {
				late = append(late, fmt.Sprintf("%s %s (%s, %d days late)", inv.ID, inv.Amount.String(), inv.Customer, d))
				src = append(src, "ar:"+inv.ID)
			}
		}
		if len(late) == 0 {
			add("No invoices are more than 14 days late.", "agent:"+agents.ARReminders)
		} else {
			add(fmt.Sprintf("%s more than 14 days late: %s. The %s agent drafts reminders; sending them needs your approval.",
				plural(len(late), "invoice is", "invoices are"), strings.Join(late, "; "), agents.ARReminders), append(src, "agent:"+agents.ARReminders)...)
		}
	}
	if has("alert", "urgent", "problem", "attention", "critical") {
		var needs []string
		for _, a := range p.Alerts {
			if a.Severity != "info" && (loc == "" || a.Location == loc) {
				needs = append(needs, fmt.Sprintf("%s (%s): %s", p.LocationName(a.Location), a.Severity, a.Text))
			}
		}
		if len(needs) == 0 {
			add("No alerts need action.", "alerts")
		} else {
			add(strings.Join(needs, " "), "alerts")
		}
	}
	if len(parts) == 0 {
		b := agents.RunMorningBrief(p)
		ans := b.Headline
		for _, l := range b.Locations {
			if loc == "" || l.ID == loc {
				ans += " " + l.Line
			}
		}
		add(ans+" You can also ask about payroll, the bank reconciliation, late invoices, royalties, or approvals.", "agent:"+agents.MorningBrief)
	}
	return strings.Join(parts, "\n\n"), sources
}

// kpiAnswer answers questions that name a measure ("car count", "labor").
func kpiAnswer(p *pack.Pack, text, loc string) (string, []string) {
	var lines, src []string
	for _, k := range p.KPIs {
		if loc != "" && k.Location != loc {
			continue
		}
		label := " " + strings.Join(tokenize(k.Label), " ") + " "
		key := " " + strings.ReplaceAll(k.Key, "_", " ") + " "
		if !strings.Contains(text, label) && !strings.Contains(text, key) {
			continue
		}
		word := "on target"
		gap := (k.Value - k.Target)
		if (k.Better == "higher" && gap < 0) || (k.Better == "lower" && gap > 0) {
			word = "off target"
		}
		lines = append(lines, fmt.Sprintf("%s %s: %s against a target of %s (%s, %s).", p.LocationName(k.Location), agents.LowerFirst(k.Label),
			agents.FormatValue(k.Value, k.Unit), agents.FormatValue(k.Target, k.Unit), word, k.Period))
		src = append(src, "kpi:"+k.Location+"."+k.Key)
	}
	return strings.Join(lines, " "), src
}

func tokenize(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

func hasWord(words []string, w string) bool {
	for _, x := range words {
		if x == w {
			return true
		}
	}
	return false
}

func orAll(loc string) string {
	if loc == "" {
		return "all"
	}
	return loc
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
