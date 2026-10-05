// Package engine is the aios core: it holds the active pack and the
// operator's local state, runs agents behind the tier gate, writes receipts,
// and executes approved tier-2 acts into the outbox.
//
// Tier 0 (read and compute) and tier 1 (reversible local change) run on
// request. Tier 2 (money, outward, irreversible) never runs on request: it
// returns a proposal bound to sha256(canonical instruction) and runs only
// when that exact hash is approved with the typed phrase APPROVE, at most
// once. Approval executes the stored instruction, after re-checking that it
// still hashes to the approved value.
package engine

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/wayneColt/smb-os-desktop/internal/agents"
	"github.com/wayneColt/smb-os-desktop/internal/canon"
	"github.com/wayneColt/smb-os-desktop/internal/llm"
	"github.com/wayneColt/smb-os-desktop/internal/pack"
	"github.com/wayneColt/smb-os-desktop/internal/receipts"
)

// ApprovePhrase is the phrase an operator types to approve a tier-2 act.
const ApprovePhrase = "APPROVE"

// Proposal statuses.
const (
	StatusPending  = "pending"
	StatusUsed     = "used"
	StatusDeclined = "declined"
)

const (
	stateFile    = "state.json"
	receiptsFile = "receipts.jsonl"
	outboxDir    = "outbox"
	maxUndo      = 100
)

// Error is an engine error with the HTTP status and code the API reports.
type Error struct {
	Status int
	Code   string
	Msg    string
}

func (e *Error) Error() string { return e.Msg }

func errf(status int, code, f string, a ...any) *Error {
	return &Error{Status: status, Code: code, Msg: fmt.Sprintf(f, a...)}
}

// Options configure an Engine.
type Options struct {
	DataDir   string
	Pack      string // pack to use when no saved state exists (or when ForcePack)
	ForcePack bool   // --pack given explicitly: it wins over the saved state
	Version   string
	LLM       *llm.Client // nil: AI model off
	Now       func() time.Time
}

// Proposal is a tier-2 act waiting for (or past) approval.
type Proposal struct {
	Hash        string          `json:"hash"`
	Agent       string          `json:"agent"`
	Tier        int             `json:"tier"`
	Pack        string          `json:"pack"`
	Summary     string          `json:"summary"`
	Instruction json.RawMessage `json:"instruction"`
	Created     string          `json:"created"`
	Status      string          `json:"status"`
	Decided     string          `json:"decided,omitempty"`
	Outbox      []string        `json:"outbox,omitempty"`
}

// SentReminder records a reminder that went out (to the outbox).
type SentReminder struct {
	Invoice  string `json:"invoice"`
	Draft    string `json:"draft"`
	Proposal string `json:"proposal"`
	Sent     string `json:"sent"`
	File     string `json:"file"`
}

// UndoEntry remembers what a tier-1 run replaced.
type UndoEntry struct {
	RunID     string          `json:"run_id"`
	Agent     string          `json:"agent"`
	Pack      string          `json:"pack"`
	Component string          `json:"component"` // drafts | matches
	Before    json.RawMessage `json:"before"`
	AfterHash string          `json:"after_hash"`
	Created   string          `json:"created"`
	Undone    bool            `json:"undone,omitempty"`
}

// state is everything aios keeps besides receipts, persisted as state.json.
type state struct {
	Version   int                       `json:"version"`
	Pack      string                    `json:"pack"`
	Drafts    map[string][]agents.Draft `json:"drafts"`
	Matches   map[string][]agents.Match `json:"matches"`
	Sent      map[string][]SentReminder `json:"sent"`
	Proposals map[string]*Proposal      `json:"proposals"`
	Undo      []UndoEntry               `json:"undo"`
	Locked    bool                      `json:"locked"`
}

// Engine is safe for concurrent use.
type Engine struct {
	mu       sync.Mutex
	opts     Options
	dir      string
	log      *receipts.Log
	st       *state
	packs    map[string]*pack.Pack
	instance string
}

