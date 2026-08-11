package main

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// settingFlag collects the repeated `-opt key=value` settings that configure
// whichever compiler the spec turns out to need.
//
// It carries the pairs verbatim rather than parsing them, because the CLI does
// not know which compiler will read them: the names, the accepted values and
// what counts as a file path are the compiler's own, and a CLI that validated
// any of them would be holding a second copy of a vocabulary it cannot see
// change. What is checked here is the shape every compiler's settings share —
// that a pair has both halves, and that no key was given twice.
type settingFlag map[string]string

// String renders the settings as they were typed, sorted so the rendering does
// not depend on map order. The flag package calls it on a zero value to decide
// whether to print a default, so a nil map must render as the empty string.
func (s settingFlag) String() string {
	pairs := make([]string, 0, len(s))
	for _, key := range slices.Sorted(maps.Keys(s)) {
		pairs = append(pairs, key+"="+s[key])
	}
	return strings.Join(pairs, " ")
}

// Set records one key=value pair. A repeated key is refused rather than
// overwritten: one of the two values would take effect with nothing to say
// which, and a user who typed both meant something by each.
func (s settingFlag) Set(raw string) error {
	if s == nil {
		return fmt.Errorf("no option set to write %q into", raw)
	}
	key, value, ok := strings.Cut(raw, "=")
	if !ok {
		return fmt.Errorf("want key=value, got %q", raw)
	}
	if key == "" {
		return fmt.Errorf("empty option name in %q", raw)
	}
	if _, repeated := s[key]; repeated {
		return fmt.Errorf("option %q set more than once", key)
	}
	s[key] = value
	return nil
}
