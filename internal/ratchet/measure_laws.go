package ratchet

import (
	"github.com/Hedronite/go-tops/internal/gate"
	"github.com/Hedronite/go-tops/internal/report"
)

// attachRatchetLaws copies judged-law counts into the measure report when the gate ran them.
func attachRatchetLaws(rep *report.Report, env *gate.Env) {
	if env == nil || env.RatchetLaws.ApplicableJudged <= 0 {
		return
	}
	rep.Laws = env.RatchetLaws
}