// Open loads (or creates) the data directory and returns a ready engine.
func Open(opts Options) (*Engine, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Pack == "" {
		opts.Pack = "auto"
	}
	if !pack.Exists(opts.Pack) {
		return nil, fmt.Errorf("unknown pack %q (choose auto or homecare)", opts.Pack)
	}
	dir := opts.DataDir
	if err := os.MkdirAll(filepath.Join(dir, outboxDir), 0o700); err != nil {
		return nil, fmt.Errorf("data folder %s: %w", dir, err)
	}
	e := &Engine{opts: opts, dir: dir, packs: map[string]*pack.Pack{}}
	for _, info := range []string{"auto", "homecare"} {
		p, err := pack.Load(info)
		if err != nil {
			return nil, err
		}
		e.packs[info] = p
	}
	st, err := loadState(filepath.Join(dir, stateFile))
	if err != nil {
		return nil, err
	}
	if st.Pack == "" || opts.ForcePack || !pack.Exists(st.Pack) {
		st.Pack = opts.Pack
	}
	e.st = st
	if e.log, err = receipts.Open(filepath.Join(dir, receiptsFile), opts.Now); err != nil {
		return nil, fmt.Errorf("receipts: %w", err)
	}
	// The chain is the record of what ran. If state.json was lost or edited,
	// every proposal the chain says was approved is marked used again, so a
	// hash can never run twice.
	_ = e.log.Each(func(r receipts.Record) {
		if r.Kind != receipts.KindApprove || r.Ref == "" {
			return
		}
		p, ok := e.st.Proposals[r.Ref]
		if !ok {
			p = &Proposal{Hash: r.Ref, Agent: r.Agent, Tier: 2, Summary: "(restored from receipts)", Created: r.TS}
			e.st.Proposals[r.Ref] = p
		}
		if p.Status != StatusUsed {
			p.Status, p.Decided = StatusUsed, r.TS
		}
	})
	if err := e.save(); err != nil {
		e.log.Close()
		return nil, err
	}
	var b [8]byte
	_, _ = rand.Read(b[:])
	e.instance = hex.EncodeToString(b[:])
	return e, nil
}

// Close releases the receipt log.
func (e *Engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.log.Close()
}

// DataDir returns the data directory.
func (e *Engine) DataDir() string { return e.dir }

// Instance returns this run's random instance id (also in /api/health).
func (e *Engine) Instance() string { return e.instance }

// Health is GET /api/health.
type Health struct {
	OK       bool   `json:"ok"`
	Name     string `json:"name"`
	Version  string `json:"version"`
	Pack     string `json:"pack"`
	LLM      string `json:"llm"`
	Model    string `json:"model,omitempty"`
	Receipts int64  `json:"receipts"`
	ChainOK  bool   `json:"chain_ok"`
	Locked   bool   `json:"locked"`
	DataDir  string `json:"data_dir"`
	Pending  int    `json:"pending_approvals"`
	Instance string `json:"instance"`
}

// Health reports runtime status.
func (e *Engine) Health() Health {
	e.mu.Lock()
	defer e.mu.Unlock()
	st := e.log.Status()
	h := Health{OK: true, Name: "aios", Version: e.opts.Version, Pack: e.st.Pack, LLM: "off",
		Receipts: st.Count, ChainOK: st.OK, Locked: e.st.Locked, DataDir: e.dir, Pending: e.pendingCount(), Instance: e.instance}
	if e.opts.LLM != nil {
		h.LLM, h.Model = "on", e.opts.LLM.Model
	}
	return h
}

// Packs lists the bundled packs.
func (e *Engine) Packs() []pack.Info { return pack.List() }

// StateView is GET /api/state: the active pack plus the operator's local work.
type StateView struct {
	*pack.Pack
	Drafts           []agents.Draft `json:"drafts"`
	SavedMatches     []agents.Match `json:"saved_matches"`
	Sent             []SentReminder `json:"sent"`
	PendingApprovals int            `json:"pending_approvals"`
	// RunID and Receipt are set only on the response to a pack switch, so
	// the switch can be undone like any other tier-1 run.
	RunID   string           `json:"run_id,omitempty"`
	Receipt *receipts.Record `json:"receipt,omitempty"`
}

// State returns the active pack's state.
func (e *Engine) State() StateView {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.stateView()
}

