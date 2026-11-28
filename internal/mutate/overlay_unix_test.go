//go:build unix

package mutate

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Hedronite/go-tops/internal/gate"
	"github.com/Hedronite/go-tops/internal/gobuild"
)

func init() {
	if os.Getenv("GOTOPS_OVERLAY_SIGINT_HELPER") != "1" {
		return
	}
	os.Exit(runOverlaySIGINTHelper())
}

func runOverlaySIGINTHelper() int {
	dir := os.Getenv("GOTOPS_OVERLAY_SIGINT_DIR")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	build := gobuild.Build{CGOEnabled: true}
	diff, err := ComputeDiff(ctx, gate.OS{}, dir, "main", build)
	if err != nil {
		return 2
	}
	env := gate.Env{
		Dir:    dir,
		Base:   "main",
		Exec:   gate.OS{},
		Config: gate.Config{Base: "main", Mutation: gate.MutationConfig{Engine: "overlay"}},
	}
	if _, err := RunOverlay(ctx, env, diff, build, OverlayOptions{}); err != nil {
		return 1
	}
	return 0
}

func TestRunOverlaySIGINTKillsMutantAndTempDir(t *testing.T) {
	dir := gitSetupOverlayRepo(t, map[string]string{
		"go.mod":  "module example.com/ctrlcmut\n\ngo 1.26\n",
		"calc.go": "package ctrlcmut\n\nfunc IsBase(n int) bool { return n < 0 }\n",
		"c_test.go": `package ctrlcmut

import "testing"

func TestBase(t *testing.T) {
	if !IsBase(-1) {
		t.Fatal("bad")
	}
}
`,
	})
	writeTreeFiles(t, dir, map[string]string{
		"calc.go": "package ctrlcmut\n\nfunc IsBase(n int) bool { return n == -1 }\n",
		"c_test.go": `package ctrlcmut

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestMutantWaitsForSIGINT(t *testing.T) {
	if !IsBase(-1) {
		child := exec.CommandContext(context.Background(), "sleep", "30")
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		tmp := filepath.Join(".", "mut-started.tmp")
		body := fmt.Sprintf("%d\n%d\n%s\n", os.Getpid(), child.Process.Pid, filepath.Base(os.Args[0]))
		if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(tmp, "mut-started.txt"); err != nil {
			t.Fatal(err)
		}
		if err := child.Wait(); err != nil {
			t.Fatal(err)
		}
	}
}
`,
	})

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	subTmp := t.TempDir()
	helper := exec.CommandContext(context.Background(), self, "-test.run=^$")
	helper.Env = append(os.Environ(),
		"GOTOPS_OVERLAY_SIGINT_HELPER=1",
		"GOTOPS_OVERLAY_SIGINT_DIR="+dir,
		"TMPDIR="+subTmp,
	)
	helper.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-helper.Process.Pid, syscall.SIGKILL) //nolint:errcheck // best-effort cleanup
	})

	startedPath := filepath.Join(dir, "mut-started.txt")
	mutPID, sleepPID, binBase := waitForMutantMarker(t, startedPath, 20*time.Second)
	if !strings.HasPrefix(binBase, "mut-") {
		t.Fatalf("expected running mut-*.test binary, got %q", binBase)
	}

	if err := syscall.Kill(-helper.Process.Pid, syscall.SIGINT); err != nil {
		t.Fatalf("send SIGINT to overlay helper pgid: %v", err)
	}
	waitDone := make(chan error, 1)
	go func() {
		waitDone <- helper.Wait()
	}()
	select {
	case werr := <-waitDone:
		t.Logf("helper exited after SIGINT: %v", werr)
	case <-time.After(10 * time.Second):
		t.Fatal("overlay helper did not exit promptly after SIGINT")
	}

	assertUnixProcessDead(t, "mutant binary", mutPID)
	assertUnixProcessDead(t, "mutant child sleep", sleepPID)

	matches, globErr := filepath.Glob(filepath.Join(subTmp, "gotops-overlay-*"))
	if globErr != nil {
		t.Fatal(globErr)
	}
	if len(matches) != 0 {
		t.Fatalf("expected gotops-overlay temp dirs cleaned on SIGINT, found %v", matches)
	}
}

func waitForMutantMarker(t *testing.T, path string, timeout time.Duration) (int, int, string) {
	t.Helper()
	deadline := time.After(timeout)
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		raw, err := os.ReadFile(path)
		if err == nil {
			lines := strings.Fields(string(raw))
			if len(lines) >= 3 {
				p1, e1 := strconv.Atoi(lines[0])
				p2, e2 := strconv.Atoi(lines[1])
				if e1 == nil && e2 == nil {
					return p1, p2, lines[2]
				}
			}
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %s", path)
			return 0, 0, ""
		case <-tick.C:
		}
	}
}

func assertUnixProcessDead(t *testing.T, label string, pid int) {
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
