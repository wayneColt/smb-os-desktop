package agents

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/wayneColt/smb-os-desktop/internal/money"
	"github.com/wayneColt/smb-os-desktop/internal/pack"
)

// MatchWindowDays is the largest date gap a bank line and a ledger entry may
// have and still match.
const MatchWindowDays = 3

// Exception types.
const (
	ExBankFee       = "bank_fee"
	ExInterest      = "interest"
	ExTransposition = "transposition"
	ExTiming        = "timing"
	ExDuplicate     = "duplicate"
)

// Match pairs one bank line with one ledger entry.
type Match struct {
	BankID     string      `json:"bank_id"`
	LedgerID   string      `json:"ledger_id"`
	Amount     money.Cents `json:"amount"`
	BankDate   string      `json:"bank_date"`
	LedgerDate string      `json:"ledger_date"`
	DaysApart  int         `json:"days_apart"`
	Ref        string      `json:"ref"`
}

// BankItem is an unmatched bank line.
type BankItem struct {
	ID          string      `json:"id"`
	Date        string      `json:"date"`
	Amount      money.Cents `json:"amount"`
	Description string      `json:"description"`
	Hint        string      `json:"hint"`
}

// LedgerItem is an unmatched ledger entry.
type LedgerItem struct {
	ID      string      `json:"id"`
	Date    string      `json:"date"`
	Amount  money.Cents `json:"amount"`
	Memo    string      `json:"memo"`
	Ref     string      `json:"ref"`
	Account string      `json:"account"`
	Hint    string      `json:"hint"`
}

// Exception explains why something did not match.
type Exception struct {
	Type     string `json:"type"`
	BankID   string `json:"bank_id,omitempty"`
	LedgerID string `json:"ledger_id,omitempty"`
	Text     string `json:"text"`
}

// Suggestion is a journal entry the books are missing.
type Suggestion struct {
	BankID string      `json:"bank_id"`
	Date   string      `json:"date"`
	Amount money.Cents `json:"amount"`
	Debit  string      `json:"debit"`
	Credit string      `json:"credit"`
	Memo   string      `json:"memo"`
}

// ReconTotals adds up both sides.
type ReconTotals struct {
	BankLines       int         `json:"bank_lines"`
	LedgerEntries   int         `json:"ledger_entries"`
	Matched         int         `json:"matched"`
	BankNet         money.Cents `json:"bank_net"`
	LedgerNet       money.Cents `json:"ledger_net"`
	UnmatchedBank   money.Cents `json:"unmatched_bank_net"`
	UnmatchedLedger money.Cents `json:"unmatched_ledger_net"`
}

// Recon is the reconciliation output.
type Recon struct {
	Account         string       `json:"account"`
	From            string       `json:"from"`
	To              string       `json:"to"`
	Rules           []string     `json:"rules"`
	Matched         []Match      `json:"matched"`
	UnmatchedBank   []BankItem   `json:"unmatched_bank"`
	UnmatchedLedger []LedgerItem `json:"unmatched_ledger"`
	Exceptions      []Exception  `json:"exceptions"`
	Suggested       []Suggestion `json:"suggested_entries"`
	Totals          ReconTotals  `json:"totals"`
	Summary         string       `json:"summary"`
	Applied         bool         `json:"applied"`
	SavedMatches    int          `json:"saved_matches"`
}

