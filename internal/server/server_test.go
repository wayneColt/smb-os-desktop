package server

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/wayneColt/smb-os-desktop/internal/engine"
	"github.com/wayneColt/smb-os-desktop/web"
)

const port = 7701

var host = "127.0.0.1:" + strconv.Itoa(port)

func newServer(t *testing.T, web fstest.MapFS) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	eng, err := engine.Open(engine.Options{DataDir: dir, Pack: "auto", Version: "0.1.0"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { eng.Close() })
	if web == nil {
		web = fstest.MapFS{
			"index.html":    {Data: []byte("<!doctype html><title>desk</title>")},
			"app.js":        {Data: []byte("console.log(1)")},
			"ds/font.woff2": {Data: []byte("wOF2")},
			"embed.go":      {Data: []byte("package web")},
		}
	}
	return New(eng, web, port), dir
}

type call struct {
	method, path, body string
	header             map[string]string
	noAIOS             bool
	host               string
}

func do(t *testing.T, s *Server, c call) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var body io.Reader
	if c.body != "" {
		body = strings.NewReader(c.body)
	}
	req := httptest.NewRequest(c.method, "http://"+host+c.path, body)
	req.Host = host
	if c.host != "" {
		req.Host = c.host
	}
	if c.method != http.MethodGet && c.method != http.MethodHead && !c.noAIOS {
		req.Header.Set("X-AIOS", "1")
	}
	for k, v := range c.header {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	s.ServeHTTP(rr, req)
	var m map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &m)
	return rr, m
}

func TestGuards(t *testing.T) {
	s, _ := newServer(t, nil)
	cases := []struct {
		name   string
		c      call
		status int
		code   string
	}{
		{"bad host", call{method: "GET", path: "/api/health", host: "evil.example:7701"}, 403, "bad_host"},
		{"rebinding host with our port", call{method: "GET", path: "/", host: "attacker.test:7701"}, 403, "bad_host"},
		{"right name, wrong port", call{method: "GET", path: "/api/health", host: "127.0.0.1:9999"}, 403, "bad_host"}, // firewall:allow=localhost-port
		{"missing X-AIOS", call{method: "POST", path: "/api/pack", body: `{"id":"homecare"}`, noAIOS: true}, 403, "missing_header"},
		{"wrong X-AIOS", call{method: "POST", path: "/api/pack", body: `{"id":"homecare"}`, noAIOS: true, header: map[string]string{"X-AIOS": "yes"}}, 403, "missing_header"},
		{"cross origin POST", call{method: "POST", path: "/api/agents/payroll-submit/run", header: map[string]string{"Origin": "https://evil.example"}}, 403, "bad_origin"},
		{"cross origin GET", call{method: "GET", path: "/api/state", header: map[string]string{"Origin": "http://localhost:9999"}}, 403, "bad_origin"}, // firewall:allow=localhost-port
		{"preflight", call{method: "OPTIONS", path: "/api/pack", noAIOS: true, header: map[string]string{"Origin": "https://evil.example"}}, 403, "bad_origin"},
		{"same origin POST", call{method: "POST", path: "/api/pack", body: `{"id":"homecare"}`, header: map[string]string{"Origin": "http://" + host}}, 200, ""},
		{"localhost host", call{method: "GET", path: "/api/health", host: "localhost:7701"}, 200, ""},
	}
	for _, tc := range cases {
		rr, m := do(t, s, tc.c)
		if rr.Code != tc.status {
			t.Errorf("%s: status %d want %d (%s)", tc.name, rr.Code, tc.status, rr.Body.String())
			continue
		}
		if tc.code != "" && m["code"] != tc.code {
			t.Errorf("%s: code %v want %s", tc.name, m["code"], tc.code)
		}
		for k := range rr.Header() {
			if strings.HasPrefix(strings.ToLower(k), "access-control-") {
				t.Errorf("%s: sent a CORS header %s", tc.name, k)
			}
		}
	}
}

func TestGuardedRequestsChangeNothing(t *testing.T) {
	s, dir := newServer(t, nil)
	do(t, s, call{method: "POST", path: "/api/agents/ar-reminders/run", noAIOS: true})
	do(t, s, call{method: "POST", path: "/api/pack", body: `{"id":"homecare"}`, host: "evil.example:7701"})
	_, m := do(t, s, call{method: "GET", path: "/api/health"})
	if m["receipts"].(float64) != 0 || m["pack"] != "auto" {
		t.Fatalf("a refused request had an effect: %v", m)
	}
	if ents, _ := os.ReadDir(filepath.Join(dir, "outbox")); len(ents) != 0 {
		t.Fatal("outbox written")
	}
}

