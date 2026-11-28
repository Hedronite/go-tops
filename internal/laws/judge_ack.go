package laws

func acksCover(id string, ev Evidence) bool {
	if !Ackable(id) {
		return false
	}
	return ackCountsSatisfy(ev.Refusals[id], ev.Acks[id])
}

func ackCountsSatisfy(need, have map[string]int) bool {
	if len(need) == 0 {
		return false
	}
	for file, n := range need {
		got := 0
		if have != nil {
			got = have[file]
		}
		if got < n {
			return false
		}
	}
	return true
}