// RunReconcile matches the bank export to the ledger.
//
// A bank line and a ledger entry match when the amounts are exactly equal,
// the dates are at most MatchWindowDays apart, and the ledger reference
// appears in the bank description. Each side is used at most once; bank lines
// are taken in date order and each takes the closest-dated candidate (ties to
// the lower ledger id), so the result is deterministic.
func RunReconcile(p *pack.Pack) Recon {
	r := Recon{
		Account: p.Bank.Account, From: p.Bank.From, To: p.Bank.To,
		Rules: []string{
			"amounts are exactly equal",
			fmt.Sprintf("dates are at most %d days apart", MatchWindowDays),
			"the ledger reference appears in the bank description",
		},
		Matched: []Match{}, UnmatchedBank: []BankItem{}, UnmatchedLedger: []LedgerItem{},
		Exceptions: []Exception{}, Suggested: []Suggestion{},
	}
	bank := append([]pack.BankLine(nil), p.Bank.Lines...)
	sort.SliceStable(bank, func(i, j int) bool {
		if bank[i].Date != bank[j].Date {
			return bank[i].Date < bank[j].Date
		}
		return bank[i].ID < bank[j].ID
	})
	ledger := append([]pack.Ledger(nil), p.Ledger...)
	sort.SliceStable(ledger, func(i, j int) bool { return ledger[i].ID < ledger[j].ID })

	usedL := map[string]bool{}
	matchedB := map[string]bool{}
	matchOf := map[string]pack.Ledger{} // ledger id -> entry, for duplicate detection
	for _, b := range bank {
		best := -1
		bestDays := 0
		for i, l := range ledger {
			if usedL[l.ID] || l.Amount != b.Amount || !refIn(l.Ref, b.Description) {
				continue
			}
			d := daysApart(b.Date, l.Date)
			if d > MatchWindowDays {
				continue
			}
			if best < 0 || d < bestDays {
				best, bestDays = i, d
			}
		}
		if best < 0 {
			continue
		}
		l := ledger[best]
		usedL[l.ID] = true
		matchedB[b.ID] = true
		matchOf[l.ID] = l
		r.Matched = append(r.Matched, Match{BankID: b.ID, LedgerID: l.ID, Amount: b.Amount,
			BankDate: b.Date, LedgerDate: l.Date, DaysApart: bestDays, Ref: l.Ref})
	}

	bankHint := map[string]string{}
	ledgerHint := map[string]string{}
	paired := map[string]bool{} // ids already explained by a pair exception
	for _, b := range bank {
		if matchedB[b.ID] {
			continue
		}
		for _, l := range ledger {
			if usedL[l.ID] || paired[l.ID] || !refIn(l.Ref, b.Description) {
				continue
			}
			d := daysApart(b.Date, l.Date)
			switch {
			case l.Amount == b.Amount && d > MatchWindowDays:
				r.Exceptions = append(r.Exceptions, Exception{Type: ExTiming, BankID: b.ID, LedgerID: l.ID,
					Text: fmt.Sprintf("Timing difference: %s for %s is on the books %s and cleared the bank %s, %d days apart (outside the %d-day window). Confirm it and match it by hand.",
						l.Ref, b.Amount.Abs().String(), shortDate(l.Date), shortDate(b.Date), d, MatchWindowDays)})
				bankHint[b.ID] = "timing difference with " + l.ID
				ledgerHint[l.ID] = "timing difference with " + b.ID
			case l.Amount != b.Amount && d <= MatchWindowDays && transposed(b.Amount, l.Amount):
				r.Exceptions = append(r.Exceptions, Exception{Type: ExTransposition, BankID: b.ID, LedgerID: l.ID,
					Text: fmt.Sprintf("Likely transposed digits: %s is %s on the books but %s at the bank (difference %s, divisible by 9). Correct the ledger entry.",
						l.Ref, l.Amount.Abs().String(), b.Amount.Abs().String(), (l.Amount - b.Amount).Abs().String())})
				bankHint[b.ID] = "amount differs from " + l.ID + " (transposed digits?)"
				ledgerHint[l.ID] = "amount differs from " + b.ID + " (transposed digits?)"
			default:
				continue
			}
			paired[l.ID] = true
			paired[b.ID] = true
			break
		}
	}
	for _, l := range ledger {
		if usedL[l.ID] || paired[l.ID] {
			continue
		}
		for _, m := range ledger {
			orig, ok := matchOf[m.ID]
			if !ok || orig.Amount != l.Amount || orig.Date != l.Date || norm(orig.Ref) != norm(l.Ref) {
				continue
			}
			r.Exceptions = append(r.Exceptions, Exception{Type: ExDuplicate, LedgerID: l.ID,
				Text: fmt.Sprintf("Duplicate entry: %s repeats %s (%s for %s on %s) and the bank shows it once. Void the duplicate.",
					l.ID, orig.ID, l.Ref, l.Amount.Abs().String(), shortDate(l.Date))})
			ledgerHint[l.ID] = "duplicate of " + orig.ID
			paired[l.ID] = true
			break
		}
	}
	for _, b := range bank {
		if matchedB[b.ID] || paired[b.ID] {
			continue
		}
		words := strings.Fields(strings.ToUpper(b.Description))
		switch {
		case b.Amount < 0 && (hasWord(words, "FEE") || hasWord(words, "CHARGE")):
			r.Exceptions = append(r.Exceptions, Exception{Type: ExBankFee, BankID: b.ID,
				Text: fmt.Sprintf("Bank fee of %s on %s is not in the books.", b.Amount.Abs().String(), shortDate(b.Date))})
			r.Suggested = append(r.Suggested, Suggestion{BankID: b.ID, Date: b.Date, Amount: b.Amount.Abs(),
				Debit: "Bank service charges", Credit: "Operating cash",
				Memo: fmt.Sprintf("%s per bank statement", titleCase(b.Description))})
			bankHint[b.ID] = "bank fee; entry suggested"
		case b.Amount > 0 && hasWord(words, "INTEREST"):
			r.Exceptions = append(r.Exceptions, Exception{Type: ExInterest, BankID: b.ID,
				Text: fmt.Sprintf("Interest of %s on %s is not in the books.", b.Amount.String(), shortDate(b.Date))})
			r.Suggested = append(r.Suggested, Suggestion{BankID: b.ID, Date: b.Date, Amount: b.Amount,
				Debit: "Operating cash", Credit: "Interest income",
				Memo: fmt.Sprintf("%s per bank statement", titleCase(b.Description))})
			bankHint[b.ID] = "interest; entry suggested"
		}
	}

	t := &r.Totals
	t.BankLines, t.LedgerEntries, t.Matched = len(bank), len(ledger), len(r.Matched)
	for _, b := range bank {
		t.BankNet += b.Amount
		if !matchedB[b.ID] {
			h := bankHint[b.ID]
			if h == "" {
				h = "no ledger entry with this amount and reference"
			}
			r.UnmatchedBank = append(r.UnmatchedBank, BankItem{ID: b.ID, Date: b.Date, Amount: b.Amount, Description: b.Description, Hint: h})
			t.UnmatchedBank += b.Amount
		}
	}
	for _, l := range ledger {
		t.LedgerNet += l.Amount
		if !usedL[l.ID] {
			h := ledgerHint[l.ID]
			if h == "" {
				h = "not on the bank export yet"
			}
			r.UnmatchedLedger = append(r.UnmatchedLedger, LedgerItem{ID: l.ID, Date: l.Date, Amount: l.Amount,
				Memo: l.Memo, Ref: l.Ref, Account: l.Account, Hint: h})
			t.UnmatchedLedger += l.Amount
		}
	}
	sort.SliceStable(r.UnmatchedLedger, func(i, j int) bool {
		a, b := r.UnmatchedLedger[i], r.UnmatchedLedger[j]
		if a.Date != b.Date {
			return a.Date < b.Date
		}
		return a.ID < b.ID
	})
	r.Summary = fmt.Sprintf("%d of %d bank lines matched; %d bank lines and %d ledger entries unmatched; %s; %s.",
		t.Matched, t.BankLines, len(r.UnmatchedBank), len(r.UnmatchedLedger),
		plural(len(r.Exceptions), "exception explained", "exceptions explained"),
		plural(len(r.Suggested), "entry suggested", "entries suggested"))
	return r
}

// refIn reports whether a ledger reference appears in a bank description,
// ignoring case and spacing.
func refIn(ref, desc string) bool {
	ref = norm(ref)
	return ref != "" && strings.Contains(" "+norm(desc)+" ", " "+ref+" ")
}

func norm(s string) string { return strings.Join(strings.Fields(strings.ToUpper(s)), " ") }

func hasWord(words []string, w string) bool {
	for _, x := range words {
		if strings.Trim(x, ".,:;") == w {
			return true
		}
	}
	return false
}

// transposed reports whether a and b differ by swapping two adjacent digits.
func transposed(a, b money.Cents) bool {
	if (a < 0) != (b < 0) {
		return false
	}
	sa := strconv.FormatInt(int64(a.Abs()), 10)
	sb := strconv.FormatInt(int64(b.Abs()), 10)
	if len(sa) != len(sb) || sa == sb {
		return false
	}
	for i := 0; i+1 < len(sa); i++ {
		if sa[i] != sb[i] {
			return sa[i] == sb[i+1] && sa[i+1] == sb[i] && sa[i+2:] == sb[i+2:]
		}
	}
	return false
}

func titleCase(s string) string {
	words := strings.Fields(strings.ToLower(s))
	for i, w := range words {
		if w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}
