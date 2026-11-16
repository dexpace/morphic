// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package promotion

import (
	"bytes"
	"strconv"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// RawStatement is the value one source of a target writes on a node, read
// whole: what it holds, which source wrote it, and where.
type RawStatement struct {
	Value  ir.RawValue
	Source ExtensionSource
	At     ir.Provenance
}

// StatementOf returns the value the first of p's sources of target writes on
// the node keeping own, read whole, and false where none writes one, for a
// step reading it once the document is whole (firstValue).
func (p Promoter) StatementOf(target ExtensionTarget, own ir.Unmodeled) (RawStatement, bool, []ir.Diagnostic) {
	return p.firstValue(target, own, nil)
}

// firstValue returns the value the first of p's sources of target writes on
// the node keeping own, by the policy's order, against which refuse, where
// set, gives no reason. A later source writing an equal value is silent, as
// firstStated's is; one writing another is reported at its own key and not
// read, as are a value refuse gives a reason against and a member of a key
// that is no object. Each report is info diag.UnpromotedExtension.
func (p Promoter) firstValue(target ExtensionTarget, own ir.Unmodeled, refuse func(ir.RawValue) string) (RawStatement, bool, []ir.Diagnostic) {
	outcome := "it does not fill " + string(target)
	var st RawStatement
	written := false
	var diags []ir.Diagnostic
	for _, src := range p.targets[target] {
		entry, declared := own[extensionKeyPrefix+src.Key]
		if !declared {
			continue
		}
		raw, at, _, located := src.value(entry)
		var why string
		if refuse != nil && located == states {
			why = refuse(raw)
		}
		switch {
		case located == misfit:
			diags = append(diags, Unpromoted(src.Key, at, "is not an object", "no member of it is read"))
		case located != states:
		case why != "":
			diags = append(diags, Unpromoted(src.String(), at, why, outcome))
		case !written:
			st, written = RawStatement{Value: raw, Source: src, At: at}, true
		case !bytes.Equal(raw, st.Value):
			diags = append(diags, Unpromoted(src.String(), at,
				"states another value than "+strconv.Quote(st.Source.String())+", which the policy reads first", outcome))
		}
	}
	return st, written, diags
}

// Unpromoted is the info diag.UnpromotedExtension reporting the key key at at,
// which states nothing usable, for why, so outcome.
func Unpromoted(key string, at ir.Provenance, why, outcome string) ir.Diagnostic {
	return diag.Newf(ir.SeverityInfo, diag.UnpromotedExtension, at, "extension %q %s, so %s", key, why, outcome)
}
