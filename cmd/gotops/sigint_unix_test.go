//go:build unix

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestGateSIGINTKillsChildProcessGroups(t *testing.T) {
	runSubcommandSIGINTTest(t, "gate", "--only", "tests")
}

func TestBaselineInitSIGINTKillsChildProcessGroups(t *testing.T) {
	runSubcommandSIGINTTest(t, "baseline", "--init")
}

func runSubcommandSIGINTTest(t *testing.T, subArgs ...string) {
	t.Helper()
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "gotops")
	bld := exec.CommandContext(context.Background(), "go", "build", "-o", bin, "./cmd/gotops")
	bld.Dir = repoRoot
	if out, err := bld.CombinedOutput(); err != nil {
		t.Fatalf("build gotops: %v\n%s", err, string(out))
	}

	dir := t.TempDir()
	pidFile := filepath.Join(dir, "started.pid")
	for _, rel := range []string{"go-tops.lock", filepath.Join("tools", "go.mod"), filepath.Join("tools", "go.sum")} {
		data, rerr := os.ReadFile(filepath.Join(repoRoot, rel))
		if rerr != nil {
			t.Fatal(rerr)
		}
		dst := filepath.Join(dir, rel)
		if mkerr := os.MkdirAll(filepath.Dir(dst), 0o755); mkerr != nil {
			t.Fatal(mkerr)
		}
		if werr := os.WriteFile(dst, data, 0o644); werr != nil {
			t.Fatal(werr)
		}
	}
	files := map[string]string{
		"go.mod": "module example.com/ctrlcgate\n\ngo 1.26\n",
		"go-tops.yaml": `protocol: "0.4.7"
version: "0.1.0"
module: example.com/ctrlcgate
class: library-core
enforce: hard

toolchain:
  go_floor: "1.26"
  go_current: "1.27"

gates:
  mutation:
    enabled: true
`,
		"a.go": "package ctrlcgate\n\nfunc F() int { return 1 }\n",
		"a_test.go": `package ctrlcgate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestWaitForSIGINT(t *testing.T) {
	child := exec.CommandContext(context.Background(), "sleep", "30")
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(".", "started.pid.tmp")
	body := fmt.Sprintf("%d\n%d\n", os.Getpid(), child.Process.Pid)
	if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, "started.pid"); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err != nil {
		t.Fatal(err)
	}
}
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.name", "Test Author"},
		{"config", "user.email", "author@example.com"},
		{"config", "commit.gpgsign", "false"},
		{"add", "."},
		{"commit", "-m", "base"},
	} {
		gcmd := exec.CommandContext(context.Background(), "git", args...)
		gcmd.Dir = dir
		if out, gerr := gcmd.CombinedOutput(); gerr != nil {
			t.Fatalf("git %v: %v\n%s", args, gerr, string(out))
		}
	}

	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "CI=") || strings.HasPrefix(kv, "GITHUB_ACTIONS=") {
			continue
		}
		env = append(env, kv)
	}

	var gateOut lockedBuf
	gateCmd := exec.CommandContext(context.Background(), bin, subArgs...)
	gateCmd.Dir = dir
	gateCmd.Env = env
	gateCmd.Stdout = &gateOut
	gateCmd.Stderr = &gateOut
	gateCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := gateCmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-gateCmd.Process.Pid, syscall.SIGKILL) //nolint:errcheck // best-effort cleanup
	})

	waitDone := make(chan error, 1)
	go func() {
		waitDone <- gateCmd.Wait()
	}()

	testPID, sleepPID := waitForTwoPIDs(t, pidFile, 20*time.Second, waitDone, &gateOut)
	if err := syscall.Kill(-gateCmd.Process.Pid, syscall.SIGINT); err != nil {
		t.Fatalf("send SIGINT to gate pgid: %v", err)
	}

	select {
	case werr := <-waitDone:
		t.Logf("gate exited after SIGINT: %v", werr)
	case <-time.After(10 * time.Second):
		t.Fatal("gotops did not exit promptly after SIGINT")
	}

	assertProcessDead(t, "test binary", testPID)
	assertProcessDead(t, "sleep child", sleepPID)
}

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func waitForTwoPIDs(t *testing.T, path string, timeout time.Duration, waitDone <-chan error, gateOut *lockedBuf) (int, int) {
	t.Helper()
	deadline := time.After(timeout)
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		raw, err := os.ReadFile(path)
		if err == nil {
			lines := strings.Fields(string(raw))
			if len(lines) >= 2 {
				p1, e1 := strconv.Atoi(lines[0])
				p2, e2 := strconv.Atoi(lines[1])
				if e1 == nil && e2 == nil {
					return p1, p2
				}
			}
		}
		select {
		case werr := <-waitDone:
			t.Fatalf("gate exited early (%v) before starting tests:\n%s", werr, gateOut.String())
			return 0, 0
		case <-deadline:
			t.Fatalf("timed out waiting for %s:\n%s", path, gateOut.String())
			return 0, 0
		case <-tick.C:
		}
	}
}

func assertProcessDead(t *testing.T, label string, pid int) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		select {
		case <-deadline:
			_ = syscall.Kill(pid, syscall.SIGKILL) //nolint:errcheck // cleanup if failed
			t.Fatalf("orphan %s pid=%d still alive after SIGINT", label, pid)
			return
		case <-tick.C:
		}
	}
}
