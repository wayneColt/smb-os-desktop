package engine

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/wayneColt/smb-os-desktop/internal/agents"
)

// outboxNote is written into every outbox file so nobody mistakes it for
// something that was sent.
const outboxNote = "Demo: this act was approved and written here instead of being sent. Nothing left this computer."

// ExecOutput is what an approved act returns.
type ExecOutput struct {
	Action  string   `json:"action"`
	Files   []string `json:"files"`
	Summary string   `json:"summary"`
	Note    string   `json:"note"`
}

// execute performs an approved, hash-checked proposal. The caller holds e.mu.
func (e *Engine) execute(prop *Proposal) (*ExecOutput, string, error) {
	stamp := e.opts.Now().UTC().Format("20060102T150405Z")
	base := fmt.Sprintf("%s_%s_%s", stamp, prop.Agent, short(prop.Hash))
	envelope := func(extra map[string]any) ([]byte, error) {
		m := map[string]any{
			"note":        outboxNote,
			"proposal":    prop.Hash,
			"agent":       prop.Agent,
			"approved_at": prop.Decided,
			"summary":     prop.Summary,
			"instruction": prop.Instruction,
		}
		for k, v := range extra {
			m[k] = v
		}
		return json.MarshalIndent(m, "", "  ")
	}
	switch prop.Agent {
	case agents.SendReminders:
		var in agents.ReminderInstruction
		if err := json.Unmarshal(prop.Instruction, &in); err != nil {
			return nil, "", err
		}
		name := base + ".json"
		b, err := envelope(nil)
		if err != nil {
			return nil, "", err
		}
		if err := writeFileAtomic(filepath.Join(e.dir, outboxDir, name), b); err != nil {
			return nil, "", err
		}
		rel := outboxDir + "/" + name
		prop.Outbox = []string{rel}
		// The reminders are out: their drafts are done, and the invoices are
		// not drafted again.
		sentInv := map[string]bool{}
		for _, m := range in.Messages {
			sentInv[m.Invoice] = true
			e.st.Sent[in.Pack] = append(e.st.Sent[in.Pack], SentReminder{
				Invoice: m.Invoice, Draft: m.Draft, Proposal: prop.Hash, Sent: prop.Decided, File: rel})
		}
		var keep []agents.Draft
		for _, d := range e.st.Drafts[in.Pack] {
			if !sentInv[d.Invoice] {
				keep = append(keep, d)
			}
		}
		e.st.Drafts[in.Pack] = keep
		summary := fmt.Sprintf("Sent %d payment reminders (%s) to the outbox: %s. Nothing left this computer.",
			len(in.Messages), in.Total.String(), rel)
		return &ExecOutput{Action: in.Action, Files: prop.Outbox, Summary: summary, Note: outboxNote}, summary, nil

	case agents.PayrollSubmit:
		var in agents.PayrollInstruction
		if err := json.Unmarshal(prop.Instruction, &in); err != nil {
			return nil, "", err
		}
		jsonName, csvName := base+".json", base+".csv"
		b, err := envelope(nil)
		if err != nil {
			return nil, "", err
		}
		c, err := payrollCSV(in)
		if err != nil {
			return nil, "", err
		}
		if err := writeFileAtomic(filepath.Join(e.dir, outboxDir, jsonName), b); err != nil {
			return nil, "", err
		}
		if err := writeFileAtomic(filepath.Join(e.dir, outboxDir, csvName), c); err != nil {
			return nil, "", err
		}
		prop.Outbox = []string{outboxDir + "/" + jsonName, outboxDir + "/" + csvName}
		summary := fmt.Sprintf("Payroll export for %s to %s written to the outbox (%d people, %s regular h, %s overtime h): %s. Nothing left this computer.",
			in.Period.Start, in.Period.End, len(in.Rows), in.RegularHours.String(), in.OvertimeHours.String(), prop.Outbox[1])
		return &ExecOutput{Action: in.Action, Files: prop.Outbox, Summary: summary, Note: outboxNote}, summary, nil
	}
	return nil, "", fmt.Errorf("no executor for %s", prop.Agent)
}

func payrollCSV(in agents.PayrollInstruction) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"period_start", "period_end", "pay_date", "employee_id", "name", "location", "pay_type",
		"regular_hours", "overtime_hours", "flagged_hours", "visits_verified", "open_issues"})
	for _, r := range in.Rows {
		flagged, visits := "", ""
		if r.FlaggedHours != nil {
			b, _ := r.FlaggedHours.MarshalJSON()
			flagged = string(b)
		}
		if r.VisitsVerified != nil {
			visits = strconv.Itoa(*r.VisitsVerified)
		}
		reg, _ := r.RegularHours.MarshalJSON()
		ot, _ := r.OvertimeHours.MarshalJSON()
		_ = w.Write([]string{in.Period.Start, in.Period.End, in.Period.PayDate, r.Person, r.Name, r.Location, r.Pay,
			string(reg), string(ot), flagged, visits, strconv.Itoa(r.OpenIssues)})
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}
