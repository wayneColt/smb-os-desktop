// Package receipts is the append-only, hash-chained record of everything aios
// does. Each line of receipts.jsonl is one record:
//
//	{"seq":N,"ts":"RFC3339","kind":"...","agent":"...","tier":0,"summary":"...","prev":"<hex>","hash":"<hex>"}
//
// where hash = sha256(prev + canonical_json(record without "hash")) and prev
// is the previous record's hash (64 zeros for the first record). Changing,
// removing, reordering or inserting any line breaks the chain from that line
// on, and Verify reports where.
package receipts

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/wayneColt/smb-os-desktop/internal/canon"
)

// Genesis is the prev value of the first record.
const Genesis = "0000000000000000000000000000000000000000000000000000000000000000"

// Record kinds.
const (
	KindRun      = "run"
	KindProposal = "proposal"
	KindApprove  = "approve"
	KindDecline  = "decline"
	KindPack     = "pack"
	KindUndo     = "undo"
	KindLock     = "lock"
	KindUnlock   = "unlock"
)

// Record is one receipt. Ref names the proposal hash or run id a record is
// about, when there is one.
type Record struct {
	Seq     int64  `json:"seq"`
	TS      string `json:"ts"`
	Kind    string `json:"kind"`
	Agent   string `json:"agent"`
	Tier    int    `json:"tier"`
	Summary string `json:"summary"`
	Ref     string `json:"ref,omitempty"`
	Prev    string `json:"prev"`
	Hash    string `json:"hash"`
}

// Result is the outcome of verifying a chain.
type Result struct {
	OK      bool   `json:"ok"`
	Count   int64  `json:"count"` // lines read
	Head    string `json:"head"`  // hash of the last line (as written on it)
	LastSeq int64  `json:"last_seq"`
	BadLine int64  `json:"bad_line,omitempty"` // 1-based line of the first break
	Reason  string `json:"reason,omitempty"`
}

// ComputeHash returns the hash a record must carry: sha256(prev +
// canonical_json(fields without "hash")). It works on a generic object so
// that any field on the line, known or not, is covered.
func ComputeHash(fields map[string]any) (string, error) {
	prev, _ := fields["prev"].(string)
	body := make(map[string]any, len(fields))
	for k, v := range fields {
		if k != "hash" {
			body[k] = v
		}
	}
	c, err := canon.Marshal(body)
	if err != nil {
		return "", err
	}
	return canon.SHA256Hex(append([]byte(prev), c...)), nil
}

// Verify reads a chain from r and checks every link.
func Verify(r io.Reader) Result {
	res := Result{OK: true, Head: Genesis}
	br := bufio.NewReader(r)
	expectPrev := Genesis
	var line int64
	fail := func(reason string) Result {
		if res.OK {
			res.OK = false
			res.BadLine = line
			res.Reason = reason
		}
		return res
	}
	for {
		raw, err := br.ReadBytes('\n')
		if len(raw) == 0 && err != nil {
			if !errors.Is(err, io.EOF) {
				return fail("read error: " + err.Error())
			}
			break
		}
		line++
		res.Count = line
		partial := !bytes.HasSuffix(raw, []byte("\n"))
		raw = bytes.TrimRight(raw, "\r\n")
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		var fields map[string]any
		if derr := dec.Decode(&fields); derr != nil || fields == nil {
			fail("line is not a JSON object")
			continue
		}
		hash, _ := fields["hash"].(string)
		prev, _ := fields["prev"].(string)
		seqNum, _ := fields["seq"].(json.Number)
		seq, serr := seqNum.Int64()
		if hash != "" {
			res.Head = hash
		}
		if serr == nil {
			res.LastSeq = seq
		}
		switch {
		case partial:
			fail("last line is incomplete (no newline)")
		case serr != nil:
			fail("seq is missing or not an integer")
		case seq != line:
			fail(fmt.Sprintf("seq %d where %d was expected", seq, line))
		case prev != expectPrev:
			fail("prev does not match the previous record's hash")
		default:
			want, herr := ComputeHash(fields)
			if herr != nil {
				fail("cannot canonicalize record: " + herr.Error())
			} else if want != hash {
				fail("hash does not match the record's contents")
			}
		}
		expectPrev = hash
		if err != nil {
			break
		}
	}
	return res
}

// VerifyFile verifies the chain stored at path. A missing file is an empty,
// valid chain.
func VerifyFile(path string) Result {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Result{OK: true, Head: Genesis}
		}
		return Result{OK: false, Head: Genesis, Reason: "cannot open receipts: " + err.Error()}
	}
	defer f.Close()
	return Verify(f)
}

// Log appends receipts to a file and keeps a cached verification result that
// is recomputed whenever the file changes underneath it.
type Log struct {
	mu   sync.Mutex
	path string
	f    *os.File
	now  func() time.Time

	seq  int64  // seq of the last record written
	head string // hash of the last record written

	cacheKey   fileKey
	cacheValid bool
	cache      Result
}

type fileKey struct {
	size  int64
	mtime int64
}

