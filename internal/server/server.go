// Package server is aios's HTTP surface: the JSON API under /api/ and the
// embedded desktop UI under /. It binds to loopback only (the caller's job)
// and enforces three guards on every request:
//
//   - Host must be 127.0.0.1:<port> or localhost:<port> (DNS-rebinding guard).
//   - On /api/, an Origin header, if present, must be the served origin.
//   - Every request that is not GET or HEAD must carry X-AIOS: 1 (CSRF guard;
//     a cross-site page cannot add the header without a CORS preflight, and
//     no CORS headers are ever sent).
package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wayneColt/smb-os-desktop/internal/engine"
)

// MaxBody is the largest request body accepted, in bytes.
const MaxBody = 1 << 20

func init() {
	// Fixed types so the UI loads the same on every OS (the Windows registry
	// can map .js to text/plain otherwise).
	for ext, typ := range map[string]string{
		".html": "text/html; charset=utf-8", ".htm": "text/html; charset=utf-8",
		".css": "text/css; charset=utf-8", ".js": "text/javascript; charset=utf-8",
		".mjs": "text/javascript; charset=utf-8", ".json": "application/json",
		".svg": "image/svg+xml", ".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
		".webp": "image/webp", ".gif": "image/gif", ".ico": "image/x-icon",
		".woff2": "font/woff2", ".woff": "font/woff", ".ttf": "font/ttf", ".otf": "font/otf",
		".txt": "text/plain; charset=utf-8", ".md": "text/markdown; charset=utf-8",
		".webmanifest": "application/manifest+json", ".map": "application/json",
	} {
		_ = mime.AddExtensionType(ext, typ)
	}
}

// Server serves the API and the UI.
type Server struct {
	eng   *engine.Engine
	web   fs.FS
	mux   *http.ServeMux
	port  int
	etags sync.Map // path -> etag
}

// New returns a server for the given engine and UI files. port is the port
// the listener is bound to; it defines the only accepted Host values.
func New(eng *engine.Engine, web fs.FS, port int) *Server {
	s := &Server{eng: eng, web: web, port: port, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("/api/health", s.only("GET", s.health))
	s.mux.HandleFunc("/api/packs", s.only("GET", s.packs))
	s.mux.HandleFunc("/api/pack", s.only("POST", s.switchPack))
	s.mux.HandleFunc("/api/state", s.only("GET", s.state))
	s.mux.HandleFunc("/api/agents", s.only("GET", s.agents))
	s.mux.HandleFunc("/api/agents/{id}/run", s.only("POST", s.run))
	s.mux.HandleFunc("/api/proposals", s.only("GET", s.proposals))
	s.mux.HandleFunc("/api/proposals/{hash}/approve", s.only("POST", s.approve))
	s.mux.HandleFunc("/api/proposals/{hash}/decline", s.only("POST", s.decline))
	s.mux.HandleFunc("/api/receipts", s.only("GET", s.receipts))
	s.mux.HandleFunc("/api/ask", s.only("POST", s.ask))
	s.mux.HandleFunc("/api/runs/{id}/undo", s.only("POST", s.undo))
	s.mux.HandleFunc("/api/capabilities", s.only("GET", s.capabilities))
	s.mux.HandleFunc("/api/lock", s.only("POST", s.lock(true)))
	s.mux.HandleFunc("/api/unlock", s.only("POST", s.lock(false)))
	s.mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusNotFound, "not_found", "no such API endpoint")
	})
	s.mux.HandleFunc("/", s.static)
}

// ServeHTTP applies the guards, then routes.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Content-Security-Policy", "frame-ancestors 'none'; base-uri 'self'; form-action 'self'; object-src 'none'; connect-src 'self'")
	if !s.hostOK(r.Host) {
		writeErr(w, http.StatusForbidden, "bad_host", "this server only answers on 127.0.0.1 or localhost")
		return
	}
	api := r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/")
	if o := r.Header.Get("Origin"); o != "" && api && o != "http://"+r.Host {
		writeErr(w, http.StatusForbidden, "bad_origin", "requests from other sites are refused")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("X-AIOS") != "1" {
		writeErr(w, http.StatusForbidden, "missing_header", "requests that change anything must carry the X-AIOS: 1 header")
		return
	}
	if api {
		h.Set("Cache-Control", "no-store")
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Server) hostOK(host string) bool {
	p := strconv.Itoa(s.port)
	return host == "127.0.0.1:"+p || strings.EqualFold(host, "localhost:"+p)
}

func (s *Server) only(method string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method && !(method == "GET" && r.Method == http.MethodHead) {
			w.Header().Set("Allow", method)
			writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "use "+method)
			return
		}
		h(w, r)
	}
}

// ---- handlers ----

func (s *Server) health(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, s.eng.Health()) }

func (s *Server) packs(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, s.eng.Packs()) }

func (s *Server) state(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, s.eng.State()) }

func (s *Server) agents(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, s.eng.Specs()) }

func (s *Server) capabilities(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.eng.Capabilities())
}

func (s *Server) lock(on bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct{}
		if !decode(w, r, &body, false) {
			return
		}
		res, err := s.eng.SetLock(on)
		if err != nil {
			writeEngineErr(w, err)
			return
		}
		writeJSON(w, 200, res)
	}
}

func (s *Server) proposals(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.eng.Proposals())
}

