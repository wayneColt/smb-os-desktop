package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wayneColt/smb-os-desktop/internal/agents"
	"github.com/wayneColt/smb-os-desktop/internal/llm"
	"github.com/wayneColt/smb-os-desktop/internal/receipts"
)

func testClock() func() time.Time {
	t0 := time.Date(2026, 10, 5, 13, 0, 0, 0, time.UTC)
	n := 0
	return func() time.Time { n++; return t0.Add(time.Duration(n) * time.Second) }
}

func open(t *testing.T, dir string, mod ...func(*Options)) *Engine {
	t.Helper()
	o := Options{DataDir: dir, Pack: "auto", Version: "test", Now: testClock()}
	for _, m := range mod {
		m(&o)
	}
	e, err := Open(o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })
	return e
}

func params(kv ...string) map[string]json.RawMessage {
	m := map[string]json.RawMessage{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = json.RawMessage(kv[i+1])
	}
	return m
}

func outbox(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(filepath.Join(dir, "outbox"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	return names
}

func code(err error) string {
	var ee *Error
	if errors.As(err, &ee) {
		return ee.Code
	}
	return ""
}

func status(err error) int {
	var ee *Error
	if errors.As(err, &ee) {
		return ee.Status
	}
	return 0
}

func propose(t *testing.T, e *Engine, agent string, p map[string]json.RawMessage) Proposal {
	t.Helper()
	res, err := e.Run(agent, p)
	if err != nil {
		t.Fatal(err)
	}
	pr, ok := res.(*ProposalResult)
	if !ok {
		t.Fatalf("%s returned %T, want a proposal", agent, res)
	}
	if pr.Status != "needs_approval" || len(pr.Proposal.Hash) != 64 {
		t.Fatalf("proposal %+v", pr)
	}
	return pr.Proposal
}

func TestTier2NeverExecutesWithoutApproval(t *testing.T) {
	dir := t.TempDir()
	e := open(t, dir)
	if _, err := e.Run(agents.ARReminders, nil); err != nil {
		t.Fatal(err)
	}
	p1 := propose(t, e, agents.PayrollSubmit, nil)
	p2 := propose(t, e, agents.SendReminders, nil)
	if n := len(outbox(t, dir)); n != 0 {
		t.Fatalf("outbox has %d files before any approval", n)
	}
	if got := e.Proposals(); len(got) != 2 {
		t.Fatalf("pending %d", len(got))
	}
	// A wrong phrase, a declined proposal and an unknown hash run nothing.
	if _, err := e.Approve(p1.Hash, "approve"); status(err) != 422 {
		t.Fatalf("lowercase phrase: %v", err)
	}
	if _, err := e.Approve(p1.Hash, ""); status(err) != 422 {
		t.Fatalf("empty phrase: %v", err)
	}
	if _, err := e.Decline(p2.Hash); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Approve(p2.Hash, ApprovePhrase); status(err) != 409 || code(err) != "not_pending" {
		t.Fatalf("approving a declined proposal: %v", err)
	}
	if _, err := e.Approve(strings.Repeat("0", 64), ApprovePhrase); status(err) != 404 {
		t.Fatalf("unknown hash: %v", err)
	}
	if n := len(outbox(t, dir)); n != 0 {
		t.Fatalf("outbox has %d files", n)
	}
	if e.State().Drafts == nil || len(e.State().Drafts) != 4 {
		t.Fatal("drafts must stay until reminders are actually sent")
	}
}

func TestApproveRunsOnceThen409(t *testing.T) {
	dir := t.TempDir()
	e := open(t, dir)
	p := propose(t, e, agents.PayrollSubmit, nil)
	res, err := e.Approve(p.Hash, ApprovePhrase)
	if err != nil {
		t.Fatal(err)
	}
	out := res.Output.(*ExecOutput)
	if res.Status != "done" || len(out.Files) != 2 || res.Receipt.Kind != receipts.KindApprove || res.Receipt.Ref != p.Hash {
		t.Fatalf("approve result %+v", res)
	}
	files := outbox(t, dir)
	if len(files) != 2 {
		t.Fatalf("outbox %v", files)
	}
	// The act wrote into outbox/ and nowhere else: the data folder holds
	// only the outbox, the receipts and the state file.
	ents, _ := os.ReadDir(dir)
	var top []string
	for _, en := range ents {
		top = append(top, en.Name())
	}
	if strings.Join(top, ",") != "outbox,receipts.jsonl,state.json" {
		t.Fatalf("data folder after a tier-2 act: %v", top)
	}
	csv, _ := os.ReadFile(filepath.Join(dir, out.Files[1]))
	if !strings.HasPrefix(string(csv), "period_start,period_end,pay_date,employee_id") || strings.Count(string(csv), "\n") != 9 {
		t.Fatalf("payroll CSV:\n%s", csv)
	}
	if _, err := e.Approve(p.Hash, ApprovePhrase); status(err) != 409 || code(err) != "already_used" {
		t.Fatalf("second approve: %v", err)
	}
	// Asking for the identical act again is refused too: it already ran.
	if _, err := e.Run(agents.PayrollSubmit, nil); status(err) != 409 || code(err) != "already_used" {
		t.Fatalf("re-proposing a used instruction: %v", err)
	}
	if len(outbox(t, dir)) != 2 {
		t.Fatal("the act ran more than once")
	}
}

func TestSameInstructionSameProposalChangedInstructionNewHash(t *testing.T) {
	e := open(t, t.TempDir())
	a := propose(t, e, agents.PayrollSubmit, nil)
	b := propose(t, e, agents.PayrollSubmit, params("location", `"all"`))
	if a.Hash != b.Hash || len(e.Proposals()) != 1 {
		t.Fatal("the same instruction must give the same pending proposal")
	}
	c := propose(t, e, agents.PayrollSubmit, params("location", `"L14"`))
	if c.Hash == a.Hash {
		t.Fatal("a changed instruction must get a new hash")
	}
	if _, err := e.Run(agents.ARReminders, nil); err != nil {
		t.Fatal(err)
	}
	all := propose(t, e, agents.SendReminders, nil)
	one := propose(t, e, agents.SendReminders, params("invoices", `["inv-1043"]`))
	oneAgain := propose(t, e, agents.SendReminders, params("invoices", `["inv-1043","inv-1043"]`))
	if all.Hash == one.Hash || one.Hash != oneAgain.Hash {
		t.Fatal("subsets hash differently; repeats do not")
	}
	// The hash is sha256 of the canonical instruction, recomputable by anyone.
	if canonHashRaw(c.Instruction) != c.Hash {
		t.Fatal("proposal hash is not sha256(canonical instruction)")
	}
}

func TestSendRemindersFlow(t *testing.T) {
	dir := t.TempDir()
	e := open(t, dir)
	if _, err := e.Run(agents.SendReminders, nil); code(err) != "no_drafts" {
		t.Fatalf("send with no drafts: %v", err)
	}
	if _, err := e.Run(agents.ARReminders, nil); err != nil {
		t.Fatal(err)
	}
	p := propose(t, e, agents.SendReminders, params("invoices", `["inv-1043","inv-1049"]`))
	res, err := e.Approve(p.Hash, ApprovePhrase)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, res.Output.(*ExecOutput).Files[0]))
	if !strings.Contains(string(b), "Nothing left this computer") || !strings.Contains(string(b), "inv-1049") {
		t.Fatalf("outbox file:\n%s", b)
	}
	st := e.State()
	if len(st.Drafts) != 2 || len(st.Sent) != 2 {
		t.Fatalf("after sending 2 of 4: drafts %d sent %d", len(st.Drafts), len(st.Sent))
	}
	// Re-drafting leaves out what was already sent.
	r, _ := e.Run(agents.ARReminders, nil)
	if d := r.(*RunResult).Output.(agents.Drafting); len(d.Drafts) != 2 || len(d.AlreadySent) != 2 {
		t.Fatalf("re-draft: %d drafts, %v already sent", len(d.Drafts), d.AlreadySent)
	}
}