// Open opens (creating if needed) the receipt log at path. A chain that does
// not verify is still opened — the break is reported by Status, and new
// records keep chaining from the last line written.
func Open(path string, now func() time.Time) (*Log, error) {
	if now == nil {
		now = time.Now
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	l := &Log{path: path, f: f, now: now, head: Genesis}
	res := VerifyFile(path)
	l.seq, l.head = res.LastSeq, res.Head
	if res.Count == 0 {
		l.seq, l.head = 0, Genesis
	}
	l.remember(res)
	return l, nil
}

// Path returns the file path of the log.
func (l *Log) Path() string { return l.path }

// Append writes a new record built from r (Seq, TS, Prev and Hash are
// assigned here) and returns it as written.
func (l *Log) Append(r Record) (Record, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return Record{}, errors.New("receipts: log is closed")
	}
	if err := l.ensureFile(); err != nil {
		return Record{}, fmt.Errorf("receipts: reopen: %w", err)
	}
	r.Seq = l.seq + 1
	r.TS = l.now().UTC().Format(time.RFC3339)
	r.Prev = l.head
	r.Hash = ""
	fields, err := toFields(r)
	if err != nil {
		return Record{}, err
	}
	if r.Hash, err = ComputeHash(fields); err != nil {
		return Record{}, err
	}
	line, err := json.Marshal(r)
	if err != nil {
		return Record{}, err
	}
	line = append(line, '\n')
	before, statErr := l.stat()
	// One write call per record: with O_APPEND a crash leaves at most one
	// incomplete trailing line, which Verify reports.
	if _, err := l.f.Write(line); err != nil {
		return Record{}, fmt.Errorf("receipts: write: %w", err)
	}
	if err := l.f.Sync(); err != nil {
		return Record{}, fmt.Errorf("receipts: sync: %w", err)
	}
	l.seq, l.head = r.Seq, r.Hash
	// Extend the cached result instead of re-reading the whole file, but only
	// if nothing else touched the file since it was last verified.
	if after, err := l.stat(); err == nil && statErr == nil && l.cacheValid &&
		before == l.cacheKey && after.size == before.size+int64(len(line)) {
		l.cache.Count++
		l.cache.LastSeq = r.Seq
		l.cache.Head = r.Hash
		l.cacheKey = after
	} else {
		l.cacheValid = false
	}
	return r, nil
}

// ensureFile makes sure appends go to the file that is at the path now. If
// the file was replaced (an editor's save, a restore, a sync tool) or
// deleted, the old handle would write into a file nobody can see, and every
// later receipt would be lost without a trace. Reopening keeps appends
// visible; chaining continues from the last record this log wrote, so a file
// whose contents changed underneath it shows up as a break, not a silent
// splice.
func (l *Log) ensureFile() error {
	fi, ferr := l.f.Stat()
	pi, perr := os.Stat(l.path)
	if ferr == nil && perr == nil && os.SameFile(fi, pi) {
		return nil
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_ = l.f.Close()
	l.f = f
	l.cacheValid = false
	return nil
}

// Status verifies the chain on disk (cached by file size and modification
// time) and returns the result.
func (l *Log) Status() Result {
	l.mu.Lock()
	defer l.mu.Unlock()
	key, err := l.stat()
	if err == nil && l.cacheValid && key == l.cacheKey {
		return l.cache
	}
	res := VerifyFile(l.path)
	l.remember(res)
	return res
}

func (l *Log) remember(res Result) {
	key, err := l.stat()
	if err != nil {
		l.cacheValid = false
		return
	}
	l.cache, l.cacheKey, l.cacheValid = res, key, true
}

func (l *Log) stat() (fileKey, error) {
	fi, err := os.Stat(l.path)
	if err != nil {
		return fileKey{}, err
	}
	return fileKey{size: fi.Size(), mtime: fi.ModTime().UnixNano()}, nil
}

// Records returns up to limit records, newest first, exactly as stored.
// Lines that are not JSON objects are returned as {"seq":null,"unreadable":"..."}
// so that a damaged file is visible rather than hidden.
func (l *Log) Records(limit int) ([]json.RawMessage, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	data, err := os.ReadFile(l.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []json.RawMessage{}, nil
		}
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}
	out := make([]json.RawMessage, 0, min(limit, len(lines)))
	for i := len(lines) - 1; i >= 0 && len(out) < limit; i-- {
		ln := strings.TrimRight(lines[i], "\r")
		if json.Valid([]byte(ln)) && strings.HasPrefix(strings.TrimSpace(ln), "{") {
			out = append(out, json.RawMessage(ln))
			continue
		}
		bad, _ := json.Marshal(map[string]any{"seq": nil, "unreadable": truncate(ln, 200)})
		out = append(out, bad)
	}
	return out, nil
}

// Each calls fn for every parseable record in file order. It is used at
// start-up to rebuild facts (such as which proposals already ran) from the
// chain.
func (l *Log) Each(fn func(Record)) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.Open(l.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var r Record
		if json.Unmarshal(sc.Bytes(), &r) == nil {
			fn(r)
		}
	}
	return sc.Err()
}

// Close flushes and closes the log.
func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}

func toFields(r Record) (map[string]any, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
