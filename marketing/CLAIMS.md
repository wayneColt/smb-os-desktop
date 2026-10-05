# Claims ledger: SMB OS Desktop 0.1.0

Every public sentence about the product, mapped to the thing that proves it. Gate 1 of
[docs/MARKETING_PACKAGE.md](../docs/MARKETING_PACKAGE.md) passes only when every row is ticked, and the
proof column names the exact test, file or URL from the **tagged** commit and release.

Where the claim appears: **R** README · **N** release notes · **S** solutions page · **P** post receipt
page · **X** the X post (candidate letter).

Kinds of proof: **test** (a `go test` name or a UI check that passes on the tagged commit), **file** (a
path in the tagged tree), **URL** (answers from outside), **run** (a saved transcript from
`docs/RELEASE_PROCESS.md` step 5 or 7), **ext** (an outside source for a fact about other software).

Test names come from the runtime builder (2026-10-05); each was mutation-checked (the property disabled,
the test went red). Rows still marked *contract* need their test or UI check named before release.

## Positioning

| # | Claim, as worded | Where | Proof | ✓ |
|---|---|---|---|---|
| P1 | Business intelligence that runs on your computer | R N S P X:A,B,C | = D1 + L1 + L5: one local program, data in a local folder, nothing sent without an AI key | [ ] |
| P2 | …and answers to you: anything that moves money waits for your typed APPROVE | R N S P X:A,C | = G3 + G5 + P4 | [ ] |
| P3 | Two planes, one authority: the agents read, reconcile, prepare and draft and hold no authority of their own | R N S P X:B | test: `internal/agents/agents_test.go:TestAgentsCannotTouchDiskNetworkOrApprove` (the agents package imports nothing that reaches disk, network, processes, the receipt log or the approval gate) | [ ] |
| P4 | An agent can make a proposal; it has no way to approve one | R N S P | test: `internal/agents/agents_test.go:TestAgentsCannotTouchDiskNetworkOrApprove`; approval lives only in the engine behind the typed phrase | [ ] |
| P5 | "Review what agents may do" lists each agent's tier, what it reads, writes and never touches, and its network access: none | R N S | test: `internal/server/server_test.go:TestLockCapabilitiesAndUndoOverHTTP`, `internal/agents/agents_test.go:TestCapabilitiesCoverEveryAgent`, `internal/agents/agents_test.go:TestAgentsCannotTouchDiskNetworkOrApprove`; `marketing/screenshots/desktop-capabilities-light.png` | [ ] |
| P6 | Lock session cancels running requests, closes every window, and then refuses runs and approvals until "Unlock session"; the dock reads "Locked"; it is a pause, not a password | R N S P | test: `internal/engine/engine_test.go:TestLockRefusesRunsApprovalsUndoAndAsk` (423; proposal stays pending; lock survives a restart), `internal/server/server_test.go:TestLockCapabilitiesAndUndoOverHTTP`; `marketing/screenshots/desktop-locked-light.png` | [ ] |
| P7 | The Apex in four bands: Search (windows, locations, numbers, agents, approvals, receipts; Ask); Operations (worst measure, alerts, who is on shift); Engine & workers (state, tier, Run); Sovereignty (approvals, chain intact, where data lives, AI model, Lock session, Review what agents may do, Open receipts) | R N S | UI check on the tagged build; `marketing/screenshots/apex-bands-light.png` | [ ] |
| P8 | The same design on a phone (approvals in thumb reach) and on a web desk comes next; neither is in 0.1.0 | R N S P | stated as not shipped. Proof is absence: the release holds only the five desktop zips. Never reworded as available | [ ] |
| P9 | In 0.1.0 the gate is enforced by the program on your computer; no separate account; anyone who can use the computer can approve | R N S P | file: the server's approve route is the only path to a tier-2 effect; the API has no auth route | [ ] |

## Delivery