func TestHealthAndPacks(t *testing.T) {
	s, _ := newServer(t, nil)
	rr, m := do(t, s, call{method: "GET", path: "/api/health"})
	if rr.Code != 200 || m["ok"] != true || m["name"] != "aios" || m["version"] != "0.1.0" || m["pack"] != "auto" ||
		m["llm"] != "off" || m["chain_ok"] != true || m["receipts"].(float64) != 0 {
		t.Fatalf("health %v", m)
	}
	if rr.Header().Get("Cache-Control") != "no-store" || !strings.HasPrefix(rr.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("headers %v", rr.Header())
	}
	rr, _ = do(t, s, call{method: "GET", path: "/api/packs"})
	var packs []map[string]string
	_ = json.Unmarshal(rr.Body.Bytes(), &packs)
	if len(packs) != 2 || packs[0]["id"] != "auto" || packs[0]["label"] != "Auto service franchise" ||
		packs[1]["id"] != "homecare" || packs[1]["label"] != "Home care franchise" {
		t.Fatalf("packs %v", packs)
	}
}

func TestStateAndAgents(t *testing.T) {
	s, _ := newServer(t, nil)
	_, st := do(t, s, call{method: "GET", path: "/api/state"})
	for _, k := range []string{"pack", "label", "brand", "fictional", "as_of", "operator", "royalty_rate", "ad_fund_rate",
		"locations", "kpis", "alerts", "inbox", "schedule", "ar"} {
		if _, ok := st[k]; !ok {
			t.Errorf("state lacks %q", k)
		}
	}
	if st["fictional"] != true || st["brand"] != "Kestrel Auto Care" {
		t.Fatalf("state %v %v", st["fictional"], st["brand"])
	}
	kpi := st["kpis"].([]any)[0].(map[string]any)
	for _, k := range []string{"location", "key", "label", "value", "target", "unit", "better", "period"} {
		if _, ok := kpi[k]; !ok {
			t.Errorf("kpi lacks %q", k)
		}
	}
	rr, _ := do(t, s, call{method: "GET", path: "/api/agents"})
	var specs []map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &specs)
	if len(specs) != 7 {
		t.Fatalf("%d agents", len(specs))
	}
	for _, sp := range specs {
		for _, k := range []string{"id", "name", "tier", "description", "params"} {
			if _, ok := sp[k]; !ok {
				t.Errorf("agent %v lacks %q", sp["id"], k)
			}
		}
	}
}

func TestRunApproveCycleOverHTTP(t *testing.T) {
	s, dir := newServer(t, nil)
	rr, m := do(t, s, call{method: "POST", path: "/api/agents/morning-brief/run", body: `{"params":{}}`})
	if rr.Code != 200 || m["status"] != "done" || m["tier"].(float64) != 0 || m["run_id"] == "" {
		t.Fatalf("brief %d %v", rr.Code, m)
	}
	if rec := m["receipt"].(map[string]any); rec["seq"].(float64) != 1 || len(rec["hash"].(string)) != 64 {
		t.Fatalf("receipt %v", rec)
	}
	rr, m = do(t, s, call{method: "POST", path: "/api/agents/payroll-submit/run"})
	if rr.Code != 202 || m["status"] != "needs_approval" {
		t.Fatalf("tier 2 run: %d %v", rr.Code, m)
	}
	prop := m["proposal"].(map[string]any)
	hash := prop["hash"].(string)
	for _, k := range []string{"hash", "agent", "summary", "instruction", "created"} {
		if _, ok := prop[k]; !ok {
			t.Errorf("proposal lacks %q", k)
		}
	}
	rr, _ = do(t, s, call{method: "GET", path: "/api/proposals"})
	var pend []map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &pend)
	if len(pend) != 1 || pend[0]["hash"] != hash {
		t.Fatalf("pending %v", pend)
	}
	if ents, _ := os.ReadDir(filepath.Join(dir, "outbox")); len(ents) != 0 {
		t.Fatal("tier 2 executed on request")
	}
	if rr, m = do(t, s, call{method: "POST", path: "/api/proposals/" + hash + "/approve", body: `{"phrase":"approve"}`}); rr.Code != 422 || m["code"] != "wrong_phrase" {
		t.Fatalf("wrong phrase: %d %v", rr.Code, m)
	}
	if rr, _ = do(t, s, call{method: "POST", path: "/api/proposals/" + strings.Repeat("a", 64) + "/approve", body: `{"phrase":"APPROVE"}`}); rr.Code != 404 {
		t.Fatalf("unknown hash: %d", rr.Code)
	}
	rr, m = do(t, s, call{method: "POST", path: "/api/proposals/" + hash + "/approve", body: `{"phrase":"APPROVE"}`})
	if rr.Code != 200 || m["status"] != "done" || m["output"] == nil || m["receipt"].(map[string]any)["kind"] != "approve" {
		t.Fatalf("approve: %d %v", rr.Code, m)
	}
	if rr, m = do(t, s, call{method: "POST", path: "/api/proposals/" + hash + "/approve", body: `{"phrase":"APPROVE"}`}); rr.Code != 409 || m["code"] != "already_used" {
		t.Fatalf("second approve: %d %v", rr.Code, m)
	}
	if ents, _ := os.ReadDir(filepath.Join(dir, "outbox")); len(ents) != 2 {
		t.Fatalf("outbox has %d files", len(ents))
	}
}

