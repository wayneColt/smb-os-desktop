# SMB OS Desktop: build contract v0.1

The single source of truth the three builders (runtime, desktop UI, marketing) share. If you need to change
it, change it here first and say so in your report.

## What it is

An operator's desktop for a small multi-location franchise business, delivered as **one downloaded file**.
Double-click it and a local runtime (`aios`) starts on `127.0.0.1`, serves the desktop to the default
browser, keeps the business data on the machine, and runs the operator's agents behind a tier gate.
No install step, no account, no cloud, AI model optional (off by default).

Lineage: forked from Wayne OS Desktop (an agentic desktop shell for Linux). The visual system, the dock,
the command surface (Apex), the executor's tier gate and the hash-chained receipts come from it; the
Wayland and Linux-only parts do not.

## Repo layout and ownership (disjoint, so builders never edit the same file)

| Path | Owner | Notes |
|---|---|---|
| `go.mod`, `cmd/aios/`, `internal/**`, `packs/*.json` | runtime | Go 1.27, standard library only |
| `web/**` | desktop UI | embedded into the binary with `//go:embed`; no build step, no CDN, no npm |
| `scripts/build_release.sh`, `scripts/install.sh`, `scripts/install.ps1`, `.github/workflows/*.yml`, `Makefile` | runtime | |
| `README.md`, `LICENSE`, `docs/MARKETING_PACKAGE.md`, `docs/RELEASE_PROCESS.md`, `marketing/**`, `site/**`, `scripts/firewall_scan.sh` | marketing | |
| `docs/CONTRACT.md` | orchestrator | |

`web/` is embedded by a file `web/embed.go` (owned by **runtime**, package `web`, `//go:embed all:*` minus
`embed.go`), so the UI builder only drops static files there.

## Public-surface rules (apply to every byte in this repo)

- Fictional demo data only. Every pack carries `"fictional": true` and the UI shows a "Fictional demo data" mark.
- No real people, no real businesses, no real franchise brands, no real addresses or phone numbers, no LAN
  addresses, hostnames, internal service names, ports of other systems, or internal project vocabulary.
- Generic language for integrations: "your scheduling system", "your shop-management system", "your accounting
  system", "your bank export". Never name a vendor as integrated.
- `scripts/firewall_scan.sh` must pass before any push (it reads a term list from a path outside the repo).

## Runtime (`aios`)

