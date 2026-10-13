// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build !darwin

package viewer

import (
	"os"
	"path/filepath"
	"time"
)

// pollWatch is the watcher off macOS (macOS watches with kqueue). It
// stats on a timer; when the file vanishes it looks for the same file
// (by identity) elsewhere in the directory so renames redirect the view
// rather than reading as deletes.
type pollWatch struct {
	path      string
	stop      <-chan struct{}
	present   bool
	lastStamp string
	last      os.FileInfo
}

// statPath stats w.path and pins the file identity, so a later
// os.SameFile still works after the path is gone (on Windows the
// identity is otherwise read lazily from the path).
func (w *pollWatch) statPath() (os.FileInfo, error) {
	info, err := os.Stat(w.path)
	if err == nil {
		os.SameFile(info, info)
	}
	return info, err
}

// vanished records the loss of w.path and reports a rename when the
// file now lives under another name in the same directory.
func (w *pollWatch) vanished() pathNote {
	last := w.last
	w.present = false
	w.lastStamp = ""
	w.last = nil
	if last != nil {
		dir := filepath.Dir(w.path)
		if entries, err := os.ReadDir(dir); err == nil {
			for _, e := range entries {
				cand := filepath.Join(dir, e.Name())
				if cand == w.path || e.IsDir() {
					continue
				}
				if info, err := os.Stat(cand); err == nil && os.SameFile(last, info) {
					return pathNote{renamed: cand}
				}
			}
		}
	}
	return pathNote{unlinked: true}
}

func openPathSource(path string, stop <-chan struct{}) (pathSource, error) {
	w := &pollWatch{path: path, stop: stop}
	w.observe()
	return w, nil
}

func (w *pollWatch) close()             {}
func (w *pollWatch) interrupt()         {}
func (w *pollWatch) watchingFile() bool { return w.present }

func (w *pollWatch) stamp() (string, bool) {
	info, err := os.Stat(w.path)
	if err != nil || info.IsDir() {
		return "", false
	}
	return sourceStamp(info), true
}

func (w *pollWatch) reconcile() (pathNote, error) {
	if _, ok := w.stamp(); ok {
		return pathNote{}, nil
	}
	if w.present {
		return w.vanished(), nil
	}
	return pathNote{}, nil
}

func (w *pollWatch) wait(deadline time.Time) (pathNote, bool, error) {
	d := 400 * time.Millisecond
	timed := false
	if !deadline.IsZero() {
		d = time.Until(deadline)
		if d < 0 {
			d = 0
		}
		timed = true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-w.stop:
		return pathNote{}, false, errWatchStopped
	case <-timer.C:
	}
	note, changed := w.observe()
	if changed {
		return note, false, nil
	}
	if timed {
		return pathNote{}, true, nil
	}
	return pathNote{}, false, nil
}

func (w *pollWatch) observe() (pathNote, bool) {
	info, err := w.statPath()
	if err != nil || info.IsDir() {
		if w.present {
			return w.vanished(), true
		}
		return pathNote{}, false
	}
	w.last = info
	st := sourceStamp(info)
	if !w.present {
		w.present = true
		w.lastStamp = st
		return pathNote{appeared: true}, true
	}
	if st != w.lastStamp {
		w.lastStamp = st
		return pathNote{content: true}, true
	}
	return pathNote{}, false
}
