package fsx

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const lockRetryInterval = 50 * time.Millisecond

var ErrLocked = errors.New("another promptstor run holds the lock")

type heldBy int

func (pid heldBy) Error() string {
	return fmt.Sprintf("%v (pid %d)", ErrLocked, int(pid))
}

func (heldBy) Unwrap() error { return ErrLocked }

var ErrNotHeld = errors.New("the run lock is not held")

type Held struct {
	release  func()
	released atomic.Bool
}

func Hold(path string, wait time.Duration) (*Held, error) {
	release, err := Acquire(path, wait)
	if err != nil {
		return nil, err
	}
	return &Held{release: release}, nil
}

func (h *Held) Release() {
	if h == nil {
		return
	}
	h.released.Store(true)
	h.release()
}

func (h *Held) Check() error {
	if h == nil || h.released.Load() {
		return ErrNotHeld
	}
	return nil
}

func Acquire(path string, wait time.Duration) (release func(), err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	deadline := time.Now().Add(wait)
	for {
		err := tryLock(path)
		if err == nil {
			return sync.OnceFunc(func() { unlock(path) }), nil
		}
		if !errors.Is(err, ErrLocked) || !time.Now().Before(deadline) {
			return nil, err
		}
		time.Sleep(lockRetryInterval)
	}
}

func tryLock(path string) error {
	err := createLock(path)
	if !errors.Is(err, fs.ErrExist) {
		return err
	}
	if err := removeStaleLock(path); err != nil {
		return err
	}
	err = createLock(path)
	if !errors.Is(err, fs.ErrExist) {
		return err
	}
	pid, err := lockOwner(path)
	if err != nil {
		return err
	}
	return heldBy(pid)
}

func createLock(path string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("lock %s: %w", path, err)
	}
	_, writeErr := file.WriteString(strconv.Itoa(os.Getpid()))
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return fmt.Errorf("lock %s: %w", path, errors.Join(err, os.Remove(path)))
	}
	return nil
}

func lockOwner(path string) (int, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, fmt.Errorf("lock %s was just released: %w", path, ErrLocked)
	}
	if err != nil {
		return 0, fmt.Errorf("read lock %s: %w", path, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("lock %s names no process, delete it if no promptstor is running: %w", path, ErrLocked)
	}
	return pid, nil
}

func isStale(path string) bool {
	pid, err := lockOwner(path)
	return err == nil && processDead(pid)
}

func unlock(path string) {
	if pid, err := lockOwner(path); err != nil || pid != os.Getpid() {
		return
	}
	if err := os.Remove(path); err != nil {
		return
	}
}
