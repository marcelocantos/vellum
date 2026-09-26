// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package viewer

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const vnodeNotes = unix.NOTE_DELETE | unix.NOTE_WRITE | unix.NOTE_EXTEND | unix.NOTE_ATTRIB | unix.NOTE_LINK | unix.NOTE_RENAME | unix.NOTE_REVOKE

// kqWatch watches one path. The file vnode is watched while the file exists.
// While the path is missing, the nearest existing ancestor directory is watched
// so a later create is visible. The watch does not follow a rename.
type kqWatch struct {
	path string
	stop <-chan struct{}

	kqOnce sync.Once
	kq     int
	fd     int
	file   bool
	dir    string
}

func openPathSource(path string, stop <-chan struct{}) (pathSource, error) {
	kq, err := unix.Kqueue()
	if err != nil {
		return nil, err
	}
	w := &kqWatch{path: path, stop: stop, kq: kq, fd: -1}
	select {
	case <-stop:
		w.close()
		return nil, errWatchStopped
	default:
	}
	if err := w.armInitial(); err != nil {
		w.close()
		return nil, err
	}
	return w, nil
}

func (w *kqWatch) close() {
	w.interrupt()
	w.closeFD()
}

func (w *kqWatch) interrupt() {
	w.kqOnce.Do(func() {
		if w.kq >= 0 {
			_ = unix.Close(w.kq)
		}
	})
}

func (w *kqWatch) watchingFile() bool { return w.file }

func (w *kqWatch) stamp() (string, bool) {
	if !w.file || w.fd < 0 {
		return "", false
	}
	var st unix.Stat_t
	if err := unix.Fstat(w.fd, &st); err != nil || st.Nlink == 0 {
		return "", false
	}
	return strconv.FormatInt(st.Mtim.Nano(), 10) + " " + strconv.FormatInt(st.Size, 10), true
}

func (w *kqWatch) reconcile() (pathNote, error) {
	if !w.file {
		return pathNote{}, nil
	}
	note, _, err := w.onFileNotes(unix.NOTE_DELETE)
	return note, err
}

func (w *kqWatch) wait(deadline time.Time) (pathNote, bool, error) {
	var timeout *unix.Timespec
	if !deadline.IsZero() {
		d := time.Until(deadline)
		if d < 0 {
			d = 0
		}
		ts := unix.NsecToTimespec(d.Nanoseconds())
		timeout = &ts
	}
	events := make([]unix.Kevent_t, 4)
	for {
		select {
		case <-w.stop:
			return pathNote{}, false, errWatchStopped
		default:
		}
		n, err := unix.Kevent(w.kq, nil, events, timeout)
		if err != nil {
			if err == unix.EINTR {
				if !deadline.IsZero() && !time.Now().Before(deadline) {
					return pathNote{}, true, nil
				}
				if !deadline.IsZero() {
					d := time.Until(deadline)
					if d < 0 {
						d = 0
					}
					ts := unix.NsecToTimespec(d.Nanoseconds())
					timeout = &ts
				}
				continue
			}
			if err == unix.EBADF {
				return pathNote{}, false, errWatchStopped
			}
			return pathNote{}, false, err
		}
		if n == 0 {
			return pathNote{}, true, nil
		}
		note, ok, err := w.interpret(events[:n])
		if err != nil {
			return pathNote{}, false, err
		}
		if ok {
			return note, false, nil
		}
		if !deadline.IsZero() && !time.Now().Before(deadline) {
			return pathNote{}, true, nil
		}
		if !deadline.IsZero() {
			d := time.Until(deadline)
			if d < 0 {
				d = 0
			}
			ts := unix.NsecToTimespec(d.Nanoseconds())
			timeout = &ts
		}
	}
}

func (w *kqWatch) interpret(events []unix.Kevent_t) (pathNote, bool, error) {
	var fflags uint32
	matched := false
	for _, ev := range events {
		if ev.Filter == unix.EVFILT_VNODE && ev.Ident == uint64(w.fd) {
			fflags |= ev.Fflags
			matched = true
		}
	}
	if !matched {
		return pathNote{}, false, nil
	}
	if w.file {
		return w.onFileNotes(fflags)
	}
	return w.onDirNotes()
}

