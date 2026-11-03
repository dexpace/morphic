// Package oas stands for the rule table, one of the two packages that may read
// the version a document declares.
package oas

// ParseMinor stands for the table's reading of a version.
func ParseMinor(version string) (int, bool) { return len(version), version != "" }

// Covers asks it, as the table does, which draws nothing here.
func Covers(version string) bool {
	_, ok := ParseMinor(version)
	return ok
}