| # | Claim, as worded | Where | Proof | ✓ |
|---|---|---|---|---|
| D1 | One downloaded file; double-click it and the desktop opens in your browser | R N S P X:A | URL: the release has 5 zips (plus 5 version-less copies, same bytes), each holding `aios` + `README.txt`; run: step 7 on Windows, a Mac and Linux | [ ] |
| D2 | Builds for Windows x86-64, macOS Apple silicon and Intel, Linux x86-64 and ARM64 | R N S | URL: release asset list; run: step 5/7 on each system actually tried. A system built but not run is said as such | [ ] |
| D3 | Prints `SMB OS Desktop running at http://127.0.0.1:7701/ (Ctrl+C to stop)`; uses the next free port if 7701 is taken | R S | test: `cmd/aios/main_test.go:TestListenFallsBackToTheNextFreePort`; run: step 7 transcript | [ ] |
| D2a | The download links always fetch the newest release (`releases/latest/download/smb-os-desktop_<os>_<arch>.zip`); the exact v0.1.0 files are on the v0.1.0 release page | R S | URL: each latest link answers 302 → 200 and its SHA-256 equals its line in `SHA256SUMS.txt` (release step 4) | [ ] |
| D3a | If no browser opens, it prints the address to open yourself; started again while running, it opens the same desktop instead of a second copy | R | file: `cmd/aios/main.go` (`run`: `instance.Acquire`, "already running at"); test *runtime* | [ ] |
| D4 | The installers pick the right file, check it against `SHA256SUMS.txt`, then start it | R N S | file: `scripts/install.sh`, `scripts/install.ps1`, uploaded as release assets by `.github/workflows/release.yml`; run: `install.sh` refused a one-bit-corrupted zip, exit 1, nothing written (runtime build check 2026-10-05). `install.ps1` is checked by reading only (Invoke-WebRequest, Get-FileHash, Expand-Archive): run it in step 5 before this row is ticked | [ ] |
| D5 | Terminal downloads don't trigger the macOS / Windows first-run prompt | R N S | run: step 5 on macOS and Windows, prompt absent; ext: curl sets no quarantine attribute, `Invoke-WebRequest` sets no Mark of the Web | [ ] |
| D6 | Not code-signed yet; macOS asks once (Privacy & Security, Open Anyway), Windows may show SmartScreen (More info, Run anyway); on macOS 15+ Control-click Open no longer skips it | R N S | run: step 7 screenshots of both prompts; ext: Apple's macOS 15 Gatekeeper change note | [ ] |
| D7 | Build from source with Go 1.27 and nothing else; `go test ./...`, `go build -o aios ./cmd/aios` | R S | file: `go.mod` (no `require`); `make check` (vet, race tests, gofmt: what CI runs); URL: CI run log of the tagged commit | [ ] |
| D8 | `scripts/build_release.sh` builds the five zips | R | file: `scripts/build_release.sh` (`make release VERSION=X.Y.Z`); URL: release workflow run | [ ] |
| D9 | Free and open source, MIT; fonts under the SIL OFL 1.1 | R N S X:B | file: `LICENSE`, `web/ds/fonts/LICENSE-*-OFL.txt`; URL: the public repo | [ ] |

## Staying on the machine