func (e *Engine) stateView() StateView {
	id := e.st.Pack
	return StateView{
		Pack:             e.packs[id],
		Drafts:           orEmpty(e.st.Drafts[id]),
		SavedMatches:     orEmpty(e.st.Matches[id]),
		Sent:             orEmpty(e.st.Sent[id]),
		PendingApprovals: e.pendingCount(),
	}
}

// SwitchPack makes id the active pack. It is a tier-1 run: receipted and
// undoable through its run id.
func (e *Engine) SwitchPack(id string) (StateView, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !pack.Exists(id) {
		return StateView{}, errf(http.StatusUnprocessableEntity, "unknown_pack", "unknown pack %q (choose auto or homecare)", id)
	}
	prev := e.st.Pack
	runID := newRunID()
	if err := e.replace(runID, "settings", "", "pack", id); err != nil {
		return StateView{}, err
	}
	summary := fmt.Sprintf("Switched the active pack from %s to %s (%s).", prev, id, e.packs[id].Brand)
	if prev == id {
		summary = fmt.Sprintf("Pack %s (%s) was already active.", id, e.packs[id].Brand)
	}
	rec, err := e.log.Append(receipts.Record{Kind: receipts.KindPack, Agent: "settings", Tier: 1, Summary: summary, Ref: runID})
	if err != nil {
		return StateView{}, err
	}
	v := e.stateView()
	v.RunID, v.Receipt = runID, &rec
	return v, nil
}

// LockResult is POST /api/lock and /api/unlock.
type LockResult struct {
	Status  string          `json:"status"`
	Locked  bool            `json:"locked"`
	Receipt receipts.Record `json:"receipt"`
}

// SetLock locks or unlocks the session. While locked, agent runs,
// approvals, undo and ask are refused with 423; declining stays possible.
// The lock survives a restart.
func (e *Engine) SetLock(on bool) (*LockResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	was := e.st.Locked
	e.st.Locked = on
	if err := e.save(); err != nil {
		e.st.Locked = was
		return nil, err
	}
	kind, status, summary := receipts.KindLock, "locked", "Locked the session: agent runs and approvals are refused until it is unlocked."
	if !on {
		kind, status, summary = receipts.KindUnlock, "unlocked", "Unlocked the session: agents may run inside their tiers again."
	}
	if was == on {
		summary = "Session was already " + status + "."
	}
	rec, err := e.log.Append(receipts.Record{Kind: kind, Agent: "settings", Tier: 1, Summary: summary})
	if err != nil {
		return nil, err
	}
	return &LockResult{Status: status, Locked: on, Receipt: rec}, nil
}

func (e *Engine) refuseIfLocked() error {
	if e.st.Locked {
		return errf(http.StatusLocked, "locked", "the session is locked; unlock it to run agents or approve")
	}
	return nil
}

// Capabilities lists what each agent may read, write and never touch.
func (e *Engine) Capabilities() []agents.Capability {
	e.mu.Lock()
	defer e.mu.Unlock()
	return agents.Capabilities(e.packs[e.st.Pack], e.opts.LLM != nil)
}

// Specs returns the agent catalog for the active pack.
func (e *Engine) Specs() []agents.Spec {
	e.mu.Lock()
	defer e.mu.Unlock()
	return agents.Specs(e.packs[e.st.Pack])
}

// RunResult is a completed tier-0/1 run.
type RunResult struct {
	Status  string          `json:"status"`
	RunID   string          `json:"run_id"`
	Agent   string          `json:"agent"`
	Tier    int             `json:"tier"`
	Output  any             `json:"output"`
	Receipt receipts.Record `json:"receipt"`
}

// ProposalResult is a tier-2 request turned into a proposal.
type ProposalResult struct {
	Status   string   `json:"status"`
	Proposal Proposal `json:"proposal"`
}

