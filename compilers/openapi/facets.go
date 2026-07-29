package openapi

import (
	"strconv"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/ir"
)

// The site type and its kinds come from the resolver (resolve.go), which step 2
// introduced to produce them. Until now only site.Node had a production reader:
// Kind and Referent were built and never consulted. The readers below are what
// consult them.
//
// The referent stays the caller's to supply. Obtaining one requires reference
// resolution, which is walk work, and the two available resolutions are not
// interchangeable — site.Referent is exactly one hop, while the property path
// follows a $ref chain to its end. Passing the wrong one silently changes
// default and description semantics on a ref-to-ref chain.

// annotationSet is everything a site's annotations yield, before any of it is
// attached to a carrier.
//
// The readers produce a value; the caller decides what its carrier can hold. A
// Parameter has no XML field and a TypeCommon has no Required, so the shape of
// the write differs per carrier while the reading does not.
type annotationSet struct {
	Docs       ir.Docs
	Deprecated bool
	XML        *ir.XMLHints
	Examples   []ir.Example
	Unmodeled  ir.Unmodeled
}

// annotations reads every site-local annotation at st.
//
// This is the single call site the decomposition exists for. Not because it
// merges duplicate readers — the docs readers are genuinely distinct and stay
// distinct — but because the site-versus-referent choice is made here once
// instead of at each position that attaches annotations. Three attachment points
// previously made it separately and disagreed: one passed a referent, one passed
// nil because a declaration has none, and one passed nil because it never
// resolved the referent it had.
func annotations(st site, pointer string, srcIndex int) (annotationSet, []ir.Diagnostic) {
	var out annotationSet

	referent := st.Referent
	if st.Kind == siteDeclaration {
		referent = nil // a declaration has no target to fall back to
	}

	fillCarrierDocs(&out.Docs, st.Node, referent)
	out.Deprecated = effectiveDeprecated(st.Node, referent)
	out.XML = xmlHints(st.Node.GetXML())

	examples, exDiags := schemaExamplesAt(st.Node, pointer, srcIndex)
	out.Examples = examples

	ext, extDiags := extensionsFrom(st.Node.GetExtensions(), srcIndex, pointer)
	vOnly, vDiags := validationOnlyAt(st.Node, pointer, srcIndex)

	diags := make([]ir.Diagnostic, 0, len(exDiags)+len(extDiags)+len(vDiags))
	diags = append(diags, exDiags...)
	diags = append(diags, extDiags...)
	diags = append(diags, vDiags...)

	out.Unmodeled = mergePreserved(ext, vOnly)
	return out, diags
}

// schemaExamplesAt reads a schema's example and examples keywords.
//
// Site-only: an example written beside a $ref describes the position, never the
// referent, which is the class of annotation the $ref-sibling defect broke.
func schemaExamplesAt(s *oas3.Schema, pointer string, srcIndex int) ([]ir.Example, []ir.Diagnostic) {
	var out []ir.Example
	var diags []ir.Diagnostic
	if node := s.GetExample(); node != nil {
		out, diags = appendExampleAt(out, diags, node, srcIndex, pointer, "example")
	}
	for i, node := range s.GetExamples() {
		out, diags = appendExampleAt(out, diags, node, srcIndex, pointer, "examples", strconv.Itoa(i))
	}
	return out, diags
}

// appendExampleAt converts one example node, reporting an unconvertible value
// rather than dropping it.
func appendExampleAt(out []ir.Example, diags []ir.Diagnostic, node *yaml.Node,
	srcIndex int, base string, seg ...string,
) ([]ir.Example, []ir.Diagnostic) {
	at := base + ptr(seg...)
	v, err := valueFromNode(node)
	if err != nil {
		return out, append(diags, diagf(ir.SeverityWarning, codeDegradedConstruct,
			ir.Provenance{Source: srcIndex, Pointer: at}, "example: %s", err.Error()))
	}
	return append(out, ir.Example{Value: &v}), diags
}

// validationOnlyAt collects the §4.7 keywords a schema declares that the IR does
// not model, keeping each verbatim and announcing it.
//
// Site-only: these constrain the value at the position that wrote them.
func validationOnlyAt(s *oas3.Schema, pointer string, srcIndex int) (ir.Unmodeled, []ir.Diagnostic) {
	var p ir.Unmodeled
	var diags []ir.Diagnostic
	keep := func(key string, raw ir.RawValue, entryPtr, label string) {
		diags = append(diags, preserveKeywordInto(&p, key, raw, pointer, entryPtr, label, srcIndex)...)
	}

	if s.GetNot() != nil {
		keep("openapi:not", nodeToRaw(rawPropertyNode(s, "not")), pointer+ptr("not"), "not")
	}
	if ite := ifThenElseRaw(s); ite != nil {
		keep("openapi:if-then-else", ite, pointer, "if/then/else")
	}
	if ds := s.GetDependentSchemas(); ds != nil && ds.Len() > 0 {
		keep("openapi:dependentSchemas", nodeToRaw(rawPropertyNode(s, "dependentSchemas")),
			pointer+ptr("dependentSchemas"), "dependentSchemas")
	}
	if s.GetPropertyNames() != nil {
		keep("openapi:propertyNames", nodeToRaw(rawPropertyNode(s, "propertyNames")),
			pointer+ptr("propertyNames"), "propertyNames")
	}
	if craw := containsRaw(s); craw != nil {
		keep("openapi:contains", craw, pointer, "contains")
	}
	if u := unevaluatedRaw(s); u != nil {
		keep("openapi:unevaluated", u, pointer, "unevaluated")
	}
	return p, diags
}

// preserveInto records raw under key in p, or records nothing when there are no
// bytes to record.
//
// len rather than a nil comparison: nil and a zero-length slice are distinct
// states, and an empty payload is the worse of the two. It preserves no
// construct, and json.Marshal rejects it for the whole document while naming
// json.RawMessage rather than the entry that carried it.
func preserveInto(p *ir.Unmodeled, key string, raw ir.RawValue,
	reason ir.UnmodeledReason, pointer string, srcIndex int,
) {
	if len(raw) == 0 {
		return
	}
	if *p == nil {
		*p = ir.Unmodeled{}
	}
	(*p)[key] = ir.UnmodeledEntry{
		Reason:     reason,
		Value:      raw,
		Provenance: ir.Provenance{Source: srcIndex, Pointer: pointer},
	}
}

// preserveKeywordInto records a validation-only keyword and returns the one
// diagnostic announcing it. An absent or unconvertible payload records nothing
// and announces nothing.
func preserveKeywordInto(p *ir.Unmodeled, key string, raw ir.RawValue,
	declPtr, entryPtr, label string, srcIndex int,
) []ir.Diagnostic {
	if len(raw) == 0 {
		return nil
	}
	preserveInto(p, key, raw, ir.ReasonValidationOnly, entryPtr, srcIndex)
	return []ir.Diagnostic{diagf(ir.SeverityInfo, codeValidationOnlyKeyword,
		ir.Provenance{Source: srcIndex, Pointer: declPtr},
		"validation-only keyword %q kept verbatim under Unmodeled", label)}
}