func TestDeclineAndUndoOverHTTP(t *testing.T) {
	s, _ := newServer(t, nil)
	_, m := do(t, s, call{method: "POST", path: "/api/agents/ar-reminders/run"})
	runID := m["run_id"].(string)
	_, m = do(t, s, call{method: "POST", path: "/api/agents/send-reminders/run", body: `{"params":{"invoices":["inv-1043"]}}`})
	hash := m["proposal"].(map[string]any)["hash"].(string)
	if rr, m := do(t, s, call{method: "POST", path: "/api/proposals/" + hash + "/decline"}); rr.Code != 200 || m["status"] != "declined" {
		t.Fatalf("decline %d %v", rr.Code, m)
	}
	if rr, _ := do(t, s, call{method: "POST", path: "/api/proposals/" + hash + "/decline"}); rr.Code != 409 {
		t.Fatalf("second decline %d", rr.Code)
	}
	if rr, m := do(t, s, call{method: "POST", path: "/api/runs/" + runID + "/undo"}); rr.Code != 200 || m["status"] != "undone" {
		t.Fatalf("undo %d %v", rr.Code, m)
	}
	_, st := do(t, s, call{method: "GET", path: "/api/state"})
	if len(st["drafts"].([]any)) != 0 {
		t.Fatal("undo did not remove the drafts")
	}
}

func TestPackSwitchOverHTTP(t *testing.T) {
	s, _ := newServer(t, nil)
	rr, st := do(t, s, call{method: "POST", path: "/api/pack", body: `{"id":"homecare"}`})
	if rr.Code != 200 || st["pack"] != "homecare" || st["brand"] != "Larkmoor Home Care" {
		t.Fatalf("switch %d %v", rr.Code, st["pack"])
	}
	if rr, m := do(t, s, call{method: "POST", path: "/api/pack", body: `{"id":"bakery"}`}); rr.Code != 422 || m["code"] != "unknown_pack" {
		t.Fatalf("unknown pack %d %v", rr.Code, m)
	}
	_, h := do(t, s, call{method: "GET", path: "/api/health"})
	if h["pack"] != "homecare" {
		t.Fatalf("health pack %v", h["pack"])
	}
	_, m := do(t, s, call{method: "POST", path: "/api/agents/reconcile/run"})
	ex := m["output"].(map[string]any)["exceptions"].([]any)
	if len(ex) != 5 {
		t.Fatalf("homecare reconcile exceptions %d", len(ex))
	}
}

func TestReceiptsEndpointAndTamper(t *testing.T) {
	s, dir := newServer(t, nil)
	for i := 0; i < 3; i++ {
		do(t, s, call{method: "POST", path: "/api/agents/morning-brief/run"})
	}
	rr, m := do(t, s, call{method: "GET", path: "/api/receipts?limit=2"})
	recs := m["records"].([]any)
	if rr.Code != 200 || m["chain_ok"] != true || m["count"].(float64) != 3 || len(recs) != 2 ||
		recs[0].(map[string]any)["seq"].(float64) != 3 {
		t.Fatalf("receipts %d %v", rr.Code, m)
	}
	if rr, _ := do(t, s, call{method: "GET", path: "/api/receipts?limit=zero"}); rr.Code != 422 {
		t.Fatalf("bad limit %d", rr.Code)
	}
	path := filepath.Join(dir, "receipts.jsonl")
	raw, _ := os.ReadFile(path)
	lines := strings.Split(string(raw), "\n")
	lines[1] = strings.Replace(lines[1], `"kind":"run"`, `"kind":"pack"`, 1)
	_ = os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o600)
	future := time.Now().Add(3 * time.Second)
	_ = os.Chtimes(path, future, future)
	_, m = do(t, s, call{method: "GET", path: "/api/receipts"})
	if m["chain_ok"] != false || m["bad_line"].(float64) != 2 {
		t.Fatalf("tampered chain reported ok: %v", m)
	}
	_, h := do(t, s, call{method: "GET", path: "/api/health"})
	if h["chain_ok"] != false {
		t.Fatal("health chain_ok still true")
	}
}