| # | Claim, as worded | Where | Proof | ✓ |
|---|---|---|---|---|
| L1 | Listens on `127.0.0.1` only; other computers on your network can't reach it | R N S | test: `cmd/aios/main_test.go:TestListenFallsBackToTheNextFreePort`, `internal/server/server_test.go:TestOverARealLoopbackListener`; run: `ss -ltn` / `netstat -an` during step 7 shows `127.0.0.1:7701` only | [ ] |
| L2 | Answers only requests addressed to `127.0.0.1` or `localhost` | R S | test: `internal/server/server_test.go:TestGuards`, `internal/server/server_test.go:TestGuardedRequestsChangeNothing` | [ ] |
| L3 | Every change needs a header your browser won't let other websites send; a site you visit can't press buttons on the desktop | R S | test: `internal/server/server_test.go:TestGuards`, `internal/server/server_test.go:TestOverARealLoopbackListener` (no `X-AIOS` → refused; foreign `Origin` → refused; no CORS headers) | [ ] |
| L4 | No account, no sign-in | R N S X:A | file: the API table in `docs/CONTRACT.md` has no auth route; run: step 7 | [ ] |
| L5 | No usage tracking; unless you turn on an AI model, nothing leaves your machine ("no cloud" in X:A) | R N S P X:A | test: `internal/engine/engine_test.go:TestNoNetworkWithoutAPIKey` (every agent, both approvals and Ask through a counting transport: 0 requests), `internal/llm/llm_test.go:TestFromEnv` | [ ] |
| L6 | Everything lives in one folder (`%AppData%`, `~/Library/Application Support`, `~/.config`, or `--data`) | R S | test *contract* (`--data` honoured; default from `os.UserConfigDir()`) | [ ] |

## The gate

| # | Claim, as worded | Where | Proof | ✓ |
|---|---|---|---|---|
| G1 | Reading and calculating (tier 0) runs at once and writes a receipt | R N S | test *contract* | [ ] |
| G2 | A reversible local change (tier 1: switch demo business, save matches, save drafts) runs at once and writes a receipt | R N S | test *contract*. Open question: the contract calls tier 1 "undoable" but has no undo route; the copy says "reversible" and names only changes that can be reversed by switching back or re-saving | [ ] |
| G3 | Money, outward or irreversible acts (tier 2) never run when asked; they become a proposal | R N S P X:A,B | test: `internal/engine/engine_test.go:TestTier2NeverExecutesWithoutApproval`, `internal/server/server_test.go:TestRunApproveCycleOverHTTP` (202, nothing in `outbox/`) | [ ] |
| G4 | The proposal's fingerprint is the SHA-256 of the exact instruction | R N S X:B | test: `internal/engine/engine_test.go:TestSameInstructionSameProposalChangedInstructionNewHash`; `internal/engine/engine_test.go:TestApproveRefusesTamperedInstruction` (an instruction edited after proposing is refused) | [ ] |
| G5 | Runs only after you type `APPROVE` | R N S P X:A,B | test: `internal/engine/engine_test.go:TestTier2NeverExecutesWithoutApproval`, `internal/server/server_test.go:TestRunApproveCycleOverHTTP` (wrong phrase → 422) | [ ] |
| G6 | Each fingerprint runs once | R N S P X:B | test: `internal/engine/engine_test.go:TestApproveRunsOnceThen409`, `internal/server/server_test.go:TestRunApproveCycleOverHTTP`, `internal/engine/engine_test.go:TestUsedHashesAreRebuiltFromReceipts` (holds even with state.json deleted) | [ ] |
| G7 | Change one detail and it is a new proposal with a new fingerprint | R S | test: `internal/engine/engine_test.go:TestSameInstructionSameProposalChangedInstructionNewHash` | [ ] |
| G8 | In 0.1.0 an approved tier-2 act writes its payload to the outbox folder and contacts nothing | R N S P | test: `internal/engine/engine_test.go:TestApproveRunsOnceThen409` (afterwards the data folder holds only `outbox/`, `receipts.jsonl`, `state.json`) + L5; `marketing/screenshots/desktop-approved-receipt-night.png` | [ ] |
| G9 | Submitting payroll and sending reminders wait for a typed APPROVE | X:A S | test: `internal/engine/engine_test.go:TestTier2NeverExecutesWithoutApproval` (`payroll-submit`, `send-reminders` are tier 2) | [ ] |

## Receipts

