package laws

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"go.yaml.in/yaml/v3"

	gotops "github.com/Hedronite/go-tops"
)

// lawYAML is the on-disk shape of laws/G-NN.yaml (§8.3).
type lawYAML struct {
	ID       string   `yaml:"id"`
	Title    string   `yaml:"title"`
	Severity string   `yaml:"severity"`
	Status   string   `yaml:"status"`
	Classes  []string `yaml:"classes"`
	Reader   string   `yaml:"reader"`
	Analyzer string   `yaml:"analyzer"`
	Tool     string   `yaml:"tool"`
	Fixture  string   `yaml:"fixture"`
	V01      bool     `yaml:"v01"`
}

var (
	catalogOnce sync.Once
	catalog     []Law
	catalogErr  error
)

// ErrCatalog reports why the embedded catalog failed to load.
func ErrCatalog() error { return catalogErr }

func loadCatalog() {
	out, err := loadCatalogFrom(gotops.Files())
	if err != nil {
		catalogErr = err
		return
	}
	catalog = out
}

func loadCatalogFrom(fsys fs.FS) ([]Law, error) {
	names, err := fs.Glob(fsys, "laws/G-*.yaml")
	if err != nil {
		return nil, fmt.Errorf("glob embedded laws: %w", err)
	}
	if len(names) < 1 {
		return nil, fmt.Errorf("embedded laws: no G-NN.yaml files")
	}
	sort.Slice(names, func(i, j int) bool { return lawIDOrder(names[i]) < lawIDOrder(names[j]) })
	var out []Law
	for _, name := range names {
		l, err := readLawFile(fsys, name)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, nil
}

func readLawFile(fsys fs.FS, name string) (Law, error) {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return Law{}, fmt.Errorf("read %s: %w", name, err)
	}
	var doc lawYAML
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return Law{}, fmt.Errorf("parse %s: %w", name, err)
	}
	if doc.ID == "" {
		return Law{}, fmt.Errorf("%s: missing id", name)
	}
	base := filepath.Base(name)
	if base != doc.ID+".yaml" {
		return Law{}, fmt.Errorf("%s: id %q does not match file name", name, doc.ID)
	}
	return lawFromYAML(doc)
}

func lawIDOrder(name string) int {
	base := strings.TrimSuffix(filepath.Base(name), ".yaml")
	n, err := strconv.Atoi(strings.TrimPrefix(base, "G-"))
	if err != nil {
		return 0
	}
	return n
}

func lawFromYAML(doc lawYAML) (Law, error) {
	sev, err := severityOf(doc.Severity)
	if err != nil {
		return Law{}, err
	}
	reader := Reader(doc.Reader)
	if reader != ReaderAnalyzer && reader != ReaderTool && reader != ReaderAttested && reader != ReaderTBD {
		return Law{}, fmt.Errorf("reader %q is not valid", doc.Reader)
	}
	retired := doc.Status == "retired"
	classes := doc.Classes
	if retired {
		classes = nil
	}
	return Law{
		ID:       doc.ID,
		Title:    doc.Title,
		Severity: sev,
		Reader:   reader,
		Tier:     tierOf(reader, doc.ID),
		V01:      doc.V01,
		Retired:  retired,
		Status:   doc.Status,
		Classes:  classes,
	}, nil
}

func severityOf(s string) (Severity, error) {
	switch s {
	case "refuse":
		return SeverityR, nil
	case "ack":
		return SeverityA, nil
	default:
		return "", fmt.Errorf("severity %q is not refuse or ack", s)
	}
}

func tierOf(reader Reader, id string) int {
	if id == "G-22" || id == "G-35" {
		return 3
	}
	if reader == ReaderTool {
		return 1
	}
	return 0
}

func ensureCatalog() {
	catalogOnce.Do(loadCatalog)
}
