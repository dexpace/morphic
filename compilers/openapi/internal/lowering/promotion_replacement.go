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
	names, _ := listedText(ir.RawValue(text))
	return names, at, diags
}

// Maps reports whether the policy names any source for target.
func (p Promoter) Maps(target ExtensionTarget) bool {
	return len(p.targets[target]) > 0
}

// Writes reports whether own keeps a key the policy reads target from,
// whatever its value says: a document writing one has spoken to the target,
// which a reading of its names then leaves alone.
func (p Promoter) Writes(own ir.Unmodeled, target ExtensionTarget) bool {
	for _, src := range p.targets[target] {
		if _, written := own[extensionKeyPrefix+src.Key]; written {
			return true
		}
	}
	return false
}

// replacementReading reads a value as the names of replacements, held as the
// text of their list so two sources compare as firstStated compares any value,
// a name and a list holding it alone stating one replacement.
func replacementReading() reading[string] {
	return reading[string]{
		target: TargetDeprecationReplacedBy,
		want:   "is neither a name nor a list of names",
		read: func(_ ExtensionSource, raw ir.RawValue) (string, statement) {
			names, ok := listedText(raw)
			switch {
			case !ok:
				return "", misfit
			case len(names) == 0:
				return "", statesNothing
			default:
				return string(asList(raw)), states
			}
		},
	}
}

// asList returns raw, one name or a list of names, as a list. raw is in the
// IR's canonical form, which writes a list of one name as that name between
// brackets.
func asList(raw ir.RawValue) ir.RawValue {
	if raw[0] != '"' {
		return raw
	}
	return slices.Concat(ir.RawValue("["), raw, ir.RawValue("]"))
}

// listedText decodes raw as one text or a list of texts, false for any other
// value or a list holding empty text. Empty text, an empty list and null state
// none. It is how a value naming one entity or several is read: replacements'
// names, and group paths.
func listedText(raw ir.RawValue) ([]string, bool) {
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
