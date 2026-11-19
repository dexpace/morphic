// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package triage

import (
	"slices"

	"github.com/dexpace/morphic/ir"
)

// Finding is one diagnostic as a Ruleset rated it.
type Finding struct {
	// Diagnostic is the finding as its stage reported it, its own severity
	// included.
	Diagnostic ir.Diagnostic `json:"diagnostic"`
	// Severity is what the finding is rated at: its rule's, or the reported
	// one when no rule rates it.
	Severity ir.Severity `json:"severity"`
	// Accepted reports that a rule accepted the finding, which then fails a run
	// at no threshold.
	Accepted bool `json:"accepted,omitzero"`
	// Rule is the rule that rated or accepted the finding, or nil for none.
	Rule *Rule `json:"rule,omitzero"`
}

// Rate returns each of diags as s rates it, index for index, and the rules
// that matched none of them, in the order s holds them. A finding meets one
// rule: the one placed at its pointer in the spec, else the one placed at its
// node, else the one naming its code alone. A severity no rule matches is left
// as reported, unknown ones included, which the caller judges.
//
// Each Finding.Rule points into a copy of s's rules made for the call, so one
// call's findings share nothing with another's.
func (s Ruleset) Rate(diags []ir.Diagnostic) ([]Finding, []Rule) {
	rules := slices.Clone(s.rules)
	matched := make([]bool, len(rules))
	var findings []Finding
	if len(diags) > 0 {
		findings = make([]Finding, len(diags))
	}
	for i, diagnostic := range diags {
		findings[i] = Finding{Diagnostic: diagnostic, Severity: diagnostic.Severity}
		at, ok := s.match(diagnostic)
		if !ok {
			continue
		}
		matched[at] = true
		rule := &rules[at]
		findings[i].Rule = rule
		if rule.Accept {
			findings[i].Accepted = true
			continue
		}
		findings[i].Severity = rule.Severity
	}

	var unused []Rule
	for i, rule := range rules {
		if !matched[i] {
			unused = append(unused, rule)
		}
	}
	return findings, unused
}

// match returns the position of the rule diagnostic meets, and false when it
// meets none. A pointer places diagnostic in the spec only when diagnostic is
// in source 0, as a pointer locates a position of its own source alone.
func (s Ruleset) match(diagnostic ir.Diagnostic) (int, bool) {
	prov := diagnostic.Provenance
	if prov.Pointer != "" && prov.Source == 0 {
		if at, ok := s.byKey[key{code: diagnostic.Code, pointer: string(prov.Pointer)}]; ok {
			return at, true
		}
	}
	if prov.Node != "" {
		if at, ok := s.byKey[key{code: diagnostic.Code, node: prov.Node}]; ok {
			return at, true
		}
	}
	at, ok := s.byKey[key{code: diagnostic.Code}]
	return at, ok
}
