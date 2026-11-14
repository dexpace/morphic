// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

import (
	"strconv"

	"github.com/dexpace/morphic/ir"
)

// TargetParamChoices fills ir.Operation.ParamChoices from a key on the
// operation naming alternatives among its parameters
// (docs/compilers/openapi.md#mutually-exclusive-parameters).
const TargetParamChoices ExtensionTarget = "operation.paramChoices"

// RawStatement is the value one source of a target writes on a node, read
// whole: what it holds, which source wrote it, and where.
type RawStatement struct {
	Value  ir.RawValue
	Source ExtensionSource
	At     ir.Provenance
}

// ParamChoiceStatement returns what the first of p's sources of
// TargetParamChoices that own, an operation's keys, writes, and false where
// it writes none. A later source's key written too is reported at itself and
// not read, whatever it holds, as only one statement of the choices is read.
func (p Promoter) ParamChoiceStatement(own ir.Unmodeled) (RawStatement, bool, []ir.Diagnostic) {
	var st RawStatement
	written := false
	var diags []ir.Diagnostic
	for _, src := range p.targets[TargetParamChoices] {
		entry, declared := own[extensionKeyPrefix+src.Key]
		if !declared {
			continue
		}
		raw, at, _, located := src.value(entry)
		switch {
		case written:
			diags = append(diags, Unpromoted(src.String(), at,
				"is written beside "+strconv.Quote(st.Source.String())+", which the policy reads first",
				"no choice is filled from it"))
		case located == states:
			st, written = RawStatement{Value: raw, Source: src, At: at}, true
		}
	}
	return st, written, diags
}

// ShapeOf names the kind of raw, a value in the IR's canonical form, as a
// report spells it, for a step reading a promotion's value once the document
// is whole.
func ShapeOf(raw ir.RawValue) string { return shapeOf(raw) }
