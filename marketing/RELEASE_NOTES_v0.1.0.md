# SMB OS Desktop 0.1.0

**Business intelligence that runs on your computer, and answers to you.**

The first release, for the owner of a small multi-location franchise. Agents on your own computer prepare
the morning numbers for every location, check payroll against the schedule, reconcile the bank, draft
reminders for late invoices and do the franchise rollup. They read, reconcile, prepare and draft; anything
that would move money, send something outside or can't be undone waits at a gate only you can open.

It is one file: double-click it and the desktop opens in your browser, with the data in a folder on your
machine. This version runs on two fictional demo businesses, an auto service franchise and a home care
franchise. It does not connect to your own systems yet.

## Download

| Your computer | File |
|---|---|
| Windows (x86-64) | `smb-os-desktop_0.1.0_windows_amd64.zip` |
| Mac with Apple silicon | `smb-os-desktop_0.1.0_darwin_arm64.zip` |
| Mac with an Intel processor | `smb-os-desktop_0.1.0_darwin_amd64.zip` |
| Linux (x86-64) | `smb-os-desktop_0.1.0_linux_amd64.zip` |
| Linux (ARM64) | `smb-os-desktop_0.1.0_linux_arm64.zip` |

Each zip is also attached under a version-less name (`smb-os-desktop_<os>_<arch>.zip`, same bytes), so
`releases/latest/download/` links always fetch the newest release. Checksums for all of them are in
`SHA256SUMS.txt`. Or install from a terminal; the installer checks the file against
`SHA256SUMS.txt` before it starts it:

```sh
curl -fsSL https://github.com/wayneColt/smb-os-desktop/releases/latest/download/install.sh | sh
```

```powershell
irm https://github.com/wayneColt/smb-os-desktop/releases/latest/download/install.ps1 | iex
```

These files are not code-signed yet. If you download in a browser, macOS and Windows ask before the first
run; the README explains the two clicks. The terminal installers don't trigger that prompt.

## What's in it

- **Today:** a morning brief and each location's numbers against target, worst first.
- **Agents, all rule-based:** morning brief, payroll pre-check, bank reconciliation, AR reminder drafts,
  send reminders, submit payroll, franchise rollup (revenue, royalty and ad fund per location).
- **Two planes, one authority.** You hold the authority; the agents hold none. Reading and calculating
  runs at once, and so does a reversible local change. Anything that would move money, leave your machine
  or can't be undone becomes a proposal bound to one exact action by the SHA-256 of its instruction; it
  runs only after you type `APPROVE`, and each fingerprint runs once. An agent can propose, never approve.
  The desktop lists what each agent may read, write and never touch, and Lock session stops every agent
  run and approval until you unlock. In this version the program on your computer enforces the gate, and an approved act writes to
  an outbox folder and contacts nothing.
- **The Apex command surface (Ctrl+K), in four bands:** Search; Operations, the business right now;
  Engine & workers, the agents and their state; Sovereignty, the approvals, whether the receipt chain is
  intact, where your data lives, what each agent may touch (network access: none) and Lock session.
- **Receipts:** every run, proposal, approval and decline is appended to `receipts.jsonl`, each line
  chained to the one before by SHA-256. The Receipts window shows whether the chain checks out.
- **Stays on your machine:** the program listens on `127.0.0.1` only, needs no account and sends nothing
  anywhere unless you turn on an AI model.
- **AI optional, off by default:** set `ANTHROPIC_API_KEY` before starting and the Ask window uses a Claude
  model; without it, Ask answers from the data with plain rules.

## Not in this version

Connecting to your scheduling, shop-management or accounting system or reading a bank export; sending
anything; the phone and web desk versions of the same design (next); separate accounts (anyone who can
use the computer can approve, and Lock session is a pause, not a password); signed binaries; automatic updates.

## License

MIT. The demo businesses, people and numbers are fictional.
