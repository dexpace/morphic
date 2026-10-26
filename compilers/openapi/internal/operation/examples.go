package operation

import (
	"encoding/json/jsontext"
	"slices"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/ir"
)

// exampleList lowers a single example node and a plural example map into value
// examples, in source order. An unconvertible node is skipped with a warning
// diagnostic rather than silently. The singular `example` keyword is a bare
// value with nowhere to hang a name or summary, so it lowers to a value alone.
func exampleList(c lowering.Ctx, single *yaml.Node, plural *sequencedmap.Map[string, *soa.ReferencedExample], pointer jsontext.Pointer) ([]ir.Example, []ir.Diagnostic) {
	var out []ir.Example
	var diags []ir.Diagnostic
	if single != nil {
		var exDiags []ir.Diagnostic
		out, exDiags = annotation.AppendExample(out, ir.Example{}, single, c.ProvenanceAt, pointer, "example")
		diags = append(diags, exDiags...)
	}
	if plural == nil {
		return out, diags
	}
	for name, re := range plural.All() {
		var pluralDiags []ir.Diagnostic
		out, pluralDiags = appendPluralExample(c, out, re, pointer, name)
		diags = append(diags, pluralDiags...)
	}
	return out, diags
}

// appendPluralExample lowers one named entry of a plural `examples` map with the
// annotations that surround its value: the map key names the example, and its
// summary and description travel with it. An entry written as a $ref holds no
// value of its own — the value lives in the referenced component — so its
// diagnostic is stamped at the reference site rather than at a `value` node this
// entry never had; an inline entry is stamped at its own `value` or
// `dataValue`. Only this hop
// is de-referenced: an enclosing $ref'd response or parameter is already
// flattened into pointer.
func appendPluralExample(c lowering.Ctx, out []ir.Example, re *soa.ReferencedExample, pointer jsontext.Pointer, name string) ([]ir.Example, []ir.Diagnostic) {
	// The declaration pointer, not the entry's, is where an Example Object's own
	// keywords are written: a $ref entry holds none of them. ir.Example carries an
	// Unmodeled map, so neither the object's x-* nor its undeclared keys need a
	// scope.
	ex, dc, decl := lowering.Declared[soa.Example](c, re, pointer+ids.Ptr("examples", name))
	if ex == nil {
		return out, nil
	}
	ext, diags := annotation.ExtensionsFrom(ex.GetExtensions(), dc.ProvenanceAt, decl)
	diags = append(diags, annotation.UnknownKeysIn(&ext, ex, dc.ProvenanceAt, decl)...)
	// A summary or description beside a $ref of the entry's chain documents this
	// use of the example (GitHub #610).
	docs := resolve.UseDocs[soa.Example](re, ir.Docs{Summary: ex.GetSummary(), Description: ex.GetDescription()})
	proto := ir.Example{
		Name:        name,
		Summary:     docs.Summary,
		Description: docs.Description,
		ExternalURL: ex.GetExternalValue(),
		Unmodeled:   ext,
	}
	out, exDiags := appendExampleValue(c, dc, out, proto, ex, re, pointer, decl, name)
	return out, append(diags, exDiags...)
}

// appendExampleValue appends the entry under the annotations proto already
// carries. Its data lowers into Value from the spelling it was written in,
// `value` or 3.2's `dataValue` (GitHub #612). A spelling ir.Example has no field
// for is kept verbatim at decl, the entry's declaration, rather than dropped:
// 3.2's `serializedValue` whatever it is written beside, and a `dataValue`
// written beside `value`, which the rule table reports. An entry with no value in
// any spelling is an empty stub, dropped with a warning.
func appendExampleValue(c, dc lowering.Ctx, out []ir.Example, proto ir.Example, ex *soa.Example,
	re *soa.ReferencedExample, pointer, decl jsontext.Pointer, name string,
) ([]ir.Example, []ir.Diagnostic) {
	serialized, diags := keepExampleSpelling(dc, &proto.Unmodeled, ex.GetCore().SerializedValue.ValueNode,
		"serializedValue", ir.ReasonNoIRHome, decl, "serializedValue has no ir.Example home")
	value, data := ex.GetValue(), ex.GetDataValue()
	switch {
	case value != nil:
		if data != nil {
			_, dataDiags := keepExampleSpelling(dc, &proto.Unmodeled, data, "dataValue",
				ir.ReasonDegradedLowering, decl, "dataValue is written beside value, which lowered")
			diags = append(diags, dataDiags...)
		}
		out, valueDiags := appendExampleData(c, out, proto, re, value, pointer, name, "value")
		return out, append(diags, valueDiags...)
	case data != nil:
		out, dataDiags := appendExampleData(c, out, proto, re, data, pointer, name, "dataValue")
		return out, append(diags, dataDiags...)
	case serialized || proto.ExternalURL != "":
		return append(out, proto), diags
	}
	return out, append(diags, c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct,
		pointer+ids.Ptr("examples", name), "example declares no value"))
}

// appendExampleData lowers one example node under keyword — the spelling it was
// written with — stamping the failure pointer where the value is written: at the
// reference site for a $ref entry, which holds no node of its own, and at its own
// keyword for an inline one.
func appendExampleData(c lowering.Ctx, out []ir.Example, proto ir.Example,
	re *soa.ReferencedExample, node *yaml.Node, pointer jsontext.Pointer, name, keyword string,
) ([]ir.Example, []ir.Diagnostic) {
	if re.IsReference() {
		return annotation.AppendExample(out, proto, node, c.ProvenanceAt, pointer, "examples", name)
	}
	return annotation.AppendExample(out, proto, node, c.ProvenanceAt, pointer, "examples", name, keyword)
}

// keepExampleSpelling keeps the node an Example Object writes under keyword
// verbatim on u, at the keyword's own pointer beneath decl, and announces it
// with why. node is the parser's own reading of the field, so a value merged in
// through `<<` is kept like one written directly. It reports whether anything
// was kept.
func keepExampleSpelling(c lowering.Ctx, u *ir.Unmodeled, node *yaml.Node, keyword string,
	reason ir.UnmodeledReason, decl jsontext.Pointer, why string,
) (bool, []ir.Diagnostic) {
	at := decl + ids.Ptr(keyword)
	kept, diags := annotation.PreserveNodeInto(u, annotation.EntryKey("", keyword), node, reason, c.ProvenanceAt(at))
	if !kept {
		return false, diags
	}
	return true, append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
		"%s; kept verbatim under Unmodeled", why))
}

// positionExamples returns the examples a parameter or header writes for
// itself: its own, then those of the media type its content spelling elected.
// Together they replace the schema's, as OpenAPI says a position's example
// overrides its schema's, but neither replaces the other: OpenAPI orders them
// nowhere, and keeping one would drop the other in silence (GitHub #611).
func positionExamples(own, media []ir.Example) []ir.Example {
	return slices.Concat(own, media)
}
