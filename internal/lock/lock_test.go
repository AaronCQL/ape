package lock

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// The helper subprocess: APE_LOCK_HELPER=<mode> runs helper() instead of the tests.
const helperEnv = "APE_LOCK_HELPER"

func TestMain(m *testing.M) {
	if mode := os.Getenv(helperEnv); mode != "" {
		os.Exit(helper(mode, os.Getenv("APE_LOCK_DIR")))
	}
	os.Exit(m.Run())
}

// helper: "hold" takes the lock, prints "locked", and holds it until stdin closes;
// "try" tries for 100 ms and exits 0 (got it), 2 (timeout) or 1 (other error).
func helper(mode, dir string) int {
	switch mode {
	case "hold":
		release, err := Acquire(dir, 5*time.Second)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("locked")
		io.Copy(io.Discard, os.Stdin)
		release()
		return 0
	case "try":
		release, err := Acquire(dir, 100*time.Millisecond)
		if errors.Is(err, ErrTimeout) {
			return 2
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		release()
		return 0
	}
	return 1
}

func apeDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestAcquire(t *testing.T) {
	dir := apeDir(t)
	defer syscall.Umask(syscall.Umask(0o022))
	release, err := Acquire(dir, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dir, ".git", "ape.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o644 {
		t.Errorf("lock file mode %v", fi.Mode().Perm())
	}

	for _, timeout := range []time.Duration{0, 30 * time.Millisecond, 150 * time.Millisecond} {
		start := time.Now()
		_, err := Acquire(dir, timeout)
		if err != ErrTimeout {
			t.Fatalf("second Acquire(%v) while held: err = %v; want ErrTimeout", timeout, err)
		}
		if waited := time.Since(start); waited < timeout {
			t.Errorf("Acquire(%v) gave up after %v", timeout, waited)
		}
	}

	release()
	release() // idempotent
	release2, err := Acquire(dir, 0)
	if err != nil {
		t.Fatalf("Acquire after release: %v", err)
	}
	release2()
}

// TestAcquireWaits: a waiter gets the lock once the holder releases it within the timeout.
func TestAcquireWaits(t *testing.T) {
	dir := apeDir(t)
	release, err := Acquire(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	time.AfterFunc(100*time.Millisecond, release)
	start := time.Now()
	release2, err := Acquire(dir, 10*time.Second)
	if err != nil {
		t.Fatalf("waiter: %v", err)
	}
	release2()
	if waited := time.Since(start); waited < 80*time.Millisecond {
		t.Errorf("waiter got the lock after %v, before the holder released it", waited)
	}
}

func TestAcquireNoGitDir(t *testing.T) {
	_, err := Acquire(t.TempDir(), time.Second)
	if err == nil || errors.Is(err, ErrTimeout) {
		t.Errorf("err = %v; want an open error", err)
	}
}

func TestCrossProcess(t *testing.T) {
	dir := apeDir(t)
	cmd := func(mode string) *exec.Cmd {
		c := exec.Command(os.Args[0], "-test.run=^$")
		c.Env = append(os.Environ(), helperEnv+"="+mode, "APE_LOCK_DIR="+dir)
		c.Stderr = os.Stderr
		return c
	}
	try := func() int {
		err := cmd("try").Run()
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exit.ExitCode()
		}
		if err != nil {
			t.Fatal(err)
		}
		return 0
	}

	t.Run("child holds, parent waits", func(t *testing.T) {
		holder := cmd("hold")
		stdin, err := holder.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		stdout, err := holder.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := holder.Start(); err != nil {
			t.Fatal(err)
		}
		defer holder.Process.Kill()
		if line, err := bufio.NewReader(stdout).ReadString('\n'); line != "locked\n" {
			t.Fatalf("holder said %q, %v", line, err)
		}
		if _, err := Acquire(dir, 100*time.Millisecond); err != ErrTimeout {
			t.Fatalf("parent Acquire while child holds: err = %v; want ErrTimeout", err)
		}
		if code := try(); code != 2 {
			t.Fatalf("second child while first holds: exit %d; want 2", code)
		}
		stdin.Close() // the holder releases and exits
		release, err := Acquire(dir, 5*time.Second)
		if err != nil {
			t.Fatalf("parent Acquire after child released: %v", err)
		}
		release()
		if err := holder.Wait(); err != nil {
			t.Fatalf("holder: %v", err)
		}
	})

	t.Run("parent holds, child waits", func(t *testing.T) {
		release, err := Acquire(dir, 0)
		if err != nil {
			t.Fatal(err)
		}
		if code := try(); code != 2 {
			t.Fatalf("child while parent holds: exit %d; want 2", code)
		}
		release()
		if code := try(); code != 0 {
			t.Fatalf("child after parent released: exit %d; want 0", code)
		}
	})

	t.Run("lock dies with its holder", func(t *testing.T) {
		holder := cmd("hold")
		stdout, err := holder.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := holder.StdinPipe(); err != nil { // kept open: the holder never releases on its own
			t.Fatal(err)
		}
		if err := holder.Start(); err != nil {
			t.Fatal(err)
		}
		if line, err := bufio.NewReader(stdout).ReadString('\n'); line != "locked\n" {
			t.Fatalf("holder said %q, %v", line, err)
		}
		holder.Process.Kill()
		holder.Wait()
		release, err := Acquire(dir, 5*time.Second)
		if err != nil {
			t.Fatalf("Acquire after holder was killed: %v", err)
		}
		release()
	})
}
