// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"strconv"
	"strings"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// nullBranch reports whether the union branch js is the union's null, lifted
// onto the reference to the union rather than made a variant
// (docs/compilers/openapi.md#union-null-variant): written inline and of null
// alone (NullAlone), so it would lower on its own to the Enum with no member
// behind a nullable reference that the reference's bit says. A $ref branch
// names a node the source declares, and stays the variant naming it.
func nullBranch(c lowering.Ctx, js *oas3.JSONSchema[oas3.Referenceable]) bool {
	return inlineBranch(js) && NullAlone(c, js.GetSchema())
}

// inlineBranch reports whether js is a schema written in place: no boolean,
// and no $ref (isRefBranch).
func inlineBranch(js *oas3.JSONSchema[oas3.Referenceable]) bool {
	return js != nil && js.IsSchema() && !isRefBranch(js)
}

// liftedNull is a null branch (nullBranch) of a union, where it is written,
// and the Unmodeled key that keeps it, `openapi:<combinator>/<index>`: its path
// from the schema writing the union, as an allOf branch's is.
type liftedNull struct {
	schema *oas3.Schema
	at     jsontext.Pointer
	key    string
}

// liftedNulls returns the null branches of the union unionBranches elects of
// s, written at pointer, in source order.
func liftedNulls(c lowering.Ctx, s *oas3.Schema, pointer jsontext.Pointer) []liftedNull {
	branches, key, _ := unionBranches(s)
	var out []liftedNull
	for i, b := range branches {
		if !nullBranch(c, b) {
			continue
		}
		index := strconv.Itoa(i)
		out = append(out, liftedNull{schema: b.GetSchema(), at: pointer + ids.Ptr(key, index), key: "openapi:" + key + "/" + index})
	}
	return out
}

// saysTheNull reports whether key, written on the null branch at, says only
// the null the reference to its union holds: a type, an enum or a const, and a
// nullable where it is read, by the dialect defining it or by the
// nullable-inference policy (docs/compilers/openapi.md#nullable). A nullable
// neither reads is a keyword its dialect does not define, kept as one is.
func saysTheNull(c lowering.Ctx, at jsontext.Pointer, key string) bool {
	switch key {
	case "type", "enum", "const":
		return true
	case "nullable":
		return c.NullableInference.Honours() || lowering.SchemaRulesAt(c, at).Defines(key)
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
func nullBranchKeeps(c lowering.Ctx, s *oas3.Schema, pointer jsontext.Pointer) []keptNullBranch {
	var out []keptNullBranch
	for _, b := range liftedNulls(c, s, pointer) {
		var written []string
		for _, key := range rawMappingKeys(b.schema.GetRootNode()) {
			if !saysTheNull(c, b.at, key) && !annotation.WritesKeywordLeftOut(b.schema, key) {
				written = append(written, key)
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
		stored, keptDiags := annotation.PreserveNodeInto(p, b.key, b.schema.GetRootNode(), ir.ReasonNoIRHome, c.Place().At(b.at))
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
func liftedNullFindings(c lowering.Ctx, s *oas3.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	lifted := liftedNulls(c, s, pointer)
	diags := make([]ir.Diagnostic, 0, len(lifted))
	for _, b := range lifted {
		n := newNulls(c)
		n.schema(b.schema, siteAt(b.at))
		diags = append(diags, n.findings(c)...)
		diags = append(diags, enumRepeats(c, readEnum(b.schema.GetEnum()), b.at)...)
	}
	return diags
}