- Binary name `aios` (`aios.exe` on Windows). Flags: `--port` (default `7701`; if busy, the next free one),
  `--data <dir>` (default `os.UserConfigDir()/smb-os-desktop`), `--pack auto|homecare` (default `auto`),
  `--no-open` (don't launch the browser), `--version`.
- Binds `127.0.0.1` only. Rejects requests whose `Host` is not `127.0.0.1:<port>` or `localhost:<port>`
  (DNS-rebinding guard). Every non-GET request must carry header `X-AIOS: 1` and, if an `Origin` header is
  present, it must be the served origin (CSRF guard). No CORS headers are ever sent.
- On start: prints one line `SMB OS Desktop running at http://127.0.0.1:<port>/ (Ctrl+C to stop)` and opens the
  browser unless `--no-open`.
- AI model: off unless `ANTHROPIC_API_KEY` is set in the environment (model `AIOS_MODEL`, default
  `claude-sonnet-5-5`). With it off, `/api/ask` answers deterministically from the data. Nothing leaves the
  machine unless the key is set.

### Tiers

| Tier | Meaning | Gate |
|---|---|---|
| 0 | read and compute | runs immediately, receipt written |
| 1 | reversible local change (switch pack, save a match, save a draft) | runs immediately, receipt written, undoable |
| 2 | money, outward, irreversible (submit payroll, send reminders, post entries) | **never runs on request**: returns a proposal bound to `sha256(canonical instruction)`; runs only after `POST /api/proposals/{hash}/approve` with the typed phrase `APPROVE`; each hash runs at most once; a changed instruction is a new hash. In this demo a tier-2 act writes its payload to `<data>/outbox/` and never contacts anything external |

### Receipts

Append-only JSONL at `<data>/receipts.jsonl`. Each record:
`{"seq":N,"ts":"RFC3339","kind":"run|proposal|approve|decline|pack","agent":"id","tier":0,"summary":"...","prev":"<hex>","hash":"<hex>"}`
where `hash = sha256(prev + canonical_json(record without hash))`, `prev` of the first record = 64 zeros.

### API (JSON; errors are `{"error":"...","code":"..."}` with a real status code)

| Method + path | Response |
|---|---|
| `GET /api/health` | `{"ok":true,"name":"aios","version":"0.1.0","pack":"auto","llm":"off","receipts":N,"chain_ok":true,"locked":false,"data_dir":"<absolute path>"}` |
| `GET /api/packs` | `[{"id":"auto","label":"Auto service franchise"},{"id":"homecare","label":"Home care franchise"}]` |
| `POST /api/pack` `{"id":"homecare"}` | tier 1; returns the new `/api/state` |
| `GET /api/state` | the active pack's state (shape below) |
| `GET /api/agents` | `[{"id","name","tier","description","params":[...]}]` |
| `POST /api/agents/{id}/run` `{"params":{}}` | tier 0/1: `{"status":"done","run_id","agent","tier","output":{...},"receipt":{...}}`; tier 2: `202 {"status":"needs_approval","proposal":{"hash","agent","summary","instruction":{...},"created"}}` |
| `GET /api/proposals` | pending proposals |
| `POST /api/proposals/{hash}/approve` `{"phrase":"APPROVE"}` | `{"status":"done","output":{...},"receipt":{...}}`; 404 unknown, 409 already used, 422 wrong phrase |
| `POST /api/proposals/{hash}/decline` | `{"status":"declined"}` |
| `GET /api/receipts?limit=50` | `{"chain_ok":true,"count":N,"records":[...newest first]}` |
| `POST /api/ask` `{"q":"..."}` | `{"mode":"deterministic"|"llm","answer":"...","sources":["kpi:...","agent:..."]}` |
| `GET /api/capabilities` | `[{"agent","name","tier","reads":[...],"writes":[...],"never":[...]}]` — what each agent may touch, for the Sovereignty band |
| `POST /api/runs/{run_id}/undo` | tier 1 only: reverses that run (a saved match set, a pack switch, saved drafts) and writes an `undo` receipt; 409 if already undone, 422 for tier 0/2 runs |
| `POST /api/lock` / `POST /api/unlock` | tier 1, receipt. While locked, every agent run and every approve returns `423 {"code":"locked"}`; `/api/health` gains `"locked":true` |

(Added 11:12 CDT: the Apex follows four bands — Search, Operations, Engine & workers, Sovereignty — and
the product is positioned as Business Intelligence. Lock and capabilities make the Sovereignty band real, not drawn.)

Everything else under `/` serves `web/` (`/` → `web/index.html`).

### State shape (`GET /api/state`)

```json
{
  "pack": "auto",
  "label": "Auto service franchise",
  "brand": "Kestrel Auto Care",
  "fictional": true,
  "as_of": "2026-10-05",
  "operator": "Demo Operator",
  "royalty_rate": 0.06,
  "ad_fund_rate": 0.02,
  "locations": [{"id": "L14", "name": "Riverside", "manager": "Jordan Pike"}],
  "kpis": [{"location": "L14", "key": "car_count", "label": "Car count", "value": 38, "target": 42,
            "unit": "", "better": "higher", "period": "week to date"}],
  "alerts": [{"id": "al-1", "severity": "warn|info|crit", "location": "L14", "text": "..."}],
  "inbox": [{"id": "m-1", "from": "...", "subject": "...", "received": "RFC3339", "tag": "customer|vendor|franchisor|staff"}],
  "schedule": [{"location": "L14", "who": "...", "role": "...", "start": "RFC3339", "end": "RFC3339"}],
  "ar": [{"id": "inv-1", "customer": "...", "amount": 412.50, "due": "2026-09-20", "days_late": 15}]
}
```

Pack `auto` (Kestrel Auto Care, locations Riverside L14 and Hillcrest L27): KPIs car count, average repair order,
gross profit %, labor %, bay utilization, comebacks. Pack `homecare` (Larkmoor Home Care, locations North County L3
and Lakeside L9): KPIs billable hours, visits verified (EVV) %, caregiver utilization, missed visits, overtime hours,
new client starts. All names fictional.

### Agents (deterministic, no model needed)

| id | tier | does |
|---|---|---|
| `morning-brief` | 0 | headline + per-location lines + variances vs target, worst first |
| `payroll-precheck` | 0 | hours vs schedule per person: overtime, missing punches, unverified visits (homecare) / flagged vs clocked hours (auto); totals |
| `reconcile` | 0 (param `apply:true` → 1) | match bank lines to ledger entries (exact amount; date within 3 days; reference contains), list unmatched both sides, suggest entries for bank fees / interest |
| `ar-reminders` | 1 | draft reminder notes for invoices > 14 days late (saved as drafts) |
| `send-reminders` | 2 | "send" the drafts → outbox only |
| `payroll-submit` | 2 | write the payroll export → outbox only |
| `franchisor-rollup` | 0 | per-location revenue, royalty, ad fund, and the franchise total |

## Desktop UI (`web/`)

- A browser desktop in the Wayne OS Desktop idiom, styled only by the vendored design system (`web/ds/`, tokens +
  Geist + IBM Plex Mono, OFL licenses kept beside the fonts). Light by default, night desk via the theme toggle and
  `prefers-color-scheme`.
- Desktop icons (double-click or Enter opens a window), movable/resizable windows, a bottom dock (brand chip that
  opens the Apex, open windows, approvals count, AI-model state, clock), and the Apex command surface (Ctrl+K or
  Ctrl+Space: quick open, run an agent, Stop everything).
- Windows: Today (morning brief + KPIs per location), Approvals (tier-2 proposals with their hash, typed APPROVE),
  Payroll, Reconciliation, Franchise rollup, Inbox, Receipts (chain state), Ask, Settings (pack switch, about).
- If the runtime can't be reached, the desktop says so (`NO SIGNAL from aios`) and never shows invented numbers.
- Works at 1280×720 and up; on a phone-sized viewport it shows a single-column "open this on a computer" view with
  screenshots of the desktop rather than breaking.

## Release

Tag `vX.Y.Z` → workflow builds `aios` for windows/amd64, darwin/arm64, darwin/amd64, linux/amd64, linux/arm64,
packages `smb-os-desktop_<ver>_<os>_<arch>.zip` (binary + `README.txt`), writes `SHA256SUMS.txt`, and publishes a
GitHub release. `scripts/install.sh` (macOS/Linux) and `scripts/install.ps1` (Windows) fetch the right asset from
`releases/latest/download/`, verify its SHA-256 against `SHA256SUMS.txt`, and start it. Command-line downloads don't
carry the browser quarantine mark, so they skip the Gatekeeper/SmartScreen prompt; the README states that plainly.

## As built in v0.1.0 (beyond the table above)

- Receipts carry an optional `ref` (the run id or proposal hash, covered by the hash). Kinds also include `undo`, `lock`, `unlock`.
- `/api/health` also returns `pending_approvals`, `instance`, and `model` when the AI model is on.
- `/api/state` also returns `royalty_base`, `revenue`, `payroll`, `bank`, `ledger`, `drafts`, `saved_matches`, `sent`,
  `pending_approvals`; AR items carry `location` and `contact`; inbox items carry `location` and `preview`.
- The pack-switch response carries `run_id` and `receipt`, so a switch can be undone. Decline returns its receipt;
  lock and unlock return `{status, locked, receipt}`.
- Further 409 codes: `already_used` (re-proposing an instruction that already ran), `no_drafts`, `chain_broken`
  (approvals freeze while the receipt chain does not verify), `hash_mismatch`, `not_pending`, `superseded`.
- The Origin check also applies to `GET /api/*`. Lock also refuses undo and ask; decline stays allowed; the lock
  survives a restart.
- "Reference contains" means the ledger reference appears as whole words in the bank description.
- `aios.lock` in the data folder: a second start opens the browser on the copy already running and exits 0.
- Builds without release flags report version `0.1.0-dev`. `ANTHROPIC_BASE_URL` is honoured.
- Releases also ship version-less zips (`smb-os-desktop_<os>_<arch>.zip`, same bytes) so
  `releases/latest/download/` links never need editing, and the installers are release assets covered by `SHA256SUMS.txt`.
