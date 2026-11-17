package gate

import (
	"context"
	"fmt"
	"strings"

	"github.com/Hedronite/go-tops/internal/report"
)

// gitState reads the head and dirtiness of the repository the run judges. A
// directory git cannot read is an environment error (§12.2 code 3).
func gitState(ctx context.Context, ex Executor, dir, base string) (report.Git, error) {
	head, err := ex.Run(ctx, Cmd{Dir: dir, Argv: []string{"git", "rev-parse", "HEAD"}})
	if err != nil {
		return report.Git{}, fmt.Errorf("unreadable repository: %w", err)
	}
	sha := strings.TrimSpace(string(head.Stdout))
	if head.ExitCode != 0 || sha == "" {
		return report.Git{}, fmt.Errorf("unreadable repository: git rev-parse HEAD failed: %s", strings.TrimSpace(string(head.Stderr)))
	}
	st, err := ex.Run(ctx, Cmd{Dir: dir, Argv: []string{"git", "status", "--porcelain"}})
	if err != nil || st.ExitCode != 0 {
		return report.Git{}, fmt.Errorf("unreadable repository: git status failed")
	}
	return report.Git{Head: sha, Base: base, Dirty: strings.TrimSpace(string(st.Stdout)) != ""}, nil
}
