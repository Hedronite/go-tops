package config

// ParseSurvivors reads the text of a survivor ledger as check does: strict YAML
// (§9), the survivorsFile definition of core.schema.json and, for each entry, an
// `expires` that is a real calendar day (§6.4). Whether the day has passed is not
// judged here. file names the ledger in the issues. The error is non-nil only
// when the embedded schema cannot be compiled.
func ParseSurvivors(file string, data []byte) ([]Issue, error) {
	v, issues := parseYAML(data)
	if len(issues) > 0 {
		return tag(issues, file), nil
	}
	found, err := validate(schemaSurvivors, file, v)
	if err != nil || len(found) > 0 {
		return found, err
	}
	c := &checker{}
	c.survivorDays(file, v)
	return c.issues, nil
}