func (w *kqWatch) onFileNotes(fflags uint32) (pathNote, bool, error) {
	identity := fflags&(unix.NOTE_DELETE|unix.NOTE_RENAME|unix.NOTE_REVOKE) != 0
	content := fflags&(unix.NOTE_WRITE|unix.NOTE_EXTEND|unix.NOTE_ATTRIB|unix.NOTE_LINK) != 0
	if !identity {
		if content {
			return pathNote{content: true}, true, nil
		}
		return pathNote{}, false, nil
	}

	var pst unix.Stat_t
	err := unix.Lstat(w.path, &pst)
	pathOK := err == nil && pst.Mode&unix.S_IFMT != unix.S_IFDIR
	var fst unix.Stat_t
	fdOK := w.fd >= 0 && unix.Fstat(w.fd, &fst) == nil
	same := pathOK && fdOK && pst.Ino == fst.Ino && fst.Nlink > 0
	if pathOK && !same {
		if err := w.armFile(); err != nil {
			return pathNote{}, false, err
		}
		return pathNote{replaced: true}, true, nil
	}
	if !pathOK {
		renamed := ""
		if fflags&unix.NOTE_RENAME != 0 && w.fd >= 0 {
			if p, err := fdPath(w.fd); err == nil {
				p = filepath.Clean(p)
				if p != w.path && p != "/" {
					renamed = p
				}
			}
		}
		if err := w.armAbsent(); err != nil {
			return pathNote{}, false, err
		}
		if renamed != "" {
			return pathNote{renamed: renamed}, true, nil
		}
		return pathNote{unlinked: true}, true, nil
	}
	if content {
		return pathNote{content: true}, true, nil
	}
	return pathNote{}, false, nil
}

func (w *kqWatch) onDirNotes() (pathNote, bool, error) {
	// A create can land in the gap between noticing a new directory and
	// arming it. Walk until the watch matches the path as it is now.
	for {
		if info, err := os.Stat(w.path); err == nil && !info.IsDir() {
			if err := w.armFile(); err != nil {
				return pathNote{}, false, err
			}
			return pathNote{appeared: true}, true, nil
		}
		dir, err := deepestExistingDir(w.path)
		if err != nil {
			return pathNote{}, false, err
		}
		if dir == w.dir && w.fd >= 0 && !w.file {
			return pathNote{}, false, nil
		}
		if err := w.armDir(dir); err != nil {
			return pathNote{}, false, err
		}
	}
}

func (w *kqWatch) armInitial() error {
	info, err := os.Stat(w.path)
	if err == nil && !info.IsDir() {
		return w.armFile()
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return w.armAbsent()
}

func (w *kqWatch) armAbsent() error {
	dir, err := deepestExistingDir(w.path)
	if err != nil {
		return err
	}
	if w.fd >= 0 && !w.file && w.dir == dir {
		return nil
	}
	return w.armDir(dir)
}

func (w *kqWatch) armFile() error {
	fd, err := unix.Open(w.path, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	return w.swapTo(fd, true, "")
}

func (w *kqWatch) armDir(dir string) error {
	fd, err := unix.Open(dir, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	return w.swapTo(fd, false, dir)
}

func (w *kqWatch) swapTo(fd int, file bool, dir string) error {
	if err := w.register(fd); err != nil {
		_ = unix.Close(fd)
		return err
	}
	w.dropFD()
	w.fd = fd
	w.file = file
	w.dir = dir
	return nil
}

func (w *kqWatch) register(fd int) error {
	ev := unix.Kevent_t{
		Ident:  uint64(fd),
		Filter: unix.EVFILT_VNODE,
		Flags:  unix.EV_ADD | unix.EV_CLEAR | unix.EV_ENABLE,
		Fflags: vnodeNotes,
	}
	_, err := unix.Kevent(w.kq, []unix.Kevent_t{ev}, nil, nil)
	return err
}

func (w *kqWatch) closeFD() {
	w.dropFD()
}

func (w *kqWatch) dropFD() {
	if w.fd < 0 {
		return
	}
	ev := unix.Kevent_t{
		Ident:  uint64(w.fd),
		Filter: unix.EVFILT_VNODE,
		Flags:  unix.EV_DELETE,
	}
	_, _ = unix.Kevent(w.kq, []unix.Kevent_t{ev}, nil, nil)
	_ = unix.Close(w.fd)
	w.fd = -1
	w.file = false
	w.dir = ""
}

func deepestExistingDir(path string) (string, error) {
	dir := filepath.Dir(path)
	for {
		info, err := os.Stat(dir)
		if err == nil && info.IsDir() {
			return dir, nil
		}
		if dir == "/" || dir == "." {
			if err != nil {
				return "", err
			}
			return dir, nil
		}
		next := filepath.Dir(dir)
		if next == dir {
			if err != nil {
				return "", err
			}
			return dir, nil
		}
		dir = next
	}
}

func fdPath(fd int) (string, error) {
	buf := make([]byte, 1024)
	_, _, errno := unix.Syscall(unix.SYS_FCNTL, uintptr(fd), uintptr(unix.F_GETPATH), uintptr(unsafe.Pointer(&buf[0])))
	if errno != 0 {
		return "", errno
	}
	if i := bytes.IndexByte(buf, 0); i >= 0 {
		buf = buf[:i]
	}
	return string(buf), nil
}
