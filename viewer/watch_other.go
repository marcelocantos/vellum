// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build !darwin

package viewer

import (
	"os"
	"time"
)

// pollWatch keeps the viewer package building off macOS. The product watches
// with kqueue; this loop only stats.
type pollWatch struct {
	path      string
	stop      <-chan struct{}
	present   bool
	lastStamp string
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
		w.present = false
		w.lastStamp = ""
		return pathNote{unlinked: true}, nil
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
	info, err := os.Stat(w.path)
	if err != nil || info.IsDir() {
		if w.present {
			w.present = false
			w.lastStamp = ""
			return pathNote{unlinked: true}, true
		}
		return pathNote{}, false
	}
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
