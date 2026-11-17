package config

import (
	"errors"
	"io/fs"
	"strconv"
)

// lock validates go-tops.lock, when there is one, against schema/lock.schema.json
// (GO_TOPS.md §10), and reports a binary name that appears twice, which JSON
// Schema cannot say. A repo with no lock has nothing to check here: the hygiene
// gate fails on the toolchain pin it cannot read.
func (c *checker) lock() error {
	data, err := c.readRel(lockFile)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	v, issues := parseYAML(data)
	if len(issues) > 0 {
		c.issues = append(c.issues, tag(issues, lockFile)...)
		return nil
	}
	found, err := validate(schemaLock, lockFile, v)
	if err != nil {
		return err
	}
	c.issues = append(c.issues, found...)
	c.duplicateBinaries(v)
	return nil
}

// duplicateBinaries: a name pins one binary, so a second entry with the same name
// is ambiguous. It reads the parsed document directly, so it reports even when
// the schema found other faults.
func (c *checker) duplicateBinaries(v any) {
	first := map[string]int{}
	for i, e := range asList(asMap(v)["binaries"]) {
		name, ok := asMap(e)["name"].(string)
		if !ok {
			continue
		}
		if j, dup := first[name]; dup {
			c.addTo(lockFile, "lock-duplicate", "/binaries/"+strconv.Itoa(i)+"/name",
				"binary name %q appears twice, first at /binaries/%d; a name pins one binary (§10)", name, j)
			continue
		}
		first[name] = i
	}
}
