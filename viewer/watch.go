// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	// DefaultWatchHeartbeat is the WebSocket protocol ping interval.
	DefaultWatchHeartbeat = 5 * time.Minute
	// watchQuiescence is how long a path must stay quiet after the latest
	// content event before a stamp is published. Each new content event
	// restarts the wait. Rename and unlink are not content and are published
	// immediately.
	watchQuiescence = 50 * time.Millisecond
)

// errWatchStopped is returned when the last subscriber has left.
var errWatchStopped = errors.New("watch stopped")

func isMarkdownPath(p string) bool {
	ext := strings.ToLower(filepath.Ext(p))
	return ext == ".md" || ext == ".markdown"
}

func sourceStamp(info os.FileInfo) string {
	return strconv.FormatInt(info.ModTime().UnixNano(), 10) + " " + strconv.FormatInt(info.Size(), 10)
}

func (s *Server) watchHeartbeat() time.Duration {
	if s != nil && s.WatchHeartbeat > 0 {
		return s.WatchHeartbeat
	}
	return DefaultWatchHeartbeat
}

func (s *Server) watchHub() *watchHub {
	s.watchOnce.Do(func() {
		s.watches = &watchHub{paths: make(map[string]*pathMachine)}
	})
	return s.watches
}

func (s *Server) handleWatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	absPath, err := queryAbsPath(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !isMarkdownPath(absPath) {
		http.Error(w, "not a markdown path", http.StatusBadRequest)
		return
	}
	info, err := os.Stat(absPath)
	if err != nil && !os.IsNotExist(err) {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err == nil && info.IsDir() {
		http.Error(w, "is a directory", http.StatusBadRequest)
		return
	}

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()

	ctx := r.Context()
	readCtx, cancelRead := context.WithCancel(ctx)
	defer cancelRead()
	go func() {
		defer cancelRead()
		for {
			if _, _, err := conn.Read(readCtx); err != nil {
				return
			}
		}
	}()

	sub := s.watchHub().join(absPath)
	defer s.watchHub().leave(absPath, sub)

	pingTick := time.NewTicker(s.watchHeartbeat())
	defer pingTick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-readCtx.Done():
			return
		case <-sub.wake:
			for _, msg := range sub.drain() {
				if err := writeWatchText(ctx, conn, msg); err != nil {
					return
				}
			}
		case <-pingTick.C:
			pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := conn.Ping(pingCtx)
			cancel()
			if err != nil {
				return
			}
			s.WatchPingCount.Add(1)
		}
	}
}

func writeWatchText(ctx context.Context, conn *websocket.Conn, msg string) error {
	return conn.Write(ctx, websocket.MessageText, []byte(msg))
}

func requireMarkdownFile(absPath string) (os.FileInfo, error) {
	if !isMarkdownPath(absPath) {
		return nil, fmt.Errorf("not a markdown path")
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, errIsDirectory
	}
	return info, nil
}

// A path is Absent (missing; the nearest existing directory is watched),
// Settling (a content change is waiting out watchQuiescence), or Present
// (a stamp has been published and the file vnode is watched).
// Rename-away and unlink both end in Absent. The event, not the destination
// state, is what current subscribers are told.
type watchState int

const (
	watchAbsent watchState = iota
	watchSettling
	watchPresent
)

// pathNote is one observation from the platform watcher.
// An empty note means the watch woke with nothing the machine should apply.
type pathNote struct {
	content  bool
	appeared bool
	replaced bool
	unlinked bool
	renamed  string
}

// pathSource is the platform file watch for one path.
type pathSource interface {
	close()
	interrupt()
	watchingFile() bool
	stamp() (string, bool)
	// reconcile rearms after stamp() fails and reports the identity change.
	reconcile() (pathNote, error)
	// wait blocks until a note, until deadline when non-zero, or until stop.
	// timedOut is set when deadline elapsed with no note.
	wait(deadline time.Time) (note pathNote, timedOut bool, err error)
}

type watchHub struct {
	mu    sync.Mutex
	paths map[string]*pathMachine
}

type pathMachine struct {
	path string
	hub  *watchHub

	mu        sync.Mutex
	subs      map[*watchSub]struct{}
	published string
	src       pathSource
	stop      chan struct{}
}

type watchSub struct {
	mu   sync.Mutex
	q    []string
	wake chan struct{}
}

func newWatchSub() *watchSub {
	return &watchSub{wake: make(chan struct{}, 1)}
}

func (s *watchSub) push(msg string) {
	s.mu.Lock()
	s.q = append(s.q, msg)
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *watchSub) drain() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.q) == 0 {
		return nil
	}
	out := s.q
	s.q = nil
	return out
}

