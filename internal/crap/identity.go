package crap

import (
	"path"
	"strconv"
)

// identities keys every function of all, which scan sorted by file and source
// position. The key is defined once, here (package doc, "Identity"): import
// path, receiver and name; a repeated key gains the file name, and a key that
// still repeats within the file gains "#n".
func identities(all []*fn) []string {
	groups := map[string][]*fn{}
	for _, f := range all {
		k := baseKey(f)
		groups[k] = append(groups[k], f)
	}
	keys := make([]string, len(all))
	for base, g := range groups {
		if len(g) == 1 {
			keys[g[0].id] = base
			continue
		}
		perFile := map[string][]*fn{}
		for _, f := range g {
			perFile[f.File] = append(perFile[f.File], f)
		}
		for file, fs := range perFile {
			withFile := base + "@" + path.Base(file)
			for i, f := range fs {
				keys[f.id] = withFile
				if len(fs) > 1 {
					keys[f.id] += "#" + strconv.Itoa(i+1)
				}
			}
		}
	}
	return keys
}

func baseKey(f *fn) string {
	if f.Recv == "" {
		return f.Pkg + "." + f.Name
	}
	return f.Pkg + "." + f.Recv + "." + f.Name
}