func (s *Server) switchPack(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if !decode(w, r, &body, true) {
		return
	}
	st, err := s.eng.SwitchPack(body.ID)
	if err != nil {
		writeEngineErr(w, err)
		return
	}
	writeJSON(w, 200, st)
}

func (s *Server) run(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Params map[string]json.RawMessage `json:"params"`
	}
	if !decode(w, r, &body, false) {
		return
	}
	res, err := s.eng.Run(r.PathValue("id"), body.Params)
	if err != nil {
		writeEngineErr(w, err)
		return
	}
	if _, ok := res.(*engine.ProposalResult); ok {
		writeJSON(w, http.StatusAccepted, res)
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) approve(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Phrase string `json:"phrase"`
	}
	if !decode(w, r, &body, false) {
		return
	}
	res, err := s.eng.Approve(r.PathValue("hash"), body.Phrase)
	if err != nil {
		writeEngineErr(w, err)
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) decline(w http.ResponseWriter, r *http.Request) {
	var body struct{}
	if !decode(w, r, &body, false) {
		return
	}
	rec, err := s.eng.Decline(r.PathValue("hash"))
	if err != nil {
		writeEngineErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"status": "declined", "receipt": rec})
}

func (s *Server) undo(w http.ResponseWriter, r *http.Request) {
	var body struct{}
	if !decode(w, r, &body, false) {
		return
	}
	res, err := s.eng.Undo(r.PathValue("id"))
	if err != nil {
		writeEngineErr(w, err)
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) receipts(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeErr(w, http.StatusUnprocessableEntity, "bad_param", "limit must be a positive integer")
			return
		}
		limit = min(n, 1000)
	}
	page, err := s.eng.Receipts(limit)
	if err != nil {
		writeEngineErr(w, err)
		return
	}
	writeJSON(w, 200, page)
}

func (s *Server) ask(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Q string `json:"q"`
	}
	if !decode(w, r, &body, true) {
		return
	}
	res, err := s.eng.Ask(r.Context(), body.Q)
	if err != nil {
		writeEngineErr(w, err)
		return
	}
	writeJSON(w, 200, res)
}

// ---- static UI ----

const fallbackPage = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>SMB OS Desktop</title>
<style>body{font:16px/1.5 system-ui,sans-serif;margin:0;padding:48px 24px;background:#f6f6f4;color:#1b1b1b}
main{max-width:560px;margin:0 auto}code{font-family:ui-monospace,monospace}
@media (prefers-color-scheme:dark){body{background:#151515;color:#eee}}</style></head>
<body><main><h1>SMB OS Desktop</h1>
<p>The runtime is running, but this build does not include the desktop interface.</p>
<p>The data API answers at <code>/api/health</code>.</p></main></body></html>
`

func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET")
		return
	}
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" || strings.HasSuffix(r.URL.Path, "/") {
		name = path.Join(name, "index.html")
	}
	f, err := s.web.Open(name)
	if err != nil {
		if name == "index.html" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			_, _ = io.WriteString(w, fallbackPage)
			return
		}
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if fi.IsDir() {
		if idx, err := s.web.Open(path.Join(name, "index.html")); err == nil {
			idx.Close()
			http.Redirect(w, r, "/"+name+"/", http.StatusMovedPermanently)
			return
		}
		http.NotFound(w, r)
		return
	}
	data, err := io.ReadAll(f)
	if err != nil {
		http.Error(w, "read error", http.StatusInternalServerError)
		return
	}
	etag, ok := s.etags.Load(name)
	if !ok {
		sum := sha256.Sum256(data)
		etag = `"` + hex.EncodeToString(sum[:8]) + `"`
		s.etags.Store(name, etag)
	}
	w.Header().Set("ETag", etag.(string))
	w.Header().Set("Cache-Control", "no-cache")
	if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}

// ---- helpers ----

// decode reads a JSON body into v. An empty body is allowed unless required.
func decode(w http.ResponseWriter, r *http.Request, v any, required bool) bool {
	r.Body = http.MaxBytesReader(w, r.Body, MaxBody)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeErr(w, http.StatusRequestEntityTooLarge, "too_large", "request body is too large")
		} else {
			writeErr(w, http.StatusBadRequest, "bad_body", "could not read the request body")
		}
		return false
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		if required {
			writeErr(w, http.StatusBadRequest, "bad_body", "a JSON body is required")
			return false
		}
		return true
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", "request body is not valid JSON for this endpoint: "+err.Error())
		return false
	}
	if dec.More() {
		writeErr(w, http.StatusBadRequest, "bad_json", "request body has trailing data")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		log.Printf("aios: encode response: %v", err)
		writeErr(w, http.StatusInternalServerError, "internal", "could not encode the response")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(b, '\n'))
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	b, _ := json.Marshal(map[string]string{"error": msg, "code": code})
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(b, '\n'))
}

func writeEngineErr(w http.ResponseWriter, err error) {
	var ee *engine.Error
	if errors.As(err, &ee) {
		if ee.Status >= 500 {
			log.Printf("aios: %s: %s", ee.Code, ee.Msg)
		}
		writeErr(w, ee.Status, ee.Code, ee.Msg)
		return
	}
	log.Printf("aios: internal error: %v", err)
	writeErr(w, http.StatusInternalServerError, "internal", "internal error: "+err.Error())
}
