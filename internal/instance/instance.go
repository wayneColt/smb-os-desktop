// Package instance keeps two copies of aios from sharing one data folder.
// The running copy writes <data>/aios.lock with its port and instance id; a
// second copy that finds a live first copy (it answers /api/health with the
// same instance id) hands over to it instead of starting. A lock left by a
// copy that crashed is detected as stale and replaced.
package instance

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// FileName is the lock file's name inside the data folder.
const FileName = "aios.lock"

// Info is what the lock file holds.
type Info struct {
	PID      int    `json:"pid"`
	Port     int    `json:"port"`
	Instance string `json:"instance"`
	Started  string `json:"started"`
}

// Lock is a held data-folder lock.
type Lock struct{ path string }

// ErrRunning is returned by Acquire when a live copy already holds the lock.
type ErrRunning struct{ Port int }

func (e *ErrRunning) Error() string { return fmt.Sprintf("already running on port %d", e.Port) }

// Acquire takes the lock for dir.
func Acquire(dir string) (*Lock, error) {
	path := filepath.Join(dir, FileName)
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			f.Close()
			return &Lock{path: path}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if info, ok := read(path); ok && alive(info) {
			return nil, &ErrRunning{Port: info.Port}
		}
		// Stale, half-written or unreadable: the copy that wrote it is gone.
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	return nil, errors.New("could not take the data folder lock")
}

// Write records the running copy's details in the lock file.
func (l *Lock) Write(info Info) error {
	b, err := json.Marshal(info)
	if err != nil {
		return err
	}
	return os.WriteFile(l.path, b, 0o600)
}

// Release removes the lock file.
func (l *Lock) Release() {
	if l != nil {
		_ = os.Remove(l.path)
	}
}

func read(path string) (Info, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Info{}, false
	}
	var info Info
	if json.Unmarshal(b, &info) != nil || info.Port <= 0 || info.Instance == "" {
		// A lock created a moment ago may not have its details yet; give the
		// other copy a moment to write them.
		if fi, err := os.Stat(path); err == nil && time.Since(fi.ModTime()) < 3*time.Second {
			time.Sleep(500 * time.Millisecond)
			b, err = os.ReadFile(path)
			if err != nil || json.Unmarshal(b, &info) != nil || info.Port <= 0 {
				return Info{}, false
			}
			return info, info.Instance != ""
		}
		return Info{}, false
	}
	return info, true
}

// alive asks the recorded port whether the same instance is still serving.
func alive(info Info) bool {
	c := &http.Client{Timeout: 1500 * time.Millisecond}
	host := "127.0.0.1:" + strconv.Itoa(info.Port)
	req, err := http.NewRequest(http.MethodGet, "http://"+host+"/api/health", nil)
	if err != nil {
		return false
	}
	res, err := c.Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	var h struct {
		Name     string `json:"name"`
		Instance string `json:"instance"`
	}
	if res.StatusCode != http.StatusOK || json.NewDecoder(res.Body).Decode(&h) != nil {
		return false
	}
	return h.Name == "aios" && h.Instance == info.Instance
}
