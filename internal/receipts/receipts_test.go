package receipts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func clock() func() time.Time {
	t0 := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	n := 0
	return func() time.Time { n++; return t0.Add(time.Duration(n) * time.Second) }
}

func writeChain(t *testing.T, n int) (*Log, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "receipts.jsonl")
	l, err := Open(path, clock())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		if _, err := l.Append(Record{Kind: KindRun, Agent: "morning-brief", Tier: 0, Summary: "brief " + string(rune('a'+i))}); err != nil {
			t.Fatal(err)
		}
	}
	return l, path
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(b), "\n"), "\n")
}

func writeLines(t *testing.T, path string, lines []string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestChainVerifies(t *testing.T) {
	l, path := writeChain(t, 5)
	defer l.Close()
	res := VerifyFile(path)
	if !res.OK || res.Count != 5 || res.LastSeq != 5 {
		t.Fatalf("verify: %+v", res)
	}
	lines := readLines(t, path)
	var first, second Record
	_ = json.Unmarshal([]byte(lines[0]), &first)
	_ = json.Unmarshal([]byte(lines[1]), &second)
	if first.Prev != Genesis || first.Seq != 1 {
		t.Fatalf("first record: %+v", first)
	}
	if second.Prev != first.Hash {
		t.Fatal("second record does not chain to the first")
	}
	if st := l.Status(); !st.OK || st.Count != 5 {
		t.Fatalf("status: %+v", st)
	}
}

func TestHashMatchesTheContractFormula(t *testing.T) {
	l, path := writeChain(t, 1)
	defer l.Close()
	var fields map[string]any
	_ = json.Unmarshal([]byte(readLines(t, path)[0]), &fields)
	want, _ := ComputeHash(fields)
	if fields["hash"] != want {
		t.Fatalf("hash %v, recomputed %s", fields["hash"], want)
	}
}

func TestEmptyAndMissingChainsAreValid(t *testing.T) {
	if res := VerifyFile(filepath.Join(t.TempDir(), "none.jsonl")); !res.OK || res.Count != 0 {
		t.Fatalf("%+v", res)
	}
	if res := Verify(strings.NewReader("")); !res.OK {
		t.Fatalf("%+v", res)
	}
}

func TestTamperedSummaryBreaksTheChain(t *testing.T) {
	l, path := writeChain(t, 4)
	defer l.Close()
	lines := readLines(t, path)
	lines[1] = strings.Replace(lines[1], `"summary":"brief b"`, `"summary":"brief B"`, 1)
	writeLines(t, path, lines)
	res := VerifyFile(path)
	if res.OK || res.BadLine != 2 {
		t.Fatalf("tampered line 2 not detected: %+v", res)
	}
	if !strings.Contains(res.Reason, "hash") {
		t.Fatalf("reason: %s", res.Reason)
	}
}

func TestTamperedTierBreaksTheChain(t *testing.T) {
	l, path := writeChain(t, 3)
	defer l.Close()
	lines := readLines(t, path)
	lines[2] = strings.Replace(lines[2], `"tier":0`, `"tier":2`, 1)
	writeLines(t, path, lines)
	if res := VerifyFile(path); res.OK || res.BadLine != 3 {
		t.Fatalf("%+v", res)
	}
}

func TestRewrittenHashIsCaughtByTheNextLink(t *testing.T) {
	// Recomputing the tampered line's own hash is not enough: the next record
	// still points at the original.
	l, path := writeChain(t, 3)
	defer l.Close()
	lines := readLines(t, path)
	var f map[string]any
	_ = json.Unmarshal([]byte(lines[0]), &f)
	f["summary"] = "forged"
	delete(f, "hash")
	f["hash"], _ = ComputeHash(f)
	forged, _ := json.Marshal(f)
	lines[0] = string(forged)
	writeLines(t, path, lines)
	if res := VerifyFile(path); res.OK || res.BadLine != 2 {
		t.Fatalf("%+v", res)
	}
}

func TestDeletedReorderedAndTruncatedLines(t *testing.T) {
	l, path := writeChain(t, 4)
	l.Close()
	orig := readLines(t, path)

	writeLines(t, path, append([]string{orig[0]}, orig[2:]...))
	if res := VerifyFile(path); res.OK || res.BadLine != 2 {
		t.Fatalf("deleted line: %+v", res)
	}
	writeLines(t, path, []string{orig[1], orig[0], orig[2], orig[3]})
	if res := VerifyFile(path); res.OK || res.BadLine != 1 {
		t.Fatalf("reordered: %+v", res)
	}
	partial := strings.Join(orig, "\n")
	if err := os.WriteFile(path, []byte(partial[:len(partial)-10]), 0o600); err != nil {
		t.Fatal(err)
	}
	if res := VerifyFile(path); res.OK || res.BadLine != 4 {
		t.Fatalf("truncated: %+v", res)
	}
}

func TestStatusSeesExternalTampering(t *testing.T) {
	l, path := writeChain(t, 3)
	defer l.Close()
	if !l.Status().OK {
		t.Fatal("fresh chain should verify")
	}
	lines := readLines(t, path)
	lines[0] = strings.Replace(lines[0], "brief a", "brief z", 1)
	writeLines(t, path, lines)
	// Make sure the modification time differs from the cached one even on
	// coarse-grained filesystems.
	future := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(path, future, future)
	if st := l.Status(); st.OK {
		t.Fatal("Status kept a stale OK after the file was edited")
	}
}

func TestAppendAfterReopenContinuesTheChain(t *testing.T) {
	l, path := writeChain(t, 2)
	l.Close()
	l2, err := Open(path, clock())
	if err != nil {
		t.Fatal(err)
	}
	defer l2.Close()
	r, err := l2.Append(Record{Kind: KindPack, Agent: "settings", Tier: 1, Summary: "switch"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Seq != 3 {
		t.Fatalf("seq %d", r.Seq)
	}
	if res := VerifyFile(path); !res.OK || res.Count != 3 {
		t.Fatalf("%+v", res)
	}
	recs, _ := l2.Records(2)
	var newest Record
	_ = json.Unmarshal(recs[0], &newest)
	if len(recs) != 2 || newest.Seq != 3 {
		t.Fatalf("records newest first: %d %d", len(recs), newest.Seq)
	}
}

func TestAppendFollowsAReplacedFile(t *testing.T) {
	l, path := writeChain(t, 2)
	defer l.Close()
	// Replace the file with an identical copy through a rename, as an editor
	// or a restore does: appends must land in the new file and still verify.
	b, _ := os.ReadFile(path)
	tmp := path + ".tmp"
	_ = os.WriteFile(tmp, b, 0o600)
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Append(Record{Kind: KindRun, Agent: "a", Summary: "after replace"}); err != nil {
		t.Fatal(err)
	}
	if res := VerifyFile(path); !res.OK || res.Count != 3 {
		t.Fatalf("append after an identical replacement: %+v", res)
	}
	// Replace it with a copy missing the last record: the next append must
	// not splice over the gap.
	lines := readLines(t, path)
	_ = os.WriteFile(tmp, []byte(strings.Join(lines[:2], "\n")+"\n"), 0o600)
	_ = os.Rename(tmp, path)
	if _, err := l.Append(Record{Kind: KindRun, Agent: "a", Summary: "after truncation"}); err != nil {
		t.Fatal(err)
	}
	if res := VerifyFile(path); res.OK || res.Count != 3 {
		t.Fatalf("a dropped record must show as a break: %+v", res)
	}
	// Delete it outright: the next receipt is written to a new, visible file
	// that does not verify on its own.
	_ = os.Remove(path)
	if _, err := l.Append(Record{Kind: KindRun, Agent: "a", Summary: "after delete"}); err != nil {
		t.Fatal(err)
	}
	if res := VerifyFile(path); res.OK || res.Count != 1 {
		t.Fatalf("after deletion: %+v", res)
	}
}
