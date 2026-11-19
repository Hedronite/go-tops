package crap_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hedronite/go-tops/internal/gobuild"
)

const module = "example.com/m"

// linux is the build every fixture run pins, so a result never depends on the
// host's GOOS.
var linux = gobuild.Build{GOOS: "linux", GOARCH: "amd64"}

// fixtureModule writes files (path to content) into a new module and returns
// its directory.
func fixtureModule(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "go.mod"), "module "+module+"\n\ngo 1.26\n")
	for name, src := range files {
		write(t, filepath.Join(dir, filepath.FromSlash(name)), src)
	}
	return dir
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// profile builds a cover profile from lines of "file line.col,line.col stmts count".
func profile(t *testing.T, dir string, lines ...string) string {
	t.Helper()
	p := filepath.Join(dir, ".go-tops", "cover.out")
	write(t, p, "mode: atomic\n"+strings.Join(lines, "\n")+"\n")
	return p
}

// block is one profile line for a file of the fixture module.
func block(file string, sl, sc, el, ec, stmts, count int) string {
	return fmt.Sprintf("%s/%s:%d.%d,%d.%d %d %d", module, file, sl, sc, el, ec, stmts, count)
}

// shaped is a function F of a fixture with plain `if` statements and error
// guards, each on known lines, so that a profile can put blocks inside the
// function, inside the guards, or outside both.
type shaped struct {
	src        string
	tail       int   // the line of the first of two plain statements at the end
	guardLines []int // the line of each guard's `if`; its body block is line.17 to line+2.2
}

func shapeFunc(ifs, guards int) shaped {
	var b strings.Builder
	line := 1
	emit := func(s string) { b.WriteString(s + "\n"); line++ }
	emit("package m")
	emit("")
	emit("func mk() error { return nil }")
	emit("")
	emit("func F(a bool) error {")
	var s shaped
	for i := 0; i < ifs; i++ {
		emit("\tif a {")
		emit("\t\tprintln()")
		emit("\t}")
	}
	for i := 0; i < guards; i++ {
		if i == 0 {
			emit("\terr := mk()")
		} else {
			emit("\terr = mk()")
		}
		s.guardLines = append(s.guardLines, line)
		emit("\tif err != nil {")
		emit("\t\treturn err")
		emit("\t}")
	}
	s.tail = line
	emit("\tprintln(1)")
	emit("\tprintln(2)")
	emit("\treturn nil")
	emit("}")
	s.src = b.String()
	return s
}