// Run runs an agent. Tier 0/1 runs return *RunResult; tier 2 returns
// *ProposalResult and executes nothing.
func (e *Engine) Run(id string, raw map[string]json.RawMessage) (any, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.refuseIfLocked(); err != nil {
		return nil, err
	}
	p := e.packs[e.st.Pack]
	spec, ok := agents.Find(p, id)
	if !ok {
		return nil, errf(http.StatusNotFound, "unknown_agent", "there is no agent %q", id)
	}
	prm, err := spec.ParseParams(raw)
	if err != nil {
		return nil, paramErr(err)
	}
	tier := spec.EffectiveTier(prm)
	if tier == 2 {
		return e.propose(p, spec, prm)
	}
	runID := newRunID()
	var out any
	var summary string
	switch id {
	case agents.MorningBrief:
		b := agents.RunMorningBrief(p)
		out, summary = b, b.Headline
	case agents.PayrollPrecheck:
		pc := agents.RunPayrollPrecheck(p, prm.String("location"))
		out, summary = pc, pc.Summary
	case agents.FranchisorRollup:
		r, err := agents.RunFranchisorRollup(p, prm.String("period"))
		if err != nil {
			return nil, paramErr(err)
		}
		out, summary = r, r.Summary
	case agents.Reconcile:
		r := agents.RunReconcile(p)
		if tier == 1 {
			if err := e.replace(runID, id, p.ID, compMatches, r.Matched); err != nil {
				return nil, err
			}
			r.Applied, r.SavedMatches = true, len(r.Matched)
			r.Summary += fmt.Sprintf(" Saved %d matches to the books (undo with run %s).", len(r.Matched), runID)
		}
		out, summary = r, r.Summary
	case agents.ARReminders:
		sent := map[string]bool{}
		for _, s := range e.st.Sent[p.ID] {
			sent[s.Invoice] = true
		}
		d := agents.RunARReminders(p, prm.Int("over_days"), sent)
		if err := e.replace(runID, id, p.ID, compDrafts, d.Drafts); err != nil {
			return nil, err
		}
		out, summary = d, d.Summary
	default:
		return nil, errf(http.StatusInternalServerError, "no_runner", "agent %q has no runner", id)
	}
	rec, err := e.log.Append(receipts.Record{Kind: receipts.KindRun, Agent: id, Tier: tier, Summary: summary, Ref: runID})
	if err != nil {
		return nil, err
	}
	return &RunResult{Status: "done", RunID: runID, Agent: id, Tier: tier, Output: out, Receipt: rec}, nil
}

// Tier-1 components: what a reversible run changes.
const (
	compDrafts  = "drafts"
	compMatches = "matches"
	compPack    = "pack"
)

func (e *Engine) component(component, packID string) any {
	switch component {
	case compDrafts:
		return orEmpty(e.st.Drafts[packID])
	case compMatches:
		return orEmpty(e.st.Matches[packID])
	default:
		return e.st.Pack
	}
}

func (e *Engine) setComponent(component, packID string, raw json.RawMessage) error {
	switch component {
	case compDrafts:
		var d []agents.Draft
		if err := json.Unmarshal(raw, &d); err != nil {
			return err
		}
		e.st.Drafts[packID] = d
	case compMatches:
		var m []agents.Match
		if err := json.Unmarshal(raw, &m); err != nil {
			return err
		}
		e.st.Matches[packID] = m
	case compPack:
		var id string
		if err := json.Unmarshal(raw, &id); err != nil || !pack.Exists(id) {
			return fmt.Errorf("undo: bad pack value %s", raw)
		}
		e.st.Pack = id
	default:
		return fmt.Errorf("undo: unknown component %q", component)
	}
	return nil
}

// replace sets a tier-1 component and remembers what it replaced, so the
// run can be undone. It persists before returning.
func (e *Engine) replace(runID, agent, packID, component string, after any) error {
	beforeRaw, err := json.Marshal(e.component(component, packID))
	if err != nil {
		return err
	}
	afterRaw, err := json.Marshal(after)
	if err != nil {
		return err
	}
	afterHash, err := canon.Hash(after)
	if err != nil {
		return err
	}
	snapshot, _ := json.Marshal(e.st)
	restore := func() {
		var old state
		if json.Unmarshal(snapshot, &old) == nil {
			*e.st = old
		}
	}
	if err := e.setComponent(component, packID, afterRaw); err != nil {
		return err
	}
	e.st.Undo = append(e.st.Undo, UndoEntry{RunID: runID, Agent: agent, Pack: packID, Component: component,
		Before: beforeRaw, AfterHash: afterHash, Created: e.now()})
	if len(e.st.Undo) > maxUndo {
		e.st.Undo = e.st.Undo[len(e.st.Undo)-maxUndo:]
	}
	if err := e.save(); err != nil {
		restore()
		return err
	}
	return nil
}