| # | Claim, as worded | Where | Proof | ✓ |
|---|---|---|---|---|
| K1 | Every run, proposal, approval, decline and switch of demo business adds a line to `receipts.jsonl` | R N S | test *contract* | [ ] |
| K2 | Each line includes the SHA-256 of the line before it | R N S X:A,B | test: `internal/receipts/receipts_test.go:TestTamperedSummaryBreaksTheChain` (hash = sha256(prev + canonical record); first prev = 64 zeros) | [ ] |
| K3 | Changing or deleting an earlier line breaks the chain, and the Receipts window shows it | R S | test: `internal/receipts/receipts_test.go:TestTamperedSummaryBreaksTheChain`, `TestTamperedTierBreaksTheChain`, `TestRewrittenHashIsCaughtByTheNextLink`, `TestDeletedReorderedAndTruncatedLines`, `TestAppendFollowsAReplacedFile`; `internal/server/server_test.go:TestReceiptsEndpointAndTamper`; `internal/engine/engine_test.go:TestBrokenChainFreezesApprovals`; `marketing/screenshots/desktop-receipts-light.png` | [ ] |
| K4 | The chain detects edits; it can't stop someone who can write the folder from rewriting the whole file | R S | file: receipts design (no outside anchor); stated as a limit, not a feature | [ ] |

## Agents and desktop

| # | Claim, as worded | Where | Proof | ✓ |
|---|---|---|---|---|
| A1 | All agents are rule-based; no AI model needed | R N S | test: `internal/engine/engine_test.go:TestNoNetworkWithoutAPIKey` (every agent runs with no key) | [ ] |
| A2 | Morning brief: headline, a line per location, variances against target, worst first | R N S | test *contract* | [ ] |
| A3 | Payroll pre-check: overtime, missing punches, unverified visits (home care) / flagged vs clocked hours (auto), totals | R N S | test *contract* (both packs) | [ ] |
| A4 | Reconciliation: same amount, dates within 3 days, matching reference; unmatched on both sides; suggested fee and interest entries | R N S | test *contract* | [ ] |
| A5 | AR reminders: drafts for invoices more than 14 days late | R N S | test *contract* | [ ] |
| A6 | Franchise rollup: revenue, royalty and ad fund per location and the franchise total | R N S X:C | test *contract* | [ ] |
| A7 | Ask answers from the data with plain rules and lists the numbers it used | R S | test *contract* (`mode:"deterministic"`, `sources` non-empty) | [ ] |
| A8 | With `ANTHROPIC_API_KEY` set, Ask uses a Claude model (`AIOS_MODEL`, default `claude-sonnet-5-5`) and sends the question and the data it draws on to Anthropic's API | R N S | file: the model adapter; test *contract* (`mode:"llm"` only with the key) | [ ] |
| A9 | Windows: Today, Approvals, Inbox, Ask, Payroll, Reconciliation, Receivables, Franchise rollup, Receipts, Agents, Settings | R S | UI check on the tagged build; screenshots | [ ] |
| A10 | Dock shows open windows, approvals waiting, AI-model state, clock | R S | UI check; `desktop-today-light.png` | [ ] |
| A11 | Ctrl+K (or Ctrl+Space) opens the Apex; "Stop everything" in its top bar cancels running requests and closes every window without locking | R S | UI check on the tagged build | [ ] |
| A12 | A "Fictional demo data" mark stays on screen; two demo businesses, Kestrel Auto Care and Larkmoor Home Care | R N S P X:A,C | file: `packs/*.json` (`"fictional": true`); UI check | [ ] |
| A13 | If the program can't be reached the desktop says so and shows no invented numbers | S | UI check *contract* (`NO SIGNAL from aios`); `marketing/screenshots/desktop-nosignal-light.png` | [ ] |
| A14 | Does not connect to your systems, send anything, ship signed binaries, update itself or support more than one user | R N S P | stated limits; checked against the tagged tree | [ ] |

## The site

| # | Claim, as worded | Where | Proof | ✓ |
|---|---|---|---|---|
| W1 | The download button names your system and links its file | S | gate 4: checked on Windows, a Mac and Linux | [ ] |
| W2 | On a phone the page says to download on a computer | S | gate 4: phone check at 390 px wide | [ ] |