func TestAskOverHTTP(t *testing.T) {
	s, _ := newServer(t, nil)
	rr, m := do(t, s, call{method: "POST", path: "/api/ask", body: `{"q":"which invoices are late?"}`})
	if rr.Code != 200 || m["mode"] != "deterministic" || m["answer"] == "" || len(m["sources"].([]any)) == 0 {
		t.Fatalf("ask %d %v", rr.Code, m)
	}
	if rr, _ := do(t, s, call{method: "POST", path: "/api/ask", body: `{}`}); rr.Code != 422 {
		t.Fatalf("empty q %d", rr.Code)
	}
}

func TestErrorsAreJSON(t *testing.T) {
	s, _ := newServer(t, nil)
	for _, tc := range []struct {
		c      call
		status int
		code   string
	}{
		{call{method: "GET", path: "/api/nope"}, 404, "not_found"},
		{call{method: "GET", path: "/api/pack"}, 405, "method_not_allowed"},
		{call{method: "POST", path: "/api/health"}, 405, "method_not_allowed"},
		{call{method: "POST", path: "/api/agents/nope/run"}, 404, "unknown_agent"},
		{call{method: "POST", path: "/api/pack", body: `{"id":`}, 400, "bad_json"},
		{call{method: "POST", path: "/api/pack", body: `{"id":"auto","extra":1}`}, 400, "bad_json"},
		{call{method: "POST", path: "/api/pack"}, 400, "bad_body"},
		{call{method: "POST", path: "/api/agents/reconcile/run", body: `{"params":{"apply":"yes"}}`}, 422, "bad_param"},
		{call{method: "POST", path: "/api/ask", body: `{"q":"` + strings.Repeat("x", MaxBody) + `"}`}, 413, "too_large"},
	} {
		rr, m := do(t, s, tc.c)
		if rr.Code != tc.status || m["code"] != tc.code || m["error"] == "" {
			t.Errorf("%s %s: %d %v", tc.c.method, tc.c.path, rr.Code, m)
		}
	}
}

func TestStaticFiles(t *testing.T) {
	s, _ := newServer(t, nil)
	rr, _ := do(t, s, call{method: "GET", path: "/"})
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "desk") || !strings.HasPrefix(rr.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("index %d %s", rr.Code, rr.Header().Get("Content-Type"))
	}
	if etag := rr.Header().Get("ETag"); etag == "" {
		t.Fatal("no ETag")
	}
	rr, _ = do(t, s, call{method: "GET", path: "/app.js"})
	if rr.Header().Get("Content-Type") != "text/javascript; charset=utf-8" {
		t.Fatalf("js type %q", rr.Header().Get("Content-Type"))
	}
	rr, _ = do(t, s, call{method: "GET", path: "/ds/font.woff2"})
	if rr.Code != 200 || rr.Header().Get("Content-Type") != "font/woff2" {
		t.Fatalf("font %d %q", rr.Code, rr.Header().Get("Content-Type"))
	}
	if rr, _ = do(t, s, call{method: "GET", path: "/missing.css"}); rr.Code != 404 {
		t.Fatalf("missing %d", rr.Code)
	}
	if rr, _ = do(t, s, call{method: "GET", path: "/ds/"}); rr.Code != 404 {
		t.Fatalf("directory listing %d", rr.Code)
	}
	if rr, _ = do(t, s, call{method: "GET", path: "/../../etc/passwd"}); rr.Code == 200 || strings.Contains(rr.Body.String(), "root:") {
		t.Fatalf("traversal %d", rr.Code)
	}
	if rr, _ = do(t, s, call{method: "POST", path: "/"}); rr.Code != 405 {
		t.Fatalf("POST static %d", rr.Code)
	}
}

func TestNoUIFallbackPage(t *testing.T) {
	s, _ := newServer(t, fstest.MapFS{})
	rr, _ := do(t, s, call{method: "GET", path: "/"})
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "does not include the desktop interface") {
		t.Fatalf("no-UI fallback: %d %s", rr.Code, rr.Body.String())
	}
}

