package detect

import "strings"

// isVersion reports whether value reads as a version rather than prose: one
// word, beginning with a digit. That admits every shape majorMinor is written
// for, and suffixed ones such as "3.1.0-rc1" on purpose: a document writing one
// is this compiler's own and wrong, and the precise complaint is load's to
// make, where declining here would give the engine's generic "unrecognized
// format". Prose has a space in it, and that is what the guard keeps out.
func isVersion(value string) bool {
	if value == "" || value[0] < '0' || value[0] > '9' {
		return false
	}
	return !strings.ContainsAny(value, " \t")
}

// majorMinor returns the "major.minor" prefix of a dotted version string,
// e.g. "3.1.0" → "3.1". Strings with fewer than two dots — a bare major
// version, or a version already in major.minor form — are returned unchanged.
func majorMinor(version string) string {
	firstDot := -1
	for i := range len(version) {
		if version[i] != '.' {
			continue
		}
		if firstDot < 0 {
			firstDot = i
			continue
		}
		return version[:i]
	}
	return version
}
