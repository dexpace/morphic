package openapi

import (
	"strconv"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
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
	kept, keptDiags := unmodeledAt(st.Node, pointer, srcIndex)

	diags := make([]ir.Diagnostic, 0, len(exDiags)+len(extDiags)+len(keptDiags))
	diags = append(diags, exDiags...)
	diags = append(diags, extDiags...)
	diags = append(diags, keptDiags...)

	out.Unmodeled = mergeUnmodeled(ext, kept)
	return out, diags
}

// unmodeledAt collects every keyword a site declares that the IR keeps verbatim
// instead of modelling, each under the reason that says which of those it is
// (§12): validation logic the IR draws a boundary against (§4.7), data with no IR
// field yet, and JSON Schema resource/dialect metadata the IR excludes on
// purpose.
func unmodeledAt(s *oas3.Schema, pointer string, srcIndex int) (ir.Unmodeled, []ir.Diagnostic) {
	vOnly, vDiags := validationOnlyAt(s, pointer, srcIndex)
	noHome, nhDiags := noIRHomeAt(s, pointer, srcIndex)
	dialect, dDiags := dialectAt(s, pointer, srcIndex)

	diags := make([]ir.Diagnostic, 0, len(vDiags)+len(nhDiags)+len(dDiags))
	diags = append(diags, vDiags...)
	diags = append(diags, nhDiags...)
	diags = append(diags, dDiags...)

	return mergeUnmodeled(mergeUnmodeled(vOnly, noHome), dialect), diags
}

// noIRHomeAt collects the keywords a schema declares that describe real data yet
// have no field at any IR position. Unlike the §4.7 family these are gaps
// expected to close rather than a boundary the IR draws, which is what
// ReasonNoIRHome says and ReasonValidationOnly would not (§12).
//
// Site-only: contentSchema describes the value at the position that wrote it.
func noIRHomeAt(s *oas3.Schema, pointer string, srcIndex int) (ir.Unmodeled, []ir.Diagnostic) {
	if s.GetContentSchema() == nil {
		return nil, nil
	}
	at := pointer + ptr("contentSchema")
	var p ir.Unmodeled
	kept, diags := preserveNodeInto(&p, "openapi:contentSchema", rawPropertyNode(s, "contentSchema"),
		ir.ReasonNoIRHome, at, srcIndex)
	if !kept {
		return nil, diags
	}
	return p, []ir.Diagnostic{diag.Newf(ir.SeverityInfo, diag.DegradedConstruct,
		ir.Provenance{Source: srcIndex, Pointer: at},
		"contentSchema is the shape of the decoded content and no IR position has a field "+
			"for it; kept verbatim under Unmodeled")}
}

// dialectKeywords are the JSON Schema resource and dialect keywords the IR
// excludes on purpose. It identifies every type by a synthetic ID derived from
// its source pointer rather than by `$id` (ir-design §3), and describes one API
// surface rather than a JSON Schema resource graph, so it has no dialect axis for
// `$schema`/`$vocabulary` to land on and none is coming — ReasonOutOfScope rather
// than ReasonNoIRHome (§12).
//
// `$id` is kept, not honoured: reference resolution addresses same-document JSON
// pointers, never an `$id` base URI.
var dialectKeywords = []string{"$id", "$schema", "$vocabulary"}