// UndoResult is POST /api/runs/{id}/undo.
type UndoResult struct {
	Status  string          `json:"status"`
	RunID   string          `json:"run_id"`
	Receipt receipts.Record `json:"receipt"`
}

// Undo reverses a tier-1 run (saved drafts, saved matches, a pack switch),
// provided nothing has changed the same thing since.
func (e *Engine) Undo(runID string) (*UndoResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.refuseIfLocked(); err != nil {
		return nil, err
	}
	idx := -1
	for i := range e.st.Undo {
		if e.st.Undo[i].RunID == runID {
			idx = i
		}
	}
	if idx < 0 {
		// The receipts say whether the id is a run that cannot be undone
		// (tier 0 or tier 2) or not a run at all.
		tier := -1
		_ = e.log.Each(func(r receipts.Record) {
			if r.Ref == runID && r.Kind != receipts.KindUndo {
				tier = r.Tier
			}
		})
		if tier == 0 || tier == 2 {
			return nil, errf(http.StatusUnprocessableEntity, "not_undoable",
				"run %s is tier %d; only tier-1 runs (reversible local changes) can be undone", runID, tier)
		}
		return nil, errf(http.StatusNotFound, "not_found", "no undoable run %q", runID)
	}
	u := &e.st.Undo[idx]
	if u.Undone {
		return nil, errf(http.StatusConflict, "already_undone", "run %s was already undone", runID)
	}
	if h, _ := canon.Hash(e.component(u.Component, u.Pack)); h != u.AfterHash {
		return nil, errf(http.StatusConflict, "superseded", "a later change replaced what run %s did; undo that one first", runID)
	}
	snapshot, _ := json.Marshal(e.st)
	if err := e.setComponent(u.Component, u.Pack, u.Before); err != nil {
		return nil, err
	}
	u.Undone = true
	if err := e.save(); err != nil {
		var old state
		if json.Unmarshal(snapshot, &old) == nil {
			*e.st = old
		}
		return nil, err
	}
	what := "the previous " + u.Component
	if u.Component == compPack {
		what = "the previous pack (" + e.st.Pack + ")"
	}
	rec, err := e.log.Append(receipts.Record{Kind: receipts.KindUndo, Agent: u.Agent, Tier: 1, Ref: runID,
		Summary: fmt.Sprintf("Undid run %s (%s): restored %s.", runID, u.Agent, what)})
	if err != nil {
		return nil, err
	}
	return &UndoResult{Status: "undone", RunID: runID, Receipt: rec}, nil
}

// propose turns a tier-2 request into a proposal bound to the hash of its
// canonical instruction. Nothing is executed.
func (e *Engine) propose(p *pack.Pack, spec agents.Spec, prm agents.Params) (*ProposalResult, error) {
	var instruction any
	var summary string
	switch spec.ID {
	case agents.SendReminders:
		drafts := e.st.Drafts[p.ID]
		if len(drafts) == 0 {
			return nil, errf(http.StatusConflict, "no_drafts", "there are no saved reminder drafts; run %s first", agents.ARReminders)
		}
		in, s, err := agents.BuildSendReminders(p, drafts, prm.List("invoices"))
		if err != nil {
			return nil, paramErr(err)
		}
		instruction, summary = in, s
	case agents.PayrollSubmit:
		in, s := agents.BuildPayrollSubmit(p, prm.String("location"))
		instruction, summary = in, s
	default:
		return nil, errf(http.StatusInternalServerError, "no_runner", "agent %q has no tier-2 builder", spec.ID)
	}
	body, err := canon.Marshal(instruction)
	if err != nil {
		return nil, err
	}
	hash := canon.SHA256Hex(body)
	if old, ok := e.st.Proposals[hash]; ok {
		switch old.Status {
		case StatusPending:
			// The same instruction asked twice is the same proposal.
			return &ProposalResult{Status: "needs_approval", Proposal: *old}, nil
		case StatusUsed:
			return nil, errf(http.StatusConflict, "already_used",
				"this exact instruction (%s) was already approved and executed on %s; each one runs at most once", short(hash), old.Decided)
		}
	}
	prop := &Proposal{Hash: hash, Agent: spec.ID, Tier: 2, Pack: p.ID, Summary: summary,
		Instruction: body, Created: e.now(), Status: StatusPending}
	prev := e.st.Proposals[hash]
	e.st.Proposals[hash] = prop
	if err := e.save(); err != nil {
		e.restoreProposal(hash, prev)
		return nil, err
	}
	if _, err := e.log.Append(receipts.Record{Kind: receipts.KindProposal, Agent: spec.ID, Tier: 2, Ref: hash,
		Summary: "Proposed (awaiting approval): " + summary}); err != nil {
		return nil, err
	}
	return &ProposalResult{Status: "needs_approval", Proposal: *prop}, nil
}

