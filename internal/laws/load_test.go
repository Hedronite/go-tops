package laws

import (
	"strings"
	"testing"
	"testing/fstest"
)

// GO_TOPS.md §8.3: YAML fields map to catalog rows (severity, reader, tier, classes).

func TestLawFromYAMLSeverityAndTier(t *testing.T) {
	l, err := lawFromYAML(lawYAML{
		ID: "G-16", Title: "t", Severity: "refuse", Status: "active",
		Reader: "tool", Classes: []string{"binary-cli"}, V01: true,
	})
	if err != nil || l.Severity != SeverityR || l.Tier != 1 || !l.V01 {
		t.Fatalf("G-16 = %+v err %v", l, err)
	}
	l, err = lawFromYAML(lawYAML{ID: "G-22", Title: "t", Severity: "ack", Status: "active", Reader: "tool", Classes: []string{"library-core"}})
	if err != nil || l.Tier != 3 {
		t.Fatalf("G-22 tier = %d", l.Tier)
	}
}

func TestLawFromYAMLRetiredClearsClasses(t *testing.T) {
	l, err := lawFromYAML(lawYAML{
		ID: "G-03", Title: "r", Severity: "refuse", Status: "retired",
		Reader: "analyzer", Classes: []string{"binary-cli"},
	})
	if err != nil || !l.Retired || len(l.Classes) != 0 || l.ID != "G-03" {
		t.Fatalf("G-03 = %+v", l)
	}
}

func TestSeverityOfRejectsUnknown(t *testing.T) {
	if _, err := severityOf("maybe"); err == nil {
		t.Fatal("want error")
	}
}

func TestLawFromYAMLRejectsBadReader(t *testing.T) {
	_, err := lawFromYAML(lawYAML{ID: "G-01", Title: "t", Severity: "ack", Status: "active", Reader: "bogus", Classes: []string{"binary-cli"}})
	if err == nil {
		t.Fatal("want error")
	}
}

func TestLawIDOrderSortsLexicallyByNumber(t *testing.T) {
	if lawIDOrder("laws/G-09.yaml") >= lawIDOrder("laws/G-10.yaml") {
		t.Fatal("G-09 should sort before G-10")
	}
}

func TestCatalogLoadsFortyThreeLaws(t *testing.T) {
	if err := ErrCatalog(); err != nil {
		t.Fatal(err)
	}
	if n := len(Catalog()); n != 43 {
		t.Fatalf("catalog len %d, want 43", n)
	}
}

func TestLoadCatalogFromEmptyFS(t *testing.T) {
	_, err := loadCatalogFrom(fstest.MapFS{})
	if err == nil || !strings.Contains(err.Error(), "no G-NN.yaml") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadCatalogFromOneLaw(t *testing.T) {
	const yaml = "id: G-01\ntitle: one\nseverity: ack\nstatus: active\nclasses: [binary-cli]\nreader: analyzer\nanalyzer: x\nfixture: testdata/x\n"
	out, err := loadCatalogFrom(fstest.MapFS{"laws/G-01.yaml": &fstest.MapFile{Data: []byte(yaml)}})
	if err != nil || len(out) != 1 || out[0].ID != "G-01" {
		t.Fatalf("out=%v err=%v", out, err)
	}
}

func TestReadLawFileRejectsMismatchedName(t *testing.T) {
	const yaml = "id: G-02\ntitle: one\nseverity: ack\nstatus: active\nclasses: [binary-cli]\nreader: analyzer\nanalyzer: x\nfixture: testdata/x\n"
	_, err := readLawFile(fstest.MapFS{"laws/G-01.yaml": &fstest.MapFile{Data: []byte(yaml)}}, "laws/G-01.yaml")
	if err == nil {
		t.Fatal("want error")
	}
}

func TestLoadCatalogFromSortsByLawID(t *testing.T) {
	y9 := "id: G-09\ntitle: a\nseverity: ack\nstatus: active\nclasses: [binary-cli]\nreader: analyzer\nanalyzer: x\nfixture: testdata/x\n"
	y10 := "id: G-10\ntitle: b\nseverity: ack\nstatus: active\nclasses: [tui-app]\nreader: analyzer\nanalyzer: x\nfixture: testdata/x\n"
	out, err := loadCatalogFrom(fstest.MapFS{
		"laws/G-10.yaml": &fstest.MapFile{Data: []byte(y10)},
		"laws/G-09.yaml": &fstest.MapFile{Data: []byte(y9)},
	})
	if err != nil || len(out) != 2 {
		t.Fatalf("out=%v err=%v", out, err)
	}
	if out[0].ID != "G-09" || out[1].ID != "G-10" {
		t.Fatalf("order %v %v, want G-09 then G-10", out[0].ID, out[1].ID)
	}
}
