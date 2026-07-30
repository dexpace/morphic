package openapi

import (
	"github.com/speakeasy-api/openapi/extensions"
	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// The lowering's shared plumbing: how a diagnostic is stamped and recorded, and
// how a construct the IR keeps verbatim is written down.
//
// It lived in schema.go, which made every file that reports a diagnostic look
// like it depended on schema lowering. Most did not: with it moved, operations
// reaches nothing in schema.go at all, content reaches one symbol and parameters
// two. Those three are real schema lowering and are what the upper layer still
// waits on; the rest of the apparent coupling was this, filed in the wrong
// place.

// appendExample converts node into proto's value and appends the result to out;
// an unconvertible node is skipped with a warning diagnostic rather than silently
// dropped — an example is an annotation, not a structural hole, so losing it is
// fine as long as it isn't silent. proto carries the annotations that surround
// the value (name, summary, description); base and seg locate the node, joined
// into a pointer only on the failure path, so an example that converts builds no
// pointer string at all. Shared by every example site: schema (schemaExamples),
// media type, header, and parameter (exampleList).
func (l *lowerer) appendExample(out []ir.Example, proto ir.Example, node *yaml.Node, base string, seg ...string) []ir.Example {
	v, err := value.FromNode(node)
	if err != nil {
		l.diag(ir.SeverityWarning, diag.DegradedConstruct, base+ids.Ptr(seg...), "example: %s", err.Error())
		return out
	}
	proto.Value = &v
	return append(out, proto)
}

// preserve records raw under key in *p with why it was kept and where it was
// written, allocating the map on first write. An absent or unconvertible
// payload records nothing, so no caller needs a nil guard of its own.
func (l *lowerer) preserve(p *ir.Unmodeled, key string, raw ir.RawValue, reason ir.UnmodeledReason, pointer string) {
	annotation.PreserveInto(p, key, raw, reason, pointer, l.ctx.SrcIndex)
}

// preserveNode records the construct written at node under key in *p, reporting
// one that could not be converted at all. It returns whether an entry was
// written, so a caller announces only what it actually kept (GitHub #144).
func (l *lowerer) preserveNode(p *ir.Unmodeled, key string, node *yaml.Node,
	reason ir.UnmodeledReason, pointer string,
) bool {
	ok, diags := annotation.PreserveNodeInto(p, key, node, reason, pointer, l.ctx.SrcIndex)
	l.diags.AppendAll(diags)
	return ok
}

// preserveSchemaKeyword records the top-level keyword s writes under key. It is
// preserveNode addressed by keyword rather than by node, which is how all but a
// handful of preservation sites reach their payload.
func (l *lowerer) preserveSchemaKeyword(p *ir.Unmodeled, s *oas3.Schema, keyword string,
	reason ir.UnmodeledReason, pointer string,
) bool {
	return l.preserveNode(p, "openapi:"+keyword, annotation.RawPropertyNode(s, keyword), reason, pointer)
}

// preserveKeyword records a validation-only keyword's raw payload under key in
// p and emits one info diagnostic naming it at declPtr, the schema that wrote
// it. An absent or unconvertible payload records nothing and reports nothing.
//
// entryPtr locates the entry itself, which is what a validation emitter reports
// against: the keyword's own node where the source writes the entry as one
// keyword, and declPtr where a §4.7 entry combines several keywords into one
// synthesized object that no single node addresses.
func (l *lowerer) preserveKeyword(p *ir.Unmodeled, key string, raw ir.RawValue, declPtr, entryPtr, label string) {
	l.diags.AppendAll(annotation.PreserveKeywordInto(p, key, raw, declPtr, entryPtr, label, l.ctx.SrcIndex))
}

// diag records one diagnostic at pointer with the given severity and code. It is
// the accumulating form of lowerCtx.diagAt, for the lowerings that still hold an
// accumulator; a lowering that returns its diagnostics calls diagAt and appends
// to what it returns. Neither builds a Provenance — provenanceAt does, once.
func (l *lowerer) diag(sev ir.Severity, code, pointer, format string, args ...any) {
	l.appendDiag(l.ctx.diagAt(sev, code, pointer, format, args...))
}

// appendConstraintDiags stamps constraint diagnostics with pointer's provenance,
// recording them at most once per pointer: a sub-schema reached from both its
// owning property and a $ref that hoists it is read twice, but a malformed bound
// must be reported only once. Constraint data still lands on both nodes; only
// the diagnostic is de-duplicated.
func (l *lowerer) appendConstraintDiags(diags []ir.Diagnostic, pointer string) {
	if l.diagnosedConstraints[pointer] {
		return
	}
	l.diagnosedConstraints[pointer] = true
	for i := range diags {
		diags[i].Provenance = l.ctx.provenanceAt(pointer)
		l.appendDiag(diags[i])
	}
}

// appendDiag records d unless one identical to it — same severity, code,
// provenance and message — was already recorded. It is the single append point
// for lowering diagnostics, so a shared declaration reported from N use sites
// yields one diagnostic rather than N indistinguishable copies.
func (l *lowerer) appendDiag(d ir.Diagnostic) { l.diags.Append(d) }

// lowerArray hoists an array schema as a Tuple when prefixItems is present, else
// a List over its item schema with its collection constraints.
func (l *lowerer) lowerArray(s *oas3.Schema, pointer, hint string) ir.TypeID {
	return internNode(l.ctx, l.types, pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		if prefix := s.GetPrefixItems(); len(prefix) > 0 {
			return l.buildTuple(s, common, pointer, hint, prefix)
		}
		list := &ir.List{
			TypeCommon:  common,
			Elem:        l.schemaRef(s.GetItems(), pointer+ids.Ptr("items"), hint+"_item"),
			Constraints: listConstraints(s),
		}
		return list
	})

}

// extensions lowers ext's x-* extensions into namespaced Unmodeled, recording
// any serialization-failure diagnostics unconditionally even when the result is
// empty. Every lowering site should call this rather than
// annotation.ExtensionsFrom directly: gating the diagnostic append behind the
// same "len(ext) > 0" that guards the assignment would drop every warning on an
// object whose extensions all failed to serialize — exactly when the result is
// empty.
func (l *lowerer) extensions(ext *extensions.Extensions, owner string) ir.Unmodeled {
	out, diags := annotation.ExtensionsFrom(ext, l.ctx.SrcIndex, owner)
	l.diags.AppendAll(diags)
	return out
}
