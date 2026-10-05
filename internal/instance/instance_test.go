package instance

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestAcquireReleaseReacquire(t *testing.T) {
	dir := t.TempDir()
	l, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	l.Release()
	l2, err := Acquire(dir)
	if err != nil {
		t.Fatalf("after release: %v", err)
	}
	l2.Release()
}

func TestStaleLockIsReplaced(t *testing.T) {
	dir := t.TempDir()
	// A lock from a copy that crashed: nothing answers on its port.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	stale := &Lock{path: filepath.Join(dir, FileName)}
	if err := stale.Write(Info{PID: 1, Port: port, Instance: "gone"}); err != nil {
		t.Fatal(err)
	}
	l, err := Acquire(dir)
	if err != nil {
		t.Fatalf("stale lock not replaced: %v", err)
	}
	l.Release()
}

func TestLiveCopyIsDetected(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"name":"aios","instance":"abc123"}`))
	}))
	defer srv.Close()
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	held := &Lock{path: filepath.Join(dir, FileName)}
	_ = held.Write(Info{PID: os.Getpid(), Port: port, Instance: "abc123"})
	_, err := Acquire(dir)
	var running *ErrRunning
	if !errors.As(err, &running) || running.Port != port {
		t.Fatalf("live copy not detected: %v", err)
	}
	// A different aios on that port (another data folder) is not this one.
	_ = held.Write(Info{PID: os.Getpid(), Port: port, Instance: "someone-else"})
	l, err := Acquire(dir)
	if err != nil {
		t.Fatalf("an unrelated aios must not block this folder: %v", err)
	}
	l.Release()
}
