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
// collision between two of the document's types at its node, "document" (#1825).
type Rule struct {
	// Code is the code a finding carries, matched exactly: no part of it is a
	// pattern.
	Code string `json:"code"`
	// Pointer places the rule at the findings this JSON Pointer locates in the
	// spec, the run's first source. The empty pointer places nothing, and a
	// finding in another source has no place a rule names (#1804).
	Pointer jsontext.Pointer `json:"pointer,omitempty"`
	// Node places the rule at the findings located at this node
	// (ir.Provenance.Node): an IR pass's about the document, or a compiler's
	// naming what it decided, as the member a fold settled.
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

// keyOf returns rule's key.
func keyOf(rule Rule) key {
	return key{code: rule.Code, pointer: string(rule.Pointer), node: rule.Node}
}

// NewRuleset checks each of rules and indexes them, in the order given. A
// refusal is an error wrapping ErrInvalidRule that names the rule's position
// and code: a rule failing check, or two ruling the same findings, since which
// of them applies would be a precedence no reader of the rules can see.
func NewRuleset(rules []Rule) (Ruleset, error) {
	if len(rules) == 0 {
		return Ruleset{}, nil
	}
	ruleset := Ruleset{rules: slices.Clone(rules), byKey: make(map[key]int, len(rules))}
	for i, rule := range ruleset.rules {
		why := check(rule)
		if earlier, ruled := ruleset.byKey[keyOf(rule)]; why == "" && ruled {
			why = fmt.Sprintf("rules the findings rule %d rules", earlier)
		}
		if why != "" {
			return Ruleset{}, fmt.Errorf("triage: rule %d (code %q): %s: %w", i, rule.Code, why, ErrInvalidRule)
		}
		ruleset.byKey[keyOf(rule)] = i
	}
	return ruleset, nil
}

// check returns why rule cannot be applied as written, or "" when it can.
func check(rule Rule) string {
	switch {
	case rule.Code == "":
		return "names no code"
	case strings.Contains(rule.Code, "*"):
		return "writes a wildcard, but a code is matched exactly"
	case rule.Pointer != "" && !rule.Pointer.IsValid():
		return fmt.Sprintf("pointer %q is not an RFC 6901 JSON Pointer", string(rule.Pointer))
	case rule.Pointer != "" && rule.Node != "":
		return "names a pointer and a node, where a rule names one place"
	case rule.Accept && rule.Severity != "":
		return "both rates and accepts"
	case rule.Accept && rule.Pointer == "" && rule.Node == "":
		return "accepts without naming a place, which an acceptance names"
	case !rule.Accept && rule.Severity == "":
		return "neither rates nor accepts"
	case !rule.Accept && !rule.Severity.Valid():
		return fmt.Sprintf("severity %q is not one of %s", string(rule.Severity), strings.Join(ir.SeverityNames(), ", "))
	case strings.TrimSpace(rule.Reason) == "":
		return "gives no reason"
	default:
		return ""
	}
}
