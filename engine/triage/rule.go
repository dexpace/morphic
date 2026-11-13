// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package triage

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// ErrInvalidRule reports a rule no run can apply as written. It is a Go error
// rather than a diagnostic: the caller configured the run wrong, and the spec
// is not at fault.
var ErrInvalidRule = errors.New("invalid triage rule")

// Rule is what a caller rules about the findings of one diagnostic code: the
// severity they are rated at, or that those at one place are accepted.
//
// A rule naming no place rates every finding of its code; one naming a place
// rules every finding there, one reported later included, and outranks a rule
// of its code naming none. An acceptance names a place, so it accepts nothing
// its code reports elsewhere. A place can hold a whole scope: a pass places a
// collision between two of the document's types at its node, "doc" (#1825).
type Rule struct {
	// Code is the code a finding carries, matched exactly: no part of it is a
	// pattern.
	Code string `json:"code"`
	// Pointer places the rule at the findings this JSON Pointer locates in the
	// spec, the run's first source. The empty pointer places nothing, and a
	// finding in another source has no place a rule names (#1804).
	Pointer jsontext.Pointer `json:"pointer,omitempty"`
	// Node places the rule at the findings an IR pass located at this node
	// (ir.Provenance.Node).
	Node string `json:"node,omitempty"`
	// Severity is what the findings the rule matches are rated at, empty for a
	// rule that accepts them.
	Severity ir.Severity `json:"severity,omitempty"`
	// Accept accepts the findings the rule matches: they fail a run at no
	// threshold, and a renderer counts them rather than showing each.
	Accept bool `json:"accept,omitzero"`
	// Reason says why, for whoever reads the rule next. Every rule gives one.
	Reason string `json:"reason"`
}

// Ruleset is a list of rules once checked, indexed by what a finding is
// matched on. Its zero value holds no rule and rates each finding as reported.
type Ruleset struct {
	rules []Rule
	// byKey indexes each rule by its code and the place it names, a rule naming
	// none under empty locators.
	byKey map[key]int
}

// key is what a rule matches a finding on: its code, and the pointer or the
// node it names, each empty when it names no such place.
type key struct {
	code, pointer, node string
}

// keyOf returns r's key.
func keyOf(r Rule) key {
	return key{code: r.Code, pointer: string(r.Pointer), node: r.Node}
}

// NewRuleset checks each of rules and indexes them, in the order given. A
// refusal is an error wrapping ErrInvalidRule that names the rule's position
// and code: a rule failing check, or two ruling the same findings, since which
// of them applies would be a precedence no reader of the rules can see.
func NewRuleset(rules []Rule) (Ruleset, error) {
	if len(rules) == 0 {
		return Ruleset{}, nil
	}
	s := Ruleset{rules: slices.Clone(rules), byKey: make(map[key]int, len(rules))}
	for i, r := range s.rules {
		why := check(r)
		if earlier, ruled := s.byKey[keyOf(r)]; why == "" && ruled {
			why = fmt.Sprintf("rules the findings rule %d rules", earlier)
		}
		if why != "" {
			return Ruleset{}, fmt.Errorf("triage: rule %d (code %q): %s: %w", i, r.Code, why, ErrInvalidRule)
		}
		s.byKey[keyOf(r)] = i
	}
	return s, nil
}

// check returns why r cannot be applied as written, or "" when it can.
func check(r Rule) string {
	switch {
	case r.Code == "":
		return "names no code"
	case strings.Contains(r.Code, "*"):
		return "writes a wildcard, but a code is matched exactly"
	case r.Pointer != "" && !r.Pointer.IsValid():
		return fmt.Sprintf("pointer %q is not an RFC 6901 JSON Pointer", string(r.Pointer))
	case r.Pointer != "" && r.Node != "":
		return "names a pointer and a node, where a rule names one place"
	case r.Accept && r.Severity != "":
		return "both rates and accepts"
	case r.Accept && r.Pointer == "" && r.Node == "":
		return "accepts without naming a place, which an acceptance names"
	case !r.Accept && r.Severity == "":
		return "neither rates nor accepts"
	case !r.Accept && !known(r.Severity):
		return fmt.Sprintf("severity %q is not one of %s", string(r.Severity), severityNames())
	case strings.TrimSpace(r.Reason) == "":
		return "gives no reason"
	default:
		return ""
	}
}

// known reports whether s is one of the IR's severities.
func known(s ir.Severity) bool {
	_, ok := Rank(s)
	return ok
}

// Severities returns the IR's severities, least severe first, so a severity's
// position is its rank.
func Severities() []ir.Severity {
	return []ir.Severity{ir.SeverityInfo, ir.SeverityWarning, ir.SeverityError}
}

// Rank returns s's position in Severities and true, or false for a severity
// the IR does not declare, which no threshold can rank.
func Rank(s ir.Severity) (int, bool) {
	rank := slices.Index(Severities(), s)
	return rank, rank >= 0
}

// severityNames spells the severities a rule may rate at, most severe first.
func severityNames() string {
	names := make([]string, 0, len(Severities()))
	for _, s := range slices.Backward(Severities()) {
		names = append(names, string(s))
	}
	return strings.Join(names, ", ")
}
