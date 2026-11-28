package gate

import (
	"context"

	"github.com/Hedronite/go-tops/internal/lawpass"
)

type lawpassExec struct{ e Executor }

func (a lawpassExec) Run(ctx context.Context, dir string, argv []string) (lawpass.Output, error) {
	out, err := a.e.Run(ctx, Cmd{Dir: dir, Argv: argv})
	return lawpass.Output{ExitCode: out.ExitCode, Stdout: out.Stdout, Stderr: out.Stderr}, err
}

type lawpassTools struct{ t Resolver }

func (a lawpassTools) Resolve(ctx context.Context, name string) ([]string, error) {
	return a.t.Resolve(ctx, name)
}

// StubLawPass satisfies tests that run the gate without a real law lint pass.
func StubLawPass(context.Context, *Env) (lawpass.LintResult, error) {
	return lawpass.LintResult{ByLaw: map[string]int{}}, nil
}
