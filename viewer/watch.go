// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
)

const (
	// DefaultWatchInterval is how often a watch connection stats the source.
	DefaultWatchInterval = 400 * time.Millisecond
	// DefaultWatchHeartbeat is the WebSocket protocol ping interval.
	DefaultWatchHeartbeat = 5 * time.Minute
)

func isMarkdownPath(p string) bool {
	ext := strings.ToLower(filepath.Ext(p))
	return ext == ".md" || ext == ".markdown"
}

func sourceStamp(info os.FileInfo) string {
	return strconv.FormatInt(info.ModTime().UnixNano(), 10) + " " + strconv.FormatInt(info.Size(), 10)
}

func (s *Server) watchInterval() time.Duration {
	if s != nil && s.WatchInterval > 0 {
		return s.WatchInterval
	}
	return DefaultWatchInterval
}

func (s *Server) watchHeartbeat() time.Duration {
	if s != nil && s.WatchHeartbeat > 0 {
		return s.WatchHeartbeat
	}
	return DefaultWatchHeartbeat
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
	if err != nil {
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if info.IsDir() {
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

	if err := writeWatchText(ctx, conn, "stamp "+sourceStamp(info)); err != nil {
		return
	}
	last := sourceStamp(info)
	errored := false

	statTick := time.NewTicker(s.watchInterval())
	defer statTick.Stop()
	pingTick := time.NewTicker(s.watchHeartbeat())
	defer pingTick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-readCtx.Done():
			return
		case <-statTick.C:
			info, err := os.Stat(absPath)
			if err != nil {
				if !errored {
					_ = writeWatchText(ctx, conn, "error "+err.Error())
					errored = true
				}
				continue
			}
			if info.IsDir() {
				if !errored {
					_ = writeWatchText(ctx, conn, "error is a directory")
					errored = true
				}
				continue
			}
			errored = false
			stamp := sourceStamp(info)
			if stamp != last {
				last = stamp
				if err := writeWatchText(ctx, conn, "stamp "+stamp); err != nil {
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
