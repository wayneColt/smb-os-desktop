// Package browser opens a URL in the user's default browser.
package browser

import (
	"os/exec"
	"runtime"
)

// Command returns the command that opens url on goos.
func Command(goos, url string) (string, []string) {
	switch goos {
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		return "open", []string{url}
	default:
		return "xdg-open", []string{url}
	}
}

// Open starts the default browser on url without waiting for it.
func Open(url string) error {
	name, args := Command(runtime.GOOS, url)
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
