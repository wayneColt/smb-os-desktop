// Command aios is the SMB OS Desktop runtime: one file that, when started,
// serves the operator desktop on 127.0.0.1, keeps the business data on this
// computer, and runs the operator's agents behind a tier gate.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/wayneColt/smb-os-desktop/internal/browser"
	"github.com/wayneColt/smb-os-desktop/internal/engine"
	"github.com/wayneColt/smb-os-desktop/internal/instance"
	"github.com/wayneColt/smb-os-desktop/internal/llm"
	"github.com/wayneColt/smb-os-desktop/internal/server"
	"github.com/wayneColt/smb-os-desktop/web"
)

// version is set at build time: -ldflags "-X main.version=0.1.0".
var version = "0.1.0-dev"

// portTries is how many ports from --port upward are tried.
const portTries = 50

type config struct {
	port    int
	dataDir string
	pack    string
	packSet bool
	noOpen  bool
	version bool
}

func main() {
	cfg, err := parseFlags(os.Args[1:], os.Stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		os.Exit(2)
	}
	if cfg.version {
		fmt.Printf("aios %s\n", version)
		return
	}
	if err := run(cfg, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "SMB OS Desktop could not start: %v\n", err)
		pauseIfDoubleClicked()
		os.Exit(1)
	}
}

func parseFlags(args []string, errOut io.Writer) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("aios", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.IntVar(&cfg.port, "port", 7701, "port to serve on (if busy, the next free one is used)")
	fs.StringVar(&cfg.dataDir, "data", "", "folder for your data (default: your user config folder/smb-os-desktop)")
	fs.StringVar(&cfg.pack, "pack", "auto", "demo business to show: auto or homecare")
	fs.BoolVar(&cfg.noOpen, "no-open", false, "don't open the browser")
	fs.BoolVar(&cfg.version, "version", false, "print the version and exit")
	fs.Usage = func() {
		fmt.Fprintf(errOut, "SMB OS Desktop %s\n\nUsage: aios [flags]\n\n", version)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(errOut, "aios: unexpected argument %q\n", fs.Arg(0))
		fs.Usage()
		return cfg, errors.New("unexpected argument")
	}
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "pack" {
			cfg.packSet = true
		}
	})
	if cfg.port < 0 || cfg.port > 65535 {
		fmt.Fprintln(errOut, "aios: --port must be between 0 and 65535")
		return cfg, errors.New("bad port")
	}
	return cfg, nil
}

func run(cfg config, out io.Writer) error {
	dir, err := dataDir(cfg.dataDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("data folder %s: %w", dir, err)
	}
	lock, err := instance.Acquire(dir)
	if err != nil {
		var running *instance.ErrRunning
		if errors.As(err, &running) {
			url := fmt.Sprintf("http://127.0.0.1:%d/", running.Port)
			fmt.Fprintf(out, "SMB OS Desktop is already running at %s\n", url)
			if !cfg.noOpen {
				_ = browser.Open(url)
			}
			return nil
		}
		return fmt.Errorf("data folder lock: %w", err)
	}
	defer lock.Release()

	eng, err := engine.Open(engine.Options{
		DataDir: dir, Pack: cfg.pack, ForcePack: cfg.packSet, Version: version,
		LLM: llm.FromEnv(os.Getenv),
	})
	if err != nil {
		return err
	}
	defer eng.Close()

	ln, port, err := listen(cfg.port)
	if err != nil {
		return err
	}
	_ = lock.Write(instance.Info{PID: os.Getpid(), Port: port, Instance: eng.Instance(),
		Started: time.Now().UTC().Format(time.RFC3339)})

	srv := &http.Server{
		Handler:           server.New(eng, web.FS(), port),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      90 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	url := fmt.Sprintf("http://127.0.0.1:%d/", port)
	fmt.Fprintf(out, "SMB OS Desktop running at %s (Ctrl+C to stop)\n", url)
	if !cfg.noOpen {
		if err := browser.Open(url); err != nil {
			fmt.Fprintf(out, "Could not open a browser (%v). Open %s yourself.\n", err, url)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case <-ctx.Done():
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("server: %w", err)
		}
	}
	stop() // a second Ctrl+C now ends the process immediately
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	fmt.Fprintln(out, "SMB OS Desktop stopped.")
	return nil
}

// listen binds 127.0.0.1 on the first free port from want upward.
func listen(want int) (net.Listener, int, error) {
	if want == 0 {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, 0, err
		}
		return ln, ln.Addr().(*net.TCPAddr).Port, nil
	}
	var lastErr error
	for p := want; p < want+portTries && p <= 65535; p++ {
		ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(p))
		if err == nil {
			return ln, p, nil
		}
		lastErr = err
	}
	return nil, 0, fmt.Errorf("no free port between %d and %d: %v", want, want+portTries-1, lastErr)
}

func dataDir(flagValue string) (string, error) {
	if flagValue != "" {
		return filepath.Abs(flagValue)
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot find your user config folder (%v); start with --data <folder>", err)
	}
	return filepath.Join(base, "smb-os-desktop"), nil
}

// pauseIfDoubleClicked keeps a Windows console window open long enough to
// read a start-up error when aios was started by double-clicking.
func pauseIfDoubleClicked() {
	if runtime.GOOS != "windows" {
		return
	}
	if fi, err := os.Stdin.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		fmt.Fprint(os.Stderr, "Press Enter to close this window.")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	}
}