func TestApproveRefusesTamperedInstruction(t *testing.T) {
	dir := t.TempDir()
	e := open(t, dir)
	p := propose(t, e, agents.PayrollSubmit, nil)
	e.Close()

	// Someone edits the stored instruction after it was proposed: the
	// overtime total is doubled.
	path := filepath.Join(dir, "state.json")
	raw, _ := os.ReadFile(path)
	var st map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&st); err != nil {
		t.Fatal(err)
	}
	ins := st["proposals"].(map[string]any)[p.Hash].(map[string]any)["instruction"].(map[string]any)
	if ins["overtime_hours"] != json.Number("9.70") {
		t.Fatalf("test setup: overtime_hours is %v", ins["overtime_hours"])
	}
	ins["overtime_hours"] = json.Number("19.40")
	edited, _ := json.Marshal(st)
	if err := os.WriteFile(path, edited, 0o600); err != nil {
		t.Fatal(err)
	}
	e2 := open(t, dir)
	if _, err := e2.Approve(p.Hash, ApprovePhrase); status(err) != 409 || code(err) != "hash_mismatch" {
		t.Fatalf("tampered instruction: %v", err)
	}
	if n := len(outbox(t, dir)); n != 0 {
		t.Fatalf("a tampered instruction executed (%d outbox files)", n)
	}
}

