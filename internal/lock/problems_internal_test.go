package lock

import (
	"errors"
	"reflect"
	"testing"
)

// The validator's error text has one "- at '<pointer>': <message>" line per
// violated rule. problemsOf reads them; the rest of the lock tests reach it only
// through a lock that fails the schema.
func TestProblemsOfSplitsOneLinePerRule(t *testing.T) {
	err := errors.New("jsonschema validation failed with 'mem:///schema/lock.schema.json#'\n" +
		"- at '': missing property 'toolchain'\n" +
		"- at '/binaries/0/version': got string, want object")
	want := []string{"/: missing property 'toolchain'", "/binaries/0/version: got string, want object"}
	if got := problemsOf(err); !reflect.DeepEqual(got, want) {
		t.Errorf("problemsOf = %q, want %q", got, want)
	}
}

func TestProblemsOfNamesTheFirstLineOfAnErrorItCannotSplit(t *testing.T) {
	got := problemsOf(errors.New("something else went wrong\nwith detail"))
	if !reflect.DeepEqual(got, []string{"something else went wrong"}) {
		t.Errorf("problemsOf = %q", got)
	}
}

func TestProblemsOfNoErrorIsNoProblem(t *testing.T) {
	if got := problemsOf(nil); got != nil {
		t.Errorf("problemsOf(nil) = %q", got)
	}
}