func (h *watchHub) join(path string) *watchSub {
	h.mu.Lock()
	defer h.mu.Unlock()
	m := h.paths[path]
	start := false
	if m == nil {
		m = &pathMachine{
			path: path,
			hub:  h,
			subs: make(map[*watchSub]struct{}),
			stop: make(chan struct{}),
		}
		h.paths[path] = m
		start = true
	}
	sub := newWatchSub()
	m.mu.Lock()
	m.subs[sub] = struct{}{}
	snap := m.published
	m.mu.Unlock()
	if start {
		go m.run()
	}
	if snap != "" {
		sub.push(snap)
	}
	return sub
}

func (h *watchHub) leave(path string, sub *watchSub) {
	h.mu.Lock()
	defer h.mu.Unlock()
	m := h.paths[path]
	if m == nil {
		return
	}
	m.mu.Lock()
	delete(m.subs, sub)
	empty := len(m.subs) == 0
	m.mu.Unlock()
	if empty {
		delete(h.paths, path)
		m.halt()
	}
}

func (m *pathMachine) halt() {
	m.mu.Lock()
	select {
	case <-m.stop:
		m.mu.Unlock()
		return
	default:
		close(m.stop)
	}
	src := m.src
	m.mu.Unlock()
	if src != nil {
		src.interrupt()
	}
}

func (m *pathMachine) setSrc(src pathSource) {
	m.mu.Lock()
	m.src = src
	m.mu.Unlock()
}

func (m *pathMachine) stopped() bool {
	select {
	case <-m.stop:
		return true
	default:
		return false
	}
}

func (m *pathMachine) sleep(d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-m.stop:
		return false
	case <-timer.C:
		return true
	}
}

func (m *pathMachine) publish(msg string) {
	m.mu.Lock()
	switch {
	case strings.HasPrefix(msg, "stamp "):
		m.published = msg
	case msg == "delete" || strings.HasPrefix(msg, "rename "):
		m.published = ""
	}
	subs := make([]*watchSub, 0, len(m.subs))
	for sub := range m.subs {
		subs = append(subs, sub)
	}
	m.mu.Unlock()
	for _, sub := range subs {
		sub.push(msg)
	}
}

func (m *pathMachine) run() {
	state := watchAbsent
	var deadline time.Time
	published := ""
	reported := ""

	for {
		if m.stopped() {
			return
		}
		src, err := openPathSource(m.path, m.stop)
		if err != nil || m.stopped() {
			if src != nil {
				src.close()
			}
			if errors.Is(err, errWatchStopped) || m.stopped() {
				return
			}
			if err != nil && err.Error() != reported {
				reported = err.Error()
				m.publish("error " + reported)
			}
			if !m.sleep(400 * time.Millisecond) {
				return
			}
			continue
		}
		m.setSrc(src)

		if src.watchingFile() {
			if st, ok := src.stamp(); ok {
				msg := "stamp " + st
				if msg != published {
					published = msg
					m.publish(published)
				}
				state = watchPresent
			}
		}

		err = m.loop(src, &state, &deadline, &published)
		m.setSrc(nil)
		src.close()
		if errors.Is(err, errWatchStopped) || m.stopped() {
			return
		}
		if err != nil && err.Error() != reported {
			reported = err.Error()
			m.publish("error " + reported)
		}
		state = watchAbsent
		deadline = time.Time{}
		if !m.sleep(400 * time.Millisecond) {
			return
		}
	}
}

func (m *pathMachine) loop(src pathSource, state *watchState, deadline *time.Time, published *string) error {
	for {
		if m.stopped() {
			return errWatchStopped
		}
		var until time.Time
		if *state == watchSettling {
			until = *deadline
		}
		note, timedOut, err := src.wait(until)
		if err != nil {
			return err
		}
		if timedOut {
			if *state != watchSettling {
				continue
			}
			if st, ok := src.stamp(); ok {
				msg := "stamp " + st
				if msg != *published {
					*published = msg
					m.publish(msg)
				}
				*state = watchPresent
				continue
			}
			note, err = src.reconcile()
			if err != nil {
				return err
			}
			if note == (pathNote{}) {
				*deadline = time.Now().Add(watchQuiescence)
				continue
			}
		}
		if note == (pathNote{}) {
			continue
		}
		switch {
		case note.renamed != "":
			m.publish("rename " + pathURL(note.renamed))
			*published = ""
			*state = watchAbsent
			*deadline = time.Time{}
		case note.unlinked:
			if *state != watchAbsent {
				m.publish("delete")
			}
			*published = ""
			*state = watchAbsent
			*deadline = time.Time{}
		case note.appeared || note.replaced || note.content:
			if note.content && !note.appeared && !note.replaced && *state == watchPresent {
				if st, ok := src.stamp(); ok && "stamp "+st == *published {
					continue
				}
			}
			*deadline = time.Now().Add(watchQuiescence)
			*state = watchSettling
		}
	}
}
