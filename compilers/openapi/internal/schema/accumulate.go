package schema

import (
	"github.com/speakeasy-api/openapi/extensions"
	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// The lowering's shared plumbing: how a diagnostic is stamped and recorded, and
// how a construct the IR keeps verbatim is written down.
//
// It lived in schema.go, which made every file that reports a diagnostic look
// like it depended on schema lowering. Most did not: with it moved, operations
// reaches nothing in schema.go, and content and parameters reach one symbol each
// — FillPropertyDetail and LoweredToOwnNode. Naming them rather than counting
// them is deliberate: the count was two when this was written and went stale
// silently when residueKeywords was deleted. Those are real schema lowering and
// are what the upper layer still waits on; the rest of the apparent coupling was
// this, filed in the wrong place.

// AppendExample converts node into proto's value and appends the result to out;
// an unconvertible node is skipped and yields a warning diagnostic for the
// caller to record, rather than being silently dropped — an example is an annotation, not a structural hole, so losing it is
// fine as long as it isn't silent. proto carries the annotations that surround
// the value (name, summary, description); base and seg locate the node, joined
// into a pointer only on the failure path, so an example that converts builds no
// pointer string at all. Shared by every example site: schema (schemaExamples),
// media type, header, and parameter (exampleList).
func AppendExample(c lowering.Ctx, out []ir.Example, proto ir.Example, node *yaml.Node,
	base string, seg ...string,
) ([]ir.Example, []ir.Diagnostic) {
	v, err := value.FromNode(node)
	if err != nil {
		return out, []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct,
			base+ids.Ptr(seg...), "example: %s", err.Error())}
	}
	proto.Value = &v
	return append(out, proto), nil
}

// Preserve records raw under key in *p with why it was kept and where it was
// written, allocating the map on first write. An absent or unconvertible
// payload records nothing, so no caller needs a nil guard of its own.
func Preserve(c lowering.Ctx, p *ir.Unmodeled, key string, raw ir.RawValue,
	reason ir.UnmodeledReason, pointer string,
) {
	annotation.PreserveInto(p, key, raw, reason, pointer, c.SrcIndex)
}

// PreserveNode records the construct written at node under key in *p, reporting
// one that could not be converted at all. It returns whether an entry was
// written, so a caller announces only what it actually kept (GitHub #144).
func PreserveNode(c lowering.Ctx, p *ir.Unmodeled, key string, node *yaml.Node,
	reason ir.UnmodeledReason, pointer string,
) (bool, []ir.Diagnostic) {
	return annotation.PreserveNodeInto(p, key, node, reason, pointer, c.SrcIndex)
}

// PreserveSchemaKeyword records the top-level keyword s writes under key. It is
// PreserveNode addressed by keyword rather than by node, which is how all but a
// handful of preservation sites reach their payload.
func PreserveSchemaKeyword(c lowering.Ctx, p *ir.Unmodeled, s *oas3.Schema, keyword string,
	reason ir.UnmodeledReason, pointer string,
) (bool, []ir.Diagnostic) {
	return PreserveNode(c, p, "openapi:"+keyword, annotation.RawPropertyNode(s, keyword), reason, pointer)
}

// preserveKeyword records a validation-only keyword's raw payload under key in
// p and returns the one info diagnostic naming it at declPtr, the schema that
// wrote it. An absent or unconvertible payload records nothing and returns
// nothing.
//
// entryPtr locates the entry itself, which is what a validation emitter reports
// against: the keyword's own node where the source writes the entry as one
// keyword, and declPtr where a §4.7 entry combines several keywords into one
// synthesized object that no single node addresses.
func preserveKeyword(c lowering.Ctx, p *ir.Unmodeled, key string, raw ir.RawValue,
	declPtr, entryPtr, label string,
) []ir.Diagnostic {
	return annotation.PreserveKeywordInto(p, key, raw, declPtr, entryPtr, label, c.SrcIndex)
}

// lowerArray hoists an array schema as a Tuple when prefixItems is present, else
// a List over its item schema with its collection constraints.
func lowerArray(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, depth int, s *oas3.Schema, pointer, hint string) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		if prefix := s.GetPrefixItems(); len(prefix) > 0 {
			t, tupleDiags := buildTuple(c, ts, anchors, depth, s, common, pointer, hint, prefix)
			diags = append(diags, tupleDiags...)
			return t
		}
		elem, elemDiags := Ref(c, ts, anchors, depth, s.GetItems(), pointer+ids.Ptr("items"), hint+"_item")
		diags = append(diags, elemDiags...)
		return &ir.List{
			TypeCommon:  common,
			Elem:        elem,
			Constraints: listConstraints(s),
		}
	})
	return id, diags
}

// ExtensionsOf lowers ext's x-* extensions into namespaced Unmodeled, returning
// the entries and any serialization-failure diagnostics separately. It is named
// for what it returns rather than for the construct, because this package also
// imports the extensions library.
//
// The two returns are independent on purpose, and a caller must record the
// diagnostics whether or not it keeps the entries: gating the append behind the
// same "len(ext) > 0" that guards the assignment drops every warning on an
// object whose extensions all failed to serialize — exactly when the result is
// empty. TestOperation_UnserializableExtensionStillWarns and its security-scheme
// twin hold two of the callers to that.
func ExtensionsOf(c lowering.Ctx, ext *extensions.Extensions, owner string) (ir.Unmodeled, []ir.Diagnostic) {
	return annotation.ExtensionsFrom(ext, c.SrcIndex, owner)
}