func TestUsedHashesAreRebuiltFromReceipts(t *testing.T) {
	dir := t.TempDir()
	e := open(t, dir)
	p := propose(t, e, agents.PayrollSubmit, nil)
	if _, err := e.Approve(p.Hash, ApprovePhrase); err != nil {
		t.Fatal(err)
	}
	e.Close()
	if err := os.Remove(filepath.Join(dir, "state.json")); err != nil {
		t.Fatal(err)
	}
	e2 := open(t, dir)
	if _, err := e2.Run(agents.PayrollSubmit, nil); code(err) != "already_used" {
		t.Fatalf("with state.json gone, the chain must still stop a second run: %v", err)
	}
}

func TestBrokenChainFreezesApprovals(t *testing.T) {
	dir := t.TempDir()
	e := open(t, dir)
	if _, err := e.Run(agents.MorningBrief, nil); err != nil {
		t.Fatal(err)
	}
	p := propose(t, e, agents.PayrollSubmit, nil)
	path := filepath.Join(dir, "receipts.jsonl")
	raw, _ := os.ReadFile(path)
	if err := os.WriteFile(path, []byte(strings.Replace(string(raw), `"tier":0`, `"tier":1`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(3 * time.Second)
	_ = os.Chtimes(path, future, future)
	if h := e.Health(); h.ChainOK {
		t.Fatal("health still reports chain_ok after tampering")
	}
	if _, err := e.Approve(p.Hash, ApprovePhrase); code(err) != "chain_broken" {
		t.Fatalf("approval with a broken chain: %v", err)
	}
	if len(outbox(t, dir)) != 0 {
		t.Fatal("executed despite a broken chain")
	}
}

func TestEveryActionWritesAReceipt(t *testing.T) {
	e := open(t, t.TempDir())
	steps := []func() error{
		func() error { _, err := e.Run(agents.MorningBrief, nil); return err },
		func() error { _, err := e.Run(agents.Reconcile, params("apply", "true")); return err },
		func() error { _, err := e.SwitchPack("homecare"); return err },
		func() error { _, err := e.Run(agents.PayrollSubmit, nil); return err },
		func() error { _, err := e.Ask(context.Background(), "how is payroll?"); return err },
	}
	for i, s := range steps {
		if err := s(); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	page, _ := e.Receipts(10)
	if !page.ChainOK || page.Count != 5 {
		t.Fatalf("receipts %+v", page)
	}
	var kinds []string
	for _, r := range page.Records {
		var rec receipts.Record
		_ = json.Unmarshal(r, &rec)
		kinds = append(kinds, rec.Kind+":"+rec.Agent)
	}
	want := "run:ask,proposal:payroll-submit,pack:settings,run:reconcile,run:morning-brief"
	if strings.Join(kinds, ",") != want {
		t.Fatalf("newest first: %s", strings.Join(kinds, ","))
	}
}

func TestPackSwitchPersistsAndForcePackWins(t *testing.T) {
	dir := t.TempDir()
	e := open(t, dir)
	st, err := e.SwitchPack("homecare")
	if err != nil || st.Pack.ID != "homecare" || st.Brand != "Larkmoor Home Care" || st.Receipt == nil ||
		st.Receipt.Tier != 1 || st.Receipt.Kind != receipts.KindPack || st.RunID == "" {
		t.Fatalf("switch: %v %+v", err, st.Receipt)
	}
	if _, err := e.SwitchPack("bakery"); status(err) != 422 {
		t.Fatalf("unknown pack: %v", err)
	}
	if e.Health().Pack != "homecare" {
		t.Fatal("health does not show the active pack")
	}
	specs := e.Specs()
	for _, s := range specs {
		if s.ID == agents.PayrollSubmit && !strings.Contains(strings.Join(s.Params[0].Enum, ","), "L9") {
			t.Fatal("agent params must follow the active pack")
		}
	}
	e.Close()
	if open(t, dir).State().Pack.ID != "homecare" {
		t.Fatal("the saved pack must survive a restart")
	}
	forced := open(t, dir, func(o *Options) { o.Pack, o.ForcePack = "auto", true })
	if forced.State().Pack.ID != "auto" {
		t.Fatal("--pack given explicitly must win")
	}
}

func TestUndoTier1(t *testing.T) {
	e := open(t, t.TempDir())
	r1, err := e.Run(agents.ARReminders, nil)
	if err != nil {
		t.Fatal(err)
	}
	run := r1.(*RunResult)
	if run.Tier != 1 || len(e.State().Drafts) != 4 {
		t.Fatalf("tier %d drafts %d", run.Tier, len(e.State().Drafts))
	}
	r2, _ := e.Run(agents.ARReminders, params("over_days", "30"))
	if _, err := e.Undo(run.RunID); code(err) != "superseded" {
		t.Fatalf("undoing an overwritten change: %v", err)
	}
	if _, err := e.Undo(r2.(*RunResult).RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Undo(run.RunID); err != nil {
		t.Fatal(err)
	}
	if n := len(e.State().Drafts); n != 0 {
		t.Fatalf("after undoing both, %d drafts remain", n)
	}
	if _, err := e.Undo(run.RunID); code(err) != "already_undone" {
		t.Fatalf("double undo: %v", err)
	}
	rec, _ := e.Run(agents.Reconcile, params("apply", "true"))
	if len(e.State().SavedMatches) != 12 {
		t.Fatal("reconcile apply saves matches")
	}
	if _, err := e.Undo(rec.(*RunResult).RunID); err != nil || len(e.State().SavedMatches) != 0 {
		t.Fatalf("undo reconcile: %v", err)
	}
	if _, err := e.Undo("run-unknown"); status(err) != 404 {
		t.Fatalf("unknown run: %v", err)
	}
}

func TestRunErrors(t *testing.T) {
	e := open(t, t.TempDir())
	if _, err := e.Run("make-coffee", nil); status(err) != 404 {
		t.Fatalf("unknown agent: %v", err)
	}
	if _, err := e.Run(agents.Reconcile, params("apply", `"yes"`)); status(err) != 422 || code(err) != "bad_param" {
		t.Fatalf("bad param: %v", err)
	}
	if _, err := e.Run(agents.FranchisorRollup, params("period", `"1999-01"`)); status(err) != 422 {
		t.Fatalf("period not in the enum: %v", err)
	}
}

func TestAskDeterministic(t *testing.T) {
	e := open(t, t.TempDir())
	for q, want := range map[string]string{
		"How much do we owe the franchisor in royalties?": "agent:franchisor-rollup",
		"Any overtime this pay period?":                   "agent:payroll-precheck",
		"Why doesn't the bank match the books?":           "agent:reconcile",
		"Which invoices are late?":                        "agent:ar-reminders",
		"What's the car count at Hillcrest?":              "kpi:L27.car_count",
		"Good morning":                                    "agent:morning-brief",
	} {
		res, err := e.Ask(context.Background(), q)
		if err != nil {
			t.Fatal(err)
		}
		if res.Mode != "deterministic" || !strings.Contains(strings.Join(res.Sources, " "), want) || res.Answer == "" {
			t.Errorf("%q: mode %s sources %v", q, res.Mode, res.Sources)
		}
	}
	res, _ := e.Ask(context.Background(), "What's the car count at Hillcrest?")
	if !strings.Contains(res.Answer, "Hillcrest car count: 47") || strings.Contains(res.Answer, "Riverside") {
		t.Errorf("answer %q", res.Answer)
	}
	if _, err := e.Ask(context.Background(), "   "); status(err) != 422 {
		t.Fatal("empty question accepted")
	}
}

func TestAskWithModelUsesTheModelAndFallsBack(t *testing.T) {
	var gotBody map[string]any
	var gotKey, gotVersion string
	fail := false
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey, gotVersion = r.Header.Get("x-api-key"), r.Header.Get("anthropic-version")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		if fail {
			w.WriteHeader(529)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"content":[{"type":"thinking","thinking":""},{"type":"text","text":"Royalty due is $11,900.64."}],"stop_reason":"end_turn"}`))
	}))
	defer fake.Close()
	client := &llm.Client{BaseURL: fake.URL, APIKey: "test-key-not-real", Model: "claude-sonnet-5-5", MaxTokens: 512, HTTP: fake.Client()} // firewall:allow=secret
	e := open(t, t.TempDir(), func(o *Options) { o.LLM = client })
	if h := e.Health(); h.LLM != "on" || h.Model != "claude-sonnet-5-5" {
		t.Fatalf("health %+v", h)
	}
	res, err := e.Ask(context.Background(), "What do we owe in royalties?")
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != "llm" || res.Answer != "Royalty due is $11,900.64." || !strings.Contains(res.Receipt.Summary, "sent to the model provider") {
		t.Fatalf("llm answer %+v", res)
	}
	if gotKey != "test-key-not-real" || gotVersion != "2023-06-01" || gotBody["model"] != "claude-sonnet-5-5" {
		t.Fatalf("request key=%q version=%q model=%v", gotKey, gotVersion, gotBody["model"])
	}
	msgs := gotBody["messages"].([]any)
	if !strings.Contains(msgs[0].(map[string]any)["content"].(string), "Kestrel Auto Care") {
		t.Fatal("the model was not given the business data")
	}
	fail = true
	res, err = e.Ask(context.Background(), "What do we owe in royalties?")
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != "deterministic" || !strings.Contains(res.Note, "could not be reached") || strings.Contains(res.Note, "test-key-not-real") {
		t.Fatalf("fallback %+v", res)
	}
}

func TestUndoPackSwitchAndNonTier1Runs(t *testing.T) {
	e := open(t, t.TempDir())
	st, err := e.SwitchPack("homecare")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Undo(st.RunID); err != nil {
		t.Fatal(err)
	}
	if got := e.State().Pack.ID; got != "auto" {
		t.Fatalf("undoing the switch left pack %s", got)
	}
	if e.State().RunID != "" || e.State().Receipt != nil {
		t.Fatal("GET state must not carry a run id")
	}
	brief, _ := e.Run(agents.MorningBrief, nil)
	if _, err := e.Undo(brief.(*RunResult).RunID); status(err) != 422 || code(err) != "not_undoable" {
		t.Fatalf("undo of a tier-0 run: %v", err)
	}
	p := propose(t, e, agents.PayrollSubmit, nil)
	if _, err := e.Undo(p.Hash); status(err) != 422 {
		t.Fatalf("undo of a tier-2 proposal: %v", err)
	}
}

func TestLockRefusesRunsApprovalsUndoAndAsk(t *testing.T) {
	dir := t.TempDir()
	e := open(t, dir)
	drafts, _ := e.Run(agents.ARReminders, nil)
	p := propose(t, e, agents.PayrollSubmit, nil)
	q := propose(t, e, agents.SendReminders, nil)
	res, err := e.SetLock(true)
	if err != nil || !res.Locked || res.Receipt.Kind != receipts.KindLock || res.Receipt.Tier != 1 {
		t.Fatalf("lock: %v %+v", err, res)
	}
	if !e.Health().Locked {
		t.Fatal("health does not report the lock")
	}
	for name, err := range map[string]error{
		"run tier 0": func() error { _, err := e.Run(agents.MorningBrief, nil); return err }(),
		"run tier 2": func() error { _, err := e.Run(agents.PayrollSubmit, params("location", `"L27"`)); return err }(),
		"approve":    func() error { _, err := e.Approve(p.Hash, ApprovePhrase); return err }(),
		"undo":       func() error { _, err := e.Undo(drafts.(*RunResult).RunID); return err }(),
		"ask":        func() error { _, err := e.Ask(context.Background(), "how is payroll?"); return err }(),
	} {
		if status(err) != 423 || code(err) != "locked" {
			t.Errorf("%s while locked: %v", name, err)
		}
	}
	if len(outbox(t, dir)) != 0 {
		t.Fatal("something executed while locked")
	}
	stillPending := false
	for _, pr := range e.Proposals() {
		stillPending = stillPending || pr.Hash == p.Hash
	}
	if !stillPending {
		t.Fatal("a refused approval must leave the proposal pending")
	}
	if _, err := e.Decline(q.Hash); err != nil {
		t.Fatalf("declining stays possible while locked: %v", err)
	}
	e.Close()
	e2 := open(t, dir)
	if !e2.Health().Locked {
		t.Fatal("the lock must survive a restart")
	}
	if res, err := e2.SetLock(false); err != nil || res.Locked || res.Receipt.Kind != receipts.KindUnlock {
		t.Fatalf("unlock: %v %+v", err, res)
	}
	if _, err := e2.Approve(p.Hash, ApprovePhrase); err != nil {
		t.Fatalf("approve after unlock: %v", err)
	}
}

func TestHealthReportsDataDir(t *testing.T) {
	dir := t.TempDir()
	h := open(t, dir).Health()
	if h.DataDir != dir || !filepath.IsAbs(h.DataDir) || h.Locked {
		t.Fatalf("health %+v", h)
	}
}

// countingTransport fails every request and counts the attempts.
type countingTransport struct{ n int }

func (c *countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	c.n++
	return nil, errors.New("network use is not allowed in this test")
}

func TestNoNetworkWithoutAPIKey(t *testing.T) {
	ct := &countingTransport{}
	old := http.DefaultTransport
	http.DefaultTransport = ct
	t.Cleanup(func() { http.DefaultTransport = old })

	if llm.FromEnv(func(string) string { return "" }) != nil {
		t.Fatal("without ANTHROPIC_API_KEY there must be no model client")
	}
	dir := t.TempDir()
	e := open(t, dir)
	if h := e.Health(); h.LLM != "off" {
		t.Fatalf("llm %q", h.LLM)
	}
	for _, id := range []string{"auto", "homecare"} {
		if _, err := e.SwitchPack(id); err != nil {
			t.Fatal(err)
		}
		for _, a := range []string{agents.MorningBrief, agents.PayrollPrecheck, agents.Reconcile, agents.FranchisorRollup, agents.ARReminders} {
			if _, err := e.Run(a, nil); err != nil {
				t.Fatal(err)
			}
		}
		for _, a := range []string{agents.SendReminders, agents.PayrollSubmit} {
			p := propose(t, e, a, nil)
			if _, err := e.Approve(p.Hash, ApprovePhrase); err != nil {
				t.Fatal(err)
			}
		}
		if res, err := e.Ask(context.Background(), "what do we owe the franchisor?"); err != nil || res.Mode != "deterministic" {
			t.Fatalf("ask: %v %+v", err, res)
		}
	}
	if ct.n != 0 {
		t.Fatalf("%d network request(s) were attempted with the AI model off", ct.n)
	}
}