func (e *Engine) restoreProposal(hash string, prev *Proposal) {
	if prev == nil {
		delete(e.st.Proposals, hash)
	} else {
		e.st.Proposals[hash] = prev
	}
}

// Proposals returns pending proposals, newest first.
func (e *Engine) Proposals() []Proposal {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := []Proposal{}
	for _, p := range e.st.Proposals {
		if p.Status == StatusPending {
			out = append(out, *p)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Created != out[j].Created {
			return out[i].Created > out[j].Created
		}
		return out[i].Hash < out[j].Hash
	})
	return out
}

// ApproveResult is a tier-2 act that ran.
type ApproveResult struct {
	Status  string          `json:"status"`
	Output  any             `json:"output"`
	Receipt receipts.Record `json:"receipt"`
}

// Approve runs the proposal with the given hash, once, if the phrase is
// exactly APPROVE.
func (e *Engine) Approve(hash, phrase string) (*ApproveResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.refuseIfLocked(); err != nil {
		return nil, err
	}
	prop, ok := e.st.Proposals[hash]
	if !ok {
		return nil, errf(http.StatusNotFound, "not_found", "no proposal %s", short(hash))
	}
	switch prop.Status {
	case StatusUsed:
		return nil, errf(http.StatusConflict, "already_used", "proposal %s was already approved and executed on %s; each one runs at most once", short(hash), prop.Decided)
	case StatusDeclined:
		return nil, errf(http.StatusConflict, "not_pending", "proposal %s was declined; ask the agent again for a new proposal", short(hash))
	}
	if phrase != ApprovePhrase {
		return nil, errf(http.StatusUnprocessableEntity, "wrong_phrase", "type %s exactly to approve", ApprovePhrase)
	}
	if st := e.log.Status(); !st.OK {
		return nil, errf(http.StatusConflict, "chain_broken",
			"the receipt chain does not verify (line %d: %s); approvals are frozen until it is restored", st.BadLine, st.Reason)
	}
	// Execute only what was approved: the stored instruction must still hash
	// to the approved value.
	if got := canonHashRaw(prop.Instruction); got != hash {
		return nil, errf(http.StatusConflict, "hash_mismatch",
			"the stored instruction no longer matches proposal %s; it was changed after it was proposed and will not run", short(hash))
	}
	// Mark used before executing: if anything fails from here on, the act
	// has run at most once, never twice.
	prop.Status, prop.Decided = StatusUsed, e.now()
	if err := e.save(); err != nil {
		prop.Status, prop.Decided = StatusPending, ""
		return nil, err
	}
	out, summary, execErr := e.execute(prop)
	if execErr != nil {
		summary = fmt.Sprintf("Approved %s but it failed to execute: %v", short(hash), execErr)
	}
	_ = e.save()
	rec, err := e.log.Append(receipts.Record{Kind: receipts.KindApprove, Agent: prop.Agent, Tier: 2, Ref: hash, Summary: summary})
	if err != nil {
		return nil, err
	}
	if execErr != nil {
		return nil, errf(http.StatusInternalServerError, "exec_failed", "%s", summary)
	}
	return &ApproveResult{Status: "done", Output: out, Receipt: rec}, nil
}