// dialectAt keeps each dialect keyword s declares verbatim and announces it, so
// the exclusion is visible in the output rather than only in this comment.
func dialectAt(s *oas3.Schema, pointer string, srcIndex int) (ir.Unmodeled, []ir.Diagnostic) {
	var p ir.Unmodeled
	var diags []ir.Diagnostic
	for _, keyword := range dialectKeywords {
		at := pointer + ptr(keyword)
		kept, keptDiags := preserveNodeInto(&p, "openapi:"+keyword, rawPropertyNode(s, keyword),
			ir.ReasonOutOfScope, at, srcIndex)
		diags = append(diags, keptDiags...)
		if !kept {
			continue
		}
		diags = append(diags, diag.Newf(ir.SeverityInfo, diag.DegradedConstruct,
			ir.Provenance{Source: srcIndex, Pointer: at},
			"%s identifies or configures a JSON Schema resource rather than describing data; "+
				"the IR models no such axis, so it is kept verbatim under Unmodeled and is not "+
				"honoured for reference resolution", keyword))
	}
	return p, diags
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
		return out, append(diags, diag.Newf(ir.SeverityWarning, diag.DegradedConstruct,
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

	// keep takes the conversion's error alongside its payload so a keyword that
	// could not be converted is reported rather than passed on as an absent one —
	// the two were indistinguishable here before GitHub #144.
	keep := func(key string, raw ir.RawValue, err error, entryPtr, label string) {
		if err != nil {
			diags = append(diags, unpreservableDiag(key, entryPtr, srcIndex, err))
			return
		}
		diags = append(diags, preserveKeywordInto(&p, key, raw, pointer, entryPtr, label, srcIndex)...)
	}
	// A keyword whose entry is its own node needs no label of its own: the
	// keyword names it. Only the §4.7 entries combining several keywords into one
	// object — if/then/else, contains, unevaluated — reach keep directly, because
	// no single keyword names those.
	keepKeyword := func(keyword string) {
		raw, err := rawFromNode(rawPropertyNode(s, keyword))
		keep("openapi:"+keyword, raw, err, pointer+ptr(keyword), keyword)
	}

	if s.GetNot() != nil {
		keepKeyword("not")
	}
	ite, iteErr := ifThenElseRaw(s)
	if ite != nil || iteErr != nil {
		keep("openapi:if-then-else", ite, iteErr, pointer, "if/then/else")
	}
	if ds := s.GetDependentSchemas(); ds != nil && ds.Len() > 0 {
		keepKeyword("dependentSchemas")
	}
	// dependentRequired is read off the raw node because oas3.Schema has no field
	// for it at v1.24.0 — the only reason it was silently dropped where its
	// sibling dependentSchemas was kept.
	dr, drErr := rawFromNode(rawPropertyNode(s, "dependentRequired"))
	if dr != nil || drErr != nil {
		keep("openapi:dependentRequired", dr, drErr, pointer+ptr("dependentRequired"), "dependentRequired")
	}
	if s.GetPropertyNames() != nil {
		keepKeyword("propertyNames")
	}
	craw, cErr := containsRaw(s)
	if craw != nil || cErr != nil {
		keep("openapi:contains", craw, cErr, pointer, "contains")
	}
	u, uErr := unevaluatedRaw(s)
	if u != nil || uErr != nil {
		keep("openapi:unevaluated", u, uErr, pointer, "unevaluated")
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

// preserveNodeInto converts node and records it under key, reporting a construct
// that reached the IR in no form at all rather than leaving its caller to
// announce a preservation that did not happen (GitHub #144).
//
// It reports whether an entry was written, which is what a caller with an
// announcement to make gates on. The three outcomes it distinguishes are the
// point: an absent node writes nothing and says nothing, because there was no
// construct; a converted one writes the entry; an unconvertible one writes
// nothing and yields the diagnostic that says so.
func preserveNodeInto(p *ir.Unmodeled, key string, node *yaml.Node,
	reason ir.UnmodeledReason, pointer string, srcIndex int,
) (bool, []ir.Diagnostic) {
	raw, err := rawFromNode(node)
	if err != nil {
		return false, []ir.Diagnostic{unpreservableDiag(key, pointer, srcIndex, err)}
	}
	if len(raw) == 0 {
		return false, nil
	}
	preserveInto(p, key, raw, reason, pointer, srcIndex)
	return true, nil
}

// unpreservableDiag reports a construct the compiler could neither model nor keep
// verbatim, so nothing of it reached the IR.
//
// Error rather than the degradation severity its callers otherwise use: a
// degraded lowering still describes the source in a weaker shape, while this one
// leaves no trace at all, which is a losslessness failure rather than a
// compromise (GitHub #144).
//
// The vendor-extension reader (extensionsFrom) reports the same conversion
// failure as a warning and is deliberately left alone: it already branches on the
// error and never claimed to have kept anything, so it is not the defect this
// code exists for.
func unpreservableDiag(key, pointer string, srcIndex int, err error) ir.Diagnostic {
	return diag.Newf(ir.SeverityError, diag.UnpreservableConstruct,
		ir.Provenance{Source: srcIndex, Pointer: pointer},
		"%s could not be kept verbatim under Unmodeled and is represented in the IR "+
			"in no form at all: %s", key, err.Error())
}

// preserveKeywordInto records a validation-only keyword and returns the one
// diagnostic announcing it. An absent payload records nothing and announces
// nothing; an unconvertible one never reaches here, because its caller reports it
// through unpreservableDiag first.
func preserveKeywordInto(p *ir.Unmodeled, key string, raw ir.RawValue,
	declPtr, entryPtr, label string, srcIndex int,
) []ir.Diagnostic {
	if len(raw) == 0 {
		return nil
	}
	preserveInto(p, key, raw, ir.ReasonValidationOnly, entryPtr, srcIndex)
	return []ir.Diagnostic{diag.Newf(ir.SeverityInfo, diag.ValidationOnlyKeyword,
		ir.Provenance{Source: srcIndex, Pointer: declPtr},
		"validation-only keyword %q kept verbatim under Unmodeled", label)}
}
