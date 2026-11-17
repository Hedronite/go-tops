package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// The protocol version is stated four times and must be one value: the header
// line of GO_TOPS.md, the "Protocol version" row of its section 2 table, and
// the protocol constant of each schema the protocol ships.
var (
	headerVersionRe = regexp.MustCompile(`^\*\*Spec status:\*\*\s+(\S+)`)
	protocolRowRe   = regexp.MustCompile(`^\|\s*Protocol version\s*\|\s*\*\*([^*]+)\*\*`)
	versionSchemas  = []string{"schema/go-tops.schema.json", "schema/gate-report.schema.json"}
)

type versionSource struct {
	Where, Version string
}

func checkVersion(r *repo) checkResult {
	res := checkResult{}
	add := func(file string, line int, format string, a ...any) {
		res.Findings = append(res.Findings, finding{file, line, fmt.Sprintf(format, a...)})
	}
	var srcs []versionSource
	f, ok := r.byRel[catalogFile]
	if !ok {
		add(catalogFile, 0, "not found")
		return res
	}
	for i, line := range f.Lines {
		if f.InFence[i] {
			continue
		}
		if m := headerVersionRe.FindStringSubmatch(line); m != nil {
			srcs = append(srcs, versionSource{fmt.Sprintf("%s:%d header", f.Rel, i+1), m[1]})
		}
		if m := protocolRowRe.FindStringSubmatch(line); m != nil {
			srcs = append(srcs, versionSource{fmt.Sprintf("%s:%d section 2 row", f.Rel, i+1), m[1]})
		}
	}
	if len(srcs) != 2 {
		add(catalogFile, 0, "want one header version line and one section 2 Protocol version row, found %d version statements", len(srcs))
	}
	for _, s := range versionSchemas {
		data, err := os.ReadFile(filepath.Join(r.Root, filepath.FromSlash(s)))
		if err != nil {
			add(s, 0, "not found: the protocol constant lives here")
			continue
		}
		var sch struct {
			Properties struct {
				Protocol struct {
					Const string `json:"const"`
				} `json:"protocol"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(data, &sch); err != nil || sch.Properties.Protocol.Const == "" {
			add(s, 0, "no properties.protocol.const")
			continue
		}
		srcs = append(srcs, versionSource{s + " protocol const", sch.Properties.Protocol.Const})
	}
	for _, s := range srcs[1:] {
		if s.Version != srcs[0].Version {
			add(catalogFile, 0, "%s says %q but %s says %q", s.Where, s.Version, srcs[0].Where, srcs[0].Version)
		}
	}
	if len(srcs) > 0 {
		res.Summary = fmt.Sprintf("protocol %s in %d places", srcs[0].Version, len(srcs))
	}
	return res
}
