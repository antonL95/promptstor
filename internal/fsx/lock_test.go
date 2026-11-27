package fsx

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func deadPID(t *testing.T) int {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stale locks are never reclaimed on windows")
	}
	command := exec.Command(os.Args[0], "-test.run=^$")
	if err := command.Run(); err != nil {
		t.Fatal(err)
	}
	return command.ProcessState.Pid()
}

func TestLockSecondAcquireFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "lock")
	release, err := Acquire(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	_, err = Acquire(path, 0)

	if !errors.Is(err, ErrLocked) {
		t.Fatalf("err = %v, want ErrLocked", err)
	}
	if want := fmt.Sprintf("another promptstor run holds the lock (pid %d)", os.Getpid()); err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}
	assertContent(t, path, strconv.Itoa(os.Getpid()))
}

func TestLockReleaseAllowsNextAcquireOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	first, err := Acquire(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	first()
	second, err := Acquire(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer second()

	first()

	if _, err := os.Stat(path); err != nil {
		t.Errorf("a repeated release removed the next lock: %v", err)
	}
}

func TestLockWaitsForRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	first, err := Acquire(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	timer := time.AfterFunc(120*time.Millisecond, first)
	defer timer.Stop()

	second, err := Acquire(path, 10*time.Second)

	if err != nil {
		t.Fatal(err)
	}
	second()
}

func TestLockStalePidReclaimed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	mustWrite(t, path, strconv.Itoa(deadPID(t)), 0o600)

	release, err := Acquire(path, 0)

	if err != nil {
		t.Fatal(err)
	}
	assertContent(t, path, strconv.Itoa(os.Getpid()))
	release()
	assertEntries(t, filepath.Dir(path), 0)
}

func TestLockStaleReclaimHasOneWinner(t *testing.T) {
	stale := strconv.Itoa(deadPID(t))
	for range 50 {
		path := filepath.Join(t.TempDir(), "lock")
		mustWrite(t, path, stale, 0o600)
		var winners atomic.Int32
		var group sync.WaitGroup
		for range 8 {
			group.Go(func() {
				_, err := Acquire(path, 0)
				switch {
				case err == nil:
					winners.Add(1)
				case !errors.Is(err, ErrLocked):
					t.Error(err)
				}
			})
		}
		group.Wait()
		if got := winners.Load(); got != 1 {
			t.Fatalf("%d goroutines acquired the lock", got)
		}
	}
}

func TestLockWithoutPidIsHeld(t *testing.T) {
	for _, content := range []string{"", "junk", "-1", "0"} {
		path := filepath.Join(t.TempDir(), "lock")
		mustWrite(t, path, content, 0o600)

		_, err := Acquire(path, 0)

		if !errors.Is(err, ErrLocked) {
			t.Errorf("content %q: err = %v, want ErrLocked", content, err)
		}
		assertContent(t, path, content)
	}
}