func TestRealEmbedHidesGoSource(t *testing.T) {
	dir := t.TempDir()
	eng, err := engine.Open(engine.Options{DataDir: dir, Pack: "auto", Version: "0.1.0"})
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	s := New(eng, web.FS(), port)
	if rr, _ := do(t, s, call{method: "GET", path: "/embed.go"}); rr.Code != 404 {
		t.Fatalf("embed.go served: %d", rr.Code)
	}
	if rr, _ := do(t, s, call{method: "GET", path: "/"}); rr.Code != 200 {
		t.Fatalf("index: %d", rr.Code)
	}
}

func TestOverARealLoopbackListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := ln.Addr().(*net.TCPAddr).Port
	eng, err := engine.Open(engine.Options{DataDir: t.TempDir(), Pack: "auto", Version: "0.1.0"})
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	srv := httptest.NewUnstartedServer(New(eng, fstest.MapFS{}, p))
	srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	defer srv.Close()
	res, err := http.Get(srv.URL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("health over loopback %d", res.StatusCode)
	}
	req, _ := http.NewRequest("POST", srv.URL+"/api/agents/morning-brief/run", nil)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatalf("POST without X-AIOS over loopback %d", res.StatusCode)
	}
}

func TestLockCapabilitiesAndUndoOverHTTP(t *testing.T) {
	s, _ := newServer(t, nil)
	rr, _ := do(t, s, call{method: "GET", path: "/api/capabilities"})
	var caps []map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &caps)
	if rr.Code != 200 || len(caps) != 8 {
		t.Fatalf("capabilities %d %d", rr.Code, len(caps))
	}
	for _, k := range []string{"agent", "name", "tier", "reads", "writes", "never"} {
		if _, ok := caps[0][k]; !ok {
			t.Errorf("capability lacks %q", k)
		}
	}
	_, h := do(t, s, call{method: "GET", path: "/api/health"})
	if h["locked"] != false || !filepath.IsAbs(h["data_dir"].(string)) {
		t.Fatalf("health %v", h)
	}
	_, st := do(t, s, call{method: "POST", path: "/api/pack", body: `{"id":"homecare"}`})
	runID, _ := st["run_id"].(string)
	if runID == "" || st["receipt"] == nil {
		t.Fatalf("pack switch response lacks run_id/receipt: %v %v", st["run_id"], st["receipt"])
	}
	if rr, m := do(t, s, call{method: "POST", path: "/api/lock"}); rr.Code != 200 || m["locked"] != true || m["receipt"] == nil {
		t.Fatalf("lock %d %v", rr.Code, m)
	}
	for _, c := range []call{
		{method: "POST", path: "/api/agents/morning-brief/run"},
		{method: "POST", path: "/api/proposals/" + strings.Repeat("a", 64) + "/approve", body: `{"phrase":"APPROVE"}`},
		{method: "POST", path: "/api/runs/" + runID + "/undo"},
	} {
		if rr, m := do(t, s, c); rr.Code != 423 || m["code"] != "locked" {
			t.Errorf("%s while locked: %d %v", c.path, rr.Code, m)
		}
	}
	if _, h := do(t, s, call{method: "GET", path: "/api/health"}); h["locked"] != true {
		t.Fatal("health.locked not true")
	}
	if rr, m := do(t, s, call{method: "POST", path: "/api/unlock"}); rr.Code != 200 || m["locked"] != false {
		t.Fatalf("unlock %d %v", rr.Code, m)
	}
	if rr, _ := do(t, s, call{method: "POST", path: "/api/runs/" + runID + "/undo"}); rr.Code != 200 {
		t.Fatalf("undo pack switch %d", rr.Code)
	}
	if _, st := do(t, s, call{method: "GET", path: "/api/state"}); st["pack"] != "auto" {
		t.Fatalf("pack after undo %v", st["pack"])
	}
	if rr, m := do(t, s, call{method: "POST", path: "/api/runs/" + runID + "/undo"}); rr.Code != 409 {
		t.Fatalf("second undo %d %v", rr.Code, m)
	}
	_, brief := do(t, s, call{method: "POST", path: "/api/agents/morning-brief/run"})
	if rr, m := do(t, s, call{method: "POST", path: "/api/runs/" + brief["run_id"].(string) + "/undo"}); rr.Code != 422 {
		t.Fatalf("undo tier 0 %d %v", rr.Code, m)
	}
	if rr, _ := do(t, s, call{method: "POST", path: "/api/lock", noAIOS: true}); rr.Code != 403 {
		t.Fatalf("lock without X-AIOS %d", rr.Code)
	}
}