// Decline drops a pending proposal.
func (e *Engine) Decline(hash string) (receipts.Record, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	prop, ok := e.st.Proposals[hash]
	if !ok {
		return receipts.Record{}, errf(http.StatusNotFound, "not_found", "no proposal %s", short(hash))
	}
	if prop.Status != StatusPending {
		return receipts.Record{}, errf(http.StatusConflict, "not_pending", "proposal %s is %s, not pending", short(hash), prop.Status)
	}
	prop.Status, prop.Decided = StatusDeclined, e.now()
	if err := e.save(); err != nil {
		prop.Status, prop.Decided = StatusPending, ""
		return receipts.Record{}, err
	}
	return e.log.Append(receipts.Record{Kind: receipts.KindDecline, Agent: prop.Agent, Tier: 2, Ref: hash,
		Summary: "Declined: " + prop.Summary})
}

// ReceiptsPage is GET /api/receipts.
type ReceiptsPage struct {
	ChainOK bool              `json:"chain_ok"`
	Count   int64             `json:"count"`
	BadLine int64             `json:"bad_line,omitempty"`
	Reason  string            `json:"reason,omitempty"`
	Records []json.RawMessage `json:"records"`
}

// Receipts returns the newest records and the chain state.
func (e *Engine) Receipts(limit int) (ReceiptsPage, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	st := e.log.Status()
	recs, err := e.log.Records(limit)
	if err != nil {
		return ReceiptsPage{}, err
	}
	return ReceiptsPage{ChainOK: st.OK, Count: st.Count, BadLine: st.BadLine, Reason: st.Reason, Records: recs}, nil
}

func (e *Engine) pendingCount() int {
	n := 0
	for _, p := range e.st.Proposals {
		if p.Status == StatusPending {
			n++
		}
	}
	return n
}

func (e *Engine) now() string { return e.opts.Now().UTC().Format(time.RFC3339) }

// save writes state.json atomically: a temporary file, synced, then renamed
// over the old one.
func (e *Engine) save() error {
	path := filepath.Join(e.dir, stateFile)
	b, err := json.MarshalIndent(e.st, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, b)
}

func loadState(path string) (*state, error) {
	st := &state{Version: 1}
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, fmt.Errorf("state: %w", err)
	default:
		if err := json.Unmarshal(b, st); err != nil {
			return nil, fmt.Errorf("state file %s is damaged (%v); move it aside to start fresh — receipts are kept separately", path, err)
		}
	}
	if st.Drafts == nil {
		st.Drafts = map[string][]agents.Draft{}
	}
	if st.Matches == nil {
		st.Matches = map[string][]agents.Match{}
	}
	if st.Sent == nil {
		st.Sent = map[string][]SentReminder{}
	}
	if st.Proposals == nil {
		st.Proposals = map[string]*Proposal{}
	}
	return st, nil
}

func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".tmp-"+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	cleanup := func() { _ = os.Remove(tmp) }
	if _, err := f.Write(data); err != nil {
		f.Close()
		cleanup()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		cleanup()
		return err
	}
	if err := f.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil && runtime.GOOS != "windows" {
		cleanup()
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		cleanup()
		return err
	}
	if runtime.GOOS != "windows" {
		if d, err := os.Open(dir); err == nil {
			_ = d.Sync()
			d.Close()
		}
	}
	return nil
}

func canonHashRaw(raw json.RawMessage) string {
	c, err := canon.Canonicalize(raw)
	if err != nil {
		return ""
	}
	return canon.SHA256Hex(c)
}

func newRunID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return "run-" + hex.EncodeToString(b[:])
}

func short(hash string) string {
	if len(hash) > 12 {
		return hash[:12]
	}
	return hash
}

func paramErr(err error) error {
	var pe *agents.ParamError
	if errors.As(err, &pe) {
		return errf(http.StatusUnprocessableEntity, "bad_param", "%s", pe.Msg)
	}
	return err
}

func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
