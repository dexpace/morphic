// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// nullBranch reports whether the union branch js is the union's null, lifted
// onto the reference to the union rather than made a variant
// (docs/compilers/openapi.md#union-null-variant): written inline and of null
// alone (NullAlone), so it would lower on its own to the Enum with no member
// behind a nullable reference that the reference's bit says. A $ref branch
// names a node the source declares, and stays the variant naming it.
func nullBranch(c lowering.Ctx, js *statement.Statement) bool {
	return inlineBranch(js) && NullAlone(c, js.Schema)
}

// variantsNull is what s's oneOf and anyOf say of null where the IR holds a
// union's null on its variants as on the reference to it, the two conjoined:
// each admits null where any branch does and keeps it out where every branch
// does, each read by n, whose budget bounds them, so a branch left unread
// keeps nothing out. That holds of a union however it lowers, so none is asked
// whether it is distributed (unionNullReading), and the read starts no
// classification.
func (n *nulls) variantsNull(s *statement.Schema) nullReading {
	choice := s.ChoiceKeywords()
	return foldNullReadings(n.branchesNull(choice.OneOf), n.branchesNull(choice.AnyOf))
}

// branchesNull is what one union's branches say of null, under each reading:
// admitted where a branch admits it, forbidden where every branch forbids it,
// and silent where it writes none.
func (n *nulls) branchesNull(branches []statement.Subschema) nullReading {
	if len(branches) == 0 {
		return nullReadingAlikeUnderBothPolicies(nullUnconstrained)
	}
	out := nullReadingAlikeUnderBothPolicies(nullForbidden)
	for _, b := range branches {
		r := n.conjunct(n.c.Statements().Subschema(b), nullSite{})
		out.declared = disjoinNullVerdicts(out.declared, r.declared)
		out.lenient = disjoinNullVerdicts(out.lenient, r.lenient)
	}
	return out
}

// disjoinNullVerdicts is what two branches of a union say of null together:
// one admitting it decides, and the union keeps it out only where both do.
func disjoinNullVerdicts(a, b nullDeclaration) nullDeclaration {
	switch {
	case a == nullAdmitted || b == nullAdmitted:
		return nullAdmitted
	case a == nullForbidden && b == nullForbidden:
		return nullForbidden
	default:
		return nullUnconstrained
	}
}

// mayAdmitNull reports whether b admits null wherever the IR holds it, on a
// variant of a union it is or reaches as on its own keywords, under the
// reading c's policy selects. holder is a schema whose own union the read
// leaves out, nil for none. A read past the walk's budget may, unless what it
// read keeps null out, which no conjunct left unread lets back in.
func mayAdmitNull(c lowering.Ctx, b *statement.Statement, holder *statement.Schema) bool {
	n := newNulls(c)
	n.variants, n.rest = true, holder
	r := n.conjunct(b, nullSite{})
	return r.admits(c) || n.conjuncts.Exhausted() && !r.forbids(c)
}

// inlineBranch reports whether js is a schema written in place: no boolean,
// and no reference site (typed.Bindings.RefSite).
func inlineBranch(js *statement.Statement) bool {
	return js != nil && stated(js.Schema) && js.Ref == nil
}

// liftedNull is a null branch (nullBranch) of a union, where it is written,
// and the Unmodeled key that keeps it, `openapi:<combinator>/<index>`: its path
// from the schema writing the union, as an allOf branch's is.
type liftedNull struct {
	schema *statement.Schema
	at     jsontext.Pointer
	key    string
}

// liftedNulls returns the null branches of the union unionBranches elects of
// s at position, s written at pointer, in source order.
func liftedNulls(c lowering.Ctx, position, s *statement.Schema, pointer jsontext.Pointer) []liftedNull {
	branches, key, _ := unionBranches(c, position, s)
	var out []liftedNull
	for i, b := range branches {
		st := c.Statements().Subschema(b)
		if !nullBranch(c, st) {
			continue
		}
		index := strconv.Itoa(i)
		out = append(out, liftedNull{schema: st.Schema, at: pointer + ids.Ptr(key, index), key: "openapi:" + key + "/" + index})
	}
	return out
}

// saysTheNull reports whether key, written on the null branch b, says only
// the null the reference to its union holds: a type, an enum or a const, and a
// nullable where it is read, by the dialect defining it or by the
// nullable-inference policy (docs/compilers/openapi.md#nullable). A nullable
// neither reads is a keyword its dialect does not define, kept as one is.
func saysTheNull(c lowering.Ctx, b liftedNull, key string) bool {
	switch key {
	case "type", "enum", "const":
		return true
	case "nullable":
		return c.Rules().At(c.Position(b.at)).Defines(key) || c.NullableInference.Honours() && nullableTrue(b.schema)
	default:
		return false
	}
}

// keptNullBranch is a null branch that writes more than its null, with the
// keys that do.
type keptNullBranch struct {
	liftedNull
	written []string
}

// nullBranchKeeps returns the null branches of s's union, written at pointer,
// that write a key beside those saying the null (saysTheNull): an annotation,
// an extension or any other keyword, but a `null` written for one, which is
// that keyword left out (annotation.WritesKeywordLeftOut). A branch is no
// variant, so nothing else keeps what it says.
func nullBranchKeeps(c lowering.Ctx, position, s *statement.Schema, pointer jsontext.Pointer) []keptNullBranch {
	var out []keptNullBranch
	for _, b := range liftedNulls(c, position, s, pointer) {
		var written []string
		for _, k := range b.schema.Keys {
			if !saysTheNull(c, b, k.Name) && !annotation.WritesKeywordLeftOut(b.schema, k.Name) {
				written = append(written, k.Name)
			}
		}
		if len(written) > 0 {
			out = append(out, keptNullBranch{liftedNull: b, written: written})
		}
	}
	return out
}

// recordNullBranches keeps each of kept whole in p under its key, as no_ir_home:
// nothing is degraded, the null being typed exactly, and only what else the
// branch says of that alternative has no field. One stored is reported at its
// branch, and one the keeper cannot convert is reported by it instead.
func recordNullBranches(c lowering.Ctx, p *ir.Unmodeled, kept []keptNullBranch) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, b := range kept {
		stored, keptDiags := annotation.PreserveNodeInto(p, b.key, b.schema.Node, ir.ReasonNoIRHome, c.Place().At(b.at))
		diags = append(diags, keptDiags...)
		if stored {
			diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, b.at,
				"this branch admits null alone, which the reference to this position holds, so it is no variant; "+
					"the branch (%s) is kept verbatim under Unmodeled", strings.Join(b.written, ", ")))
		}
	}
	return diags
}

// liftedNullFindings returns what reading each null branch of s's union,
// written at pointer, reports at the branch, as its own lowering reports it
// where it is a variant: a null the nullable-inference policy reads in
// (nulls.findings), and an enum entry repeating an earlier one (enumRepeats),
// which the document writes whichever path takes the branch.
func liftedNullFindings(c lowering.Ctx, position, s *statement.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	lifted := liftedNulls(c, position, s, pointer)
	diags := make([]ir.Diagnostic, 0, len(lifted))
	for _, b := range lifted {
		n := newNulls(c)
		n.schema(b.schema, siteAt(b.at))
		diags = append(diags, n.findings(c)...)
		diags = append(diags, enumRepeats(c, readEnum(b.schema.Enum()), b.at)...)
	}
	return diags
}
