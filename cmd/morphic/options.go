// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package main

import (
	"encoding/json/jsontext"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/dexpace/morphic/engine"
)

// settingFlag collects the repeated `--opt key=value` settings that configure
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
// not depend on map order. pflag calls it on a zero value to decide
// whether to print a default, so a nil map must render as the empty string.
func (s settingFlag) String() string {
	pairs := make([]string, 0, len(s))
	for _, key := range slices.Sorted(maps.Keys(s)) {
		pairs = append(pairs, key+"="+s[key])
	}
	return strings.Join(pairs, " ")
}

// Type names the value in flag documentation.
func (s settingFlag) Type() string { return "key=value" }

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

// pointerFlag is --explain's value: a JSON Pointer, and whether the flag was
// given at all. The empty pointer names the whole document, so an empty value is
// a coordinate like any other and cannot double as the flag being absent.
type pointerFlag struct {
	pointer jsontext.Pointer
	set     bool
}

// String renders the pointer as given. pflag calls it on a zero value
// to decide whether to print a default, and the empty string prints none.
func (f *pointerFlag) String() string { return string(f.pointer) }

// Type names the value in flag documentation.
func (f *pointerFlag) Type() string { return "pointer" }

// Set records the pointer and that the flag was given.
func (f *pointerFlag) Set(raw string) error {
	f.pointer, f.set = jsontext.Pointer(raw), true
	return nil
}

// passFlag collects the repeated `--disable-pass name` values, verbatim and in
// order. Which names exist, and what a repeated or empty one means, is the
// engine's to say: checking them here as well would keep a second copy of a
// list the CLI cannot see change.
type passFlag []string

// String renders the names as typed. A flag never given renders as the empty
// string, which pflag reads as having no default.
func (p *passFlag) String() string { return strings.Join(*p, " ") }

// Type names the value in flag documentation.
func (p *passFlag) Type() string { return "name" }

// Set records one pass name.
func (p *passFlag) Set(raw string) error {
	*p = append(*p, raw)
	return nil
}

// failOnFlag is --fail-on's value. Unlike a pass name, a threshold does not
// depend on which engine runs, so it is parsed as it is set: one the engine
// does not know is a misuse reported while the flags are read, and the run is
// handed a value already checked rather than the text it came from.
type failOnFlag engine.FailOn

// String renders the threshold as typed.
func (f *failOnFlag) String() string { return string(*f) }

// Type names the value in flag documentation.
func (f *failOnFlag) Type() string { return "threshold" }

// Set records the threshold raw names, refusing one the engine does not know.
func (f *failOnFlag) Set(raw string) error {
	failOn, err := engine.ParseFailOn(raw)
	if err != nil {
		return err
	}
	*f = failOnFlag(failOn)
	return nil
}
