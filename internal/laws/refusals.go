package laws

import (
	"github.com/Hedronite/go-tops/internal/lawpass"
)

// FileCounts is a per-file count map keyed by law id.
type FileCounts map[string]map[string]int

// RefusalCounts is per-law per-file instances the gate would refuse before acks.
func RefusalCounts(ev Evidence, rel func(string) string) FileCounts {
	return mergeCounts(lawpass.CountsByLawFile(ev.Lint.Issues, rel), ev.AnalyzersByFile)
}

func mergeCounts(a, b map[string]map[string]int) FileCounts {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	out := FileCounts{}
	for law, files := range a {
		for file, n := range files {
			if out[law] == nil {
				out[law] = map[string]int{}
			}
			out[law][file] += n
		}
	}
	for law, files := range b {
		for file, n := range files {
			if out[law] == nil {
				out[law] = map[string]int{}
			}
			out[law][file] += n
		}
	}
	return out
}
