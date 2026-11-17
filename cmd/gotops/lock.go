package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Hedronite/go-tops/internal/cli"
	"github.com/Hedronite/go-tops/internal/lock"
)

func init() {
	cli.Register(cli.Command{
		Name:    "lock",
		Summary: "verify or update the tool pins: --verify or --update",
		Run:     runLock,
	})
}

const lockUsage = `Usage: gotops lock --verify
       gotops lock --update [name@version ...]

--verify  re-checks every pin: the toolchain against go env GOVERSION, each
          downloaded binary by SHA-256, each module tool by building it from
          tools/go.mod and reading go version -m. A mismatch exits 2; a missing
          tool exits 1.
--update  re-downloads each pinned binary for every platform and rewrites its
          SHA-256 (the trust mode becomes tofu). name@version raises a pin first;
          toolchain@go1.x.y raises the toolchain pin. Refused when CI=true.
`

func runLock(env cli.Env, args []string) int {
	flags := flag.NewFlagSet("lock", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	verify := flags.Bool("verify", false, "")
	update := flags.Bool("update", false, "")
	if err := flags.Parse(args); err != nil {
		return usageError(env, "lock", lockUsage, err)
	}
	if *verify == *update {
		return usageError(env, "lock", lockUsage, fmt.Errorf("give exactly one of --verify and --update"))
	}
	dir, err := os.Getwd()
	if err != nil {
		env.Stderr.Printf("gotops lock: %v\n", err)
		return cli.ExitEnvironment
	}
	if *verify {
		if flags.NArg() > 0 {
			return usageError(env, "lock", lockUsage, fmt.Errorf("--verify takes no arguments, got %q", flags.Arg(0)))
		}
		return verifyLock(env, dir)
	}
	return updateLock(env, dir, flags.Args(), os.Getenv("CI") == "true")
}

func verifyLock(env cli.Env, dir string) int {
	findings := lock.Verify(lock.VerifyOptions{Dir: dir})
	for _, f := range findings {
		env.Stdout.Printf("%-9s %-22s %s\n", statusWord(f.Status), f.Subject, f.Detail)
	}
	return lock.ExitCode(findings)
}

func statusWord(s lock.Status) string {
	switch s {
	case lock.StatusOK:
		return "ok"
	case lock.StatusMissing:
		return "MISSING"
	}
	return "MISMATCH"
}

// updateLock refuses under CI before it reads anything: a pin change is a
// reviewed change, never an automatic one (§10).
func updateLock(env cli.Env, dir string, args []string, ci bool) int {
	if ci {
		env.Stderr.Printf("gotops lock: --update is refused when CI=true: a pin change is a reviewed change, never an automatic one (GO_TOPS.md §10)\n")
		return exitPolicy
	}
	set, err := parseSet(args)
	if err != nil {
		return usageError(env, "lock", lockUsage, err)
	}
	changes, err := lock.Update(lock.UpdateOptions{Dir: dir, Set: set, Fetch: fetchBody})
	if err != nil {
		env.Stderr.Printf("gotops lock: %v\n", err)
		return cli.ExitUsage
	}
	for _, c := range changes {
		env.Stdout.Printf("%s %s %s: %.12s -> %.12s\n", c.Name, c.Platform, c.Kind, c.Old, c.New)
	}
	env.Stdout.Printf("go-tops.lock updated (%d hashes changed); review the diff before committing\n", len(changes))
	return 0
}

// exitPolicy is the code of a refusal by policy under CI=true (§12.2).
const exitPolicy = 4

func parseSet(args []string) (map[string]string, error) {
	set := map[string]string{}
	for _, a := range args {
		name, ver, ok := strings.Cut(a, "@")
		if !ok || name == "" || ver == "" {
			return nil, fmt.Errorf("%q: want name@version", a)
		}
		set[name] = ver
	}
	return set, nil
}

// maxDownload bounds one download: the largest pinned archive is a few tens of megabytes.
const maxDownload = 512 << 20

// fetchBody downloads url and returns the body.
func fetchBody(url string) ([]byte, error) {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url) //nolint:noctx // a one-shot command; the client timeout bounds it
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDownload+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxDownload {
		return nil, fmt.Errorf("GET %s: more than %d bytes", url, maxDownload)
	}
	return body, nil
}
