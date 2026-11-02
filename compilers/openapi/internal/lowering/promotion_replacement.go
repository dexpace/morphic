// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

import (
	"encoding/json/v2"
	"slices"

	"github.com/dexpace/morphic/ir"
)

// Replacements reads from own the names its node's replacements go by
// (TargetDeprecationReplacedBy), with the provenance of the key stating them,
// where a name that names nothing is reported. A string is one name and a list
// of strings several; another value is a misfit, reported as every target
// reports one. Nothing is read from a referent: a replacement names an entity
// of its own node's class, which a carrier and the type it uses are not.
func (p Promoter) Replacements(own ir.Unmodeled) ([]string, ir.Provenance, []ir.Diagnostic) {
	text, at, stated, diags := firstStatedAt(own, p.targets[TargetDeprecationReplacedBy], replacementReading())
	if !stated {
		return nil, ir.Provenance{}, diags
	}
	names, _ := replacementNames(ir.RawValue(text))
	return names, at, diags
}

// Maps reports whether the policy names any source for target.
func (p Promoter) Maps(target ExtensionTarget) bool {
	return len(p.targets[target]) > 0
}

// replacementReading reads a value as the names of replacements, held as the
// value's text so two sources compare as firstStated compares any value.
func replacementReading() reading[string] {
	return reading[string]{
		target: TargetDeprecationReplacedBy,
		want:   "is neither a name nor a list of names",
		read: func(_ ExtensionSource, raw ir.RawValue) (string, statement) {
			names, ok := replacementNames(raw)
			switch {
			case !ok:
				return "", misfit
			case len(names) == 0:
				return "", statesNothing
			default:
				return string(raw), states
			}
		},
	}
}

// replacementNames decodes raw as one name or a list of names, false for any
// other value or a list holding an empty name. An empty string names nothing.
func replacementNames(raw ir.RawValue) ([]string, bool) {
	if text, ok := extensionText(raw); ok {
		if text == "" {
			return nil, true
		}
		return []string{text}, true
	}
	var names []string
	if err := json.Unmarshal(raw, &names); err != nil {
		return nil, false
	}
	if slices.Contains(names, "") {
		return nil, false
	}
	return names, true
}
