package oas

import (
	"strings"
)

// Minor is an OpenAPI 3 minor version. The zero value is no version.
type Minor uint8

// The minors the table covers, oldest first so that they order.
const (
	V30 Minor = 1 + iota
	V31
	V32
)

// minors lists every Minor in order.
func minors() []Minor { return []Minor{V30, V31, V32} }

// String spells the minor as the specification does: "3.1".
func (m Minor) String() string {
	switch m {
	case V30:
		return "3.0"
	case V31:
		return "3.1"
	case V32:
		return "3.2"
	default:
		return "none"
	}
}

// ParseMinor returns the minor of an OpenAPI version string such as "3.0.3",
// and whether the table covers it. The major.minor prefix it compares is a
// slice of version, so the answer allocates nothing: the lowering asks it of
// every schema whose null it reads (lowering.Ctx.Is30).
func ParseMinor(version string) (Minor, bool) {
	major, rest, ok := strings.Cut(version, ".")
	if !ok {
		return 0, false
	}
	minor, _, _ := strings.Cut(rest, ".")
	prefix := version[:len(major)+1+len(minor)]
	for _, m := range minors() {
		if prefix == m.String() {
			return m, true
		}
	}
	return 0, false
}

// Span is a run of minors, inclusive at both ends. The zero Span holds no
// minor, which is how a row says "never"; a zero Until leaves the run open,
// reaching every later minor.
type Span struct{ Since, Until Minor }

// Always is every minor.
func Always() Span { return Span{Since: V30} }

// From is the minor m and every later one.
func From(m Minor) Span { return Span{Since: m} }

// Only is the minor m alone.
func Only(m Minor) Span { return Span{Since: m, Until: m} }

// Through is every minor up to and including m.
func Through(m Minor) Span { return Span{Since: V30, Until: m} }

// Never is the empty run: no minor.
func Never() Span { return Span{} }

// Empty reports whether the span holds no minor.
func (s Span) Empty() bool { return s.Since == 0 }

// Holds reports whether minor m falls in the span.
func (s Span) Holds(m Minor) bool {
	return s.Since != 0 && m >= s.Since && (s.Until == 0 || m <= s.Until)
}

// Later reports whether the span starts after minor m: a construct it
// describes was added by a later minor than the one declared.
func (s Span) Later(m Minor) bool { return s.Since > m }

// Valid reports whether the span is empty or runs forward over known minors.
func (s Span) Valid() bool {
	if s.Empty() {
		return s.Until == 0
	}
	known := s.Since <= V32 && s.Until <= V32
	return known && (s.Until == 0 || s.Until >= s.Since)
}

// Overlaps reports whether the two spans share a minor.
func (s Span) Overlaps(o Span) bool {
	for _, m := range minors() {
		if s.Holds(m) && o.Holds(m) {
			return true
		}
	}
	return false
}
