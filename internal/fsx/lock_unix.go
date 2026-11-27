//go:build unix

package fsx

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

func processDead(pid int) bool {
	return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

func removeStaleLock(path string) (err error) {
	if !isStale(path) {
		return nil
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("reclaim lock %s: %w", path, err)
	}
	defer func() { err = errors.Join(err, dir.Close()) }()
	if err := syscall.Flock(int(dir.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("reclaim lock %s: %w", path, err)
	}
	if !isStale(path) {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("reclaim lock %s: %w", path, err)
	}
	return nil
}
