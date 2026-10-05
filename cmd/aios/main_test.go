package main

import (
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wayneColt/smb-os-desktop/internal/browser"
)

func TestListenFallsBackToTheNextFreePort(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	want := busy.Addr().(*net.TCPAddr).Port
	ln, got, err := listen(want)
	if err != nil {
		t.Skipf("no free port above %d: %v", want, err)
	}
	defer ln.Close()
	if got <= want || got >= want+portTries {
		t.Fatalf("got port %d, busy port was %d", got, want)
	}
	if host, _, _ := net.SplitHostPort(ln.Addr().String()); host != "127.0.0.1" {
		t.Fatalf("bound to %s, want loopback only", host)
	}
}

func TestFlags(t *testing.T) {
	cfg, err := parseFlags([]string{"--port", "8000", "--pack", "homecare", "--no-open", "--data", "x"}, io.Discard)
	if err != nil || cfg.port != 8000 || cfg.pack != "homecare" || !cfg.packSet || !cfg.noOpen || cfg.dataDir != "x" {
		t.Fatalf("%+v %v", cfg, err)
	}
	cfg, _ = parseFlags(nil, io.Discard)
	if cfg.port != 7701 || cfg.pack != "auto" || cfg.packSet || cfg.noOpen {
		t.Fatalf("defaults %+v", cfg)
	}
	for _, bad := range [][]string{{"--port", "70000"}, {"extra"}, {"--nope"}} {
		if _, err := parseFlags(bad, io.Discard); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}

func TestDataDir(t *testing.T) {
	got, err := dataDir("")
	if err != nil {
		t.Skip("no user config dir here")
	}
	if filepath.Base(got) != "smb-os-desktop" {
		t.Fatalf("default data dir %s", got)
	}
	abs, _ := dataDir("rel")
	if !filepath.IsAbs(abs) {
		t.Fatalf("%s not absolute", abs)
	}
}

func TestBrowserCommands(t *testing.T) {
	u := "http://127.0.0.1:7701/"
	for goos, want := range map[string]string{
		"linux":   "xdg-open " + u,
		"darwin":  "open " + u,
		"windows": "rundll32 url.dll,FileProtocolHandler " + u,
	} {
		name, args := browser.Command(goos, u)
		if got := name + " " + strings.Join(args, " "); got != want {
			t.Errorf("%s: %q", goos, got)
		}
	}
}
