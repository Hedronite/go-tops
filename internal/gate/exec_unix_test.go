//go:build unix

package gate

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

func init() {
	if os.Getenv("GOTOPS_EXEC_PGID_HELPER") == "1" {
		c := exec.CommandContext(context.Background(), "sleep", "30")
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		if err := c.Run(); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
}

func TestConfigureExecCmdAndKillProcessGroup(t *testing.T) {
	cmd := exec.CommandContext(context.Background(), "true")
	configureExecCmd(cmd)
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Fatal("expected SysProcAttr.Setpgid = true")
	}
	if cmd.WaitDelay != 2*time.Second {
		t.Fatalf("WaitDelay = %v, want 2s", cmd.WaitDelay)
	}
	if err := killProcessGroup(&exec.Cmd{}); err != nil {
		t.Fatalf("killProcessGroup with nil Process: %v", err)
	}
}

func TestOSExecTimeoutKillsProcessGroupFast(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, runErr := (OS{}).Run(ctx, Cmd{
		Argv: []string{self, "-test.run=^$"},
		Env:  []string{"GOTOPS_EXEC_PGID_HELPER=1"},
	})
	elapsed := time.Since(start)
	if ctx.Err() == nil && runErr == nil {
		t.Fatal("expected context timeout error or non-nil outcome")
	}
	// If only the parent PID were killed instead of -pgid, sleep 30 would hold stdout and delay return to >=2s.
	if elapsed >= 1500*time.Millisecond {
		t.Fatalf("expected process group kill before WaitDelay, elapsed=%v (err=%v)", elapsed, runErr)
	}
}
