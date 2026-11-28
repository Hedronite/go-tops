package laws

// Ackable reports whether a catalog law may be acknowledged (severity A only).
func Ackable(id string) bool {
	for _, l := range Catalog() {
		if l.ID == id {
			return l.Severity == SeverityA && l.Active()
		}
	}
	return false
}
