// Package web holds the desktop UI's static files (everything in this
// directory except Go source), embedded into the aios binary. The desktop UI
// builder drops files here; there is no build step.
package web

import (
	"embed"
	"io/fs"
	"path"
	"strings"
)

//go:embed all:*
var files embed.FS

// FS returns the embedded UI files with Go source files hidden.
func FS() fs.FS { return hideGo{files} }

type hideGo struct{ fsys fs.FS }

func hidden(name string) bool { return strings.HasSuffix(path.Base(name), ".go") }

func (h hideGo) Open(name string) (fs.File, error) {
	if hidden(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return h.fsys.Open(name)
}

func (h hideGo) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, err := fs.ReadDir(h.fsys, name)
	if err != nil {
		return nil, err
	}
	out := entries[:0:0]
	for _, e := range entries {
		if !hidden(e.Name()) {
			out = append(out, e)
		}
	}
	return out, nil
}
