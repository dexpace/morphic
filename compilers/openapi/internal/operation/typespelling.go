// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"encoding/json/jsontext"
	"slices"
	"strings"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

// typeSpelling is how a parameter or header stated its type: the schema node,
// the pointer that node sits at, the media type serializing it — empty for the
// `schema` spelling — the examples a content-style entry declares, and what the
// elected entry writes beyond its type and the spelling the election passed
// over, kept for the carrier at this position to merge onto its own Unmodeled.
type typeSpelling struct {
	js        *oas3.JSONSchema[oas3.Referenceable]
	pointer   jsontext.Pointer
	mediaType string
	unmodeled ir.Unmodeled
	examples  []ir.Example
}

// electTypeSpelling picks the spelling a parameter or header states its type
// with, keeping the other verbatim where the document writes both.
//
// OpenAPI forbids both `schema` and `content`, and neither position can lower
// both, so one lowers and the other is kept beside it, as schema.dispatchOf
// does for competing keywords.
//
// `content` wins: its entry carries a schema and the media type serializing it,
// both with IR homes here (HTTPParamBinding.ContentType,
// Property.Encoding.MediaType), where `schema` would push the media type into
// Unmodeled. The specification gives no precedence. The election is
// unconditional, even where the entry names no schema (GitHub #320).
func electTypeSpelling(c lowering.Ctx, js *oas3.JSONSchema[oas3.Referenceable],
	content *sequencedmap.Map[string, *soa.MediaType], root *yaml.Node, at jsontext.Pointer,
	carrier, home string,
) (typeSpelling, []ir.Diagnostic) {
	// A content parameter or header declares exactly one media type;
	// electContentEntry takes it, and passedOverEntries keeps and reports what a
	// document declares beyond it, rather than dropping that in silence (GitHub
	// #139).
	var diags []ir.Diagnostic
	mt, media, ok := electContentEntry(content)
	if ok {
		mediaPtr := at + ids.Ptr("content", mt)
		scope := ids.Scope("content", mt)
		examples, residue, residueDiags := contentEntryFields(c, media, mediaPtr, scope, carrier, home)
		diags = append(diags, residueDiags...)
		diags = append(diags, passedOverEntries(c, &residue, content, mt, root, at)...)
		elected := typeSpelling{
			js:        media.GetSchema(),
			pointer:   mediaPtr + ids.Ptr("schema"),
			mediaType: mt,
			unmodeled: residue,
			examples:  examples,
		}
		diags = append(diags, passedOverSpelling(c, &elected.unmodeled, root, "schema", "content", at)...)
		return elected, diags
	}
	elected := typeSpelling{js: js, pointer: at + ids.Ptr("schema")}
	if js == nil {
		// Neither spelling states a type — a header carrying only a description,
		// or a `content` map naming no usable entry — so there is no winner, and
		// nothing was passed over for one.
		return elected, diags
	}
	diags = append(diags, passedOverSpelling(c, &elected.unmodeled, root, "content", "schema", at)...)
	return elected, diags
}

// passedOverSpelling keeps verbatim the spelling the election passed over and
// reports it once, naming both. A document that wrote only the elected one
// records nothing and says nothing.
//
// The reason is ReasonDegradedLowering, as schema.recordSkippedFamilies uses for
// the keyword families its own election passes over. The severity is warning,
// not the info announcing a conjunction JSON Schema allows, because OpenAPI
// forbids this one; it matches passedOverEntries'. It is not an error: the
// document lowers as well as an election can make it, and harness.Check stops
// at the first error diagnostic, hiding later findings.
func passedOverSpelling(c lowering.Ctx, u *ir.Unmodeled, root *yaml.Node, passed, elected string, at jsontext.Pointer) []ir.Diagnostic {
	pointer := at + ids.Ptr(passed)
	kept, diags := annotation.PreserveNodeInto(u, "openapi:"+passed,
		nodeview.Child(root, passed), ir.ReasonDegradedLowering, c.ProvenanceAt(pointer))
	if !kept {
		return diags
	}
	return append(diags, c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, pointer,
		"a parameter or header declares either schema or content, not both; this one declares "+
			"both, so it lowered as its %s, with %s kept verbatim under Unmodeled", elected, passed))
}

// electContentEntry returns the one entry a content-style header or parameter
// lowers through, or false where no entry holds one.
//
// OpenAPI requires exactly one entry at both positions, and ir.Property and
// ir.Parameter each hold a single type, so a document declaring more needs an
// election. It takes the lowest key as written, which no declaration order can
// change: the specification ranks none, and the entry a run lowers must not
// follow the order of a map.
func electContentEntry(content *sequencedmap.Map[string, *soa.MediaType]) (string, *soa.MediaType, bool) {
	var elected string
	var chosen *soa.MediaType
	for mt, media := range content.All() {
		if media != nil && (chosen == nil || mt < elected) {
			elected, chosen = mt, media
		}
	}
	return elected, chosen, chosen != nil
}

// passedOverEntries keeps each entry of content but elected verbatim under
// openapi:content/<key>, the key escaped as a pointer segment, and reports them
// once at the map, sorted so the report does not follow declaration order
// either. root is the object content was written on.
//
// The reason and severity are passedOverSpelling's: OpenAPI forbids the entries
// and an election passed them over, so the document's own error is visible
// rather than absorbed. An entry interns no node, so --explain at its pointer
// finds none; the warning at the map is what names it.
func passedOverEntries(c lowering.Ctx, u *ir.Unmodeled, content *sequencedmap.Map[string, *soa.MediaType],
	elected string, root *yaml.Node, at jsontext.Pointer,
) []ir.Diagnostic {
	written := nodeview.Child(root, "content")
	var diags []ir.Diagnostic
	var kept []string
	for _, mt := range slices.Sorted(content.Keys()) {
		if mt == elected {
			continue
		}
		ok, keptDiags := annotation.PreserveNodeInto(u, annotation.EntryKey("content", mt),
			nodeview.Child(written, mt), ir.ReasonDegradedLowering, c.ProvenanceAt(at+ids.Ptr("content", mt)))
		diags = append(diags, keptDiags...)
		if ok {
			kept = append(kept, mt)
		}
	}
	if len(kept) == 0 {
		return diags
	}
	return append(diags, c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, at+ids.Ptr("content"),
		"a content-style header or parameter must declare exactly one media type; "+
			"%q is lowered and %s kept verbatim under Unmodeled", elected, strings.Join(kept, ", ")))
}

// contentEntryFields returns everything a Media Type Object declares at a
// parameter's or header's elected `content` position beyond the one type that
// position lowers: its example/examples, its x-* and undeclared keys, the
// fields contentOnlyFields names, and what keepMediaTypeFields keeps, none of
// which may vanish without a diagnostic (GitHub #611).
//
// scope is the content entry's own path, so several media types and the
// enclosing object cannot collide on one key. carrier and home name the
// position in each content-only field's info, with ReasonNoIRHome.
func contentEntryFields(c lowering.Ctx, media *soa.MediaType, mediaPtr jsontext.Pointer,
	scope, carrier, home string,
) ([]ir.Example, ir.Unmodeled, []ir.Diagnostic) {
	examples, diags := exampleList(c, media.GetExample(), media.GetExamples(), mediaPtr)
	var unmodeled ir.Unmodeled
	ext, extDiags := annotation.ExtensionsUnder(media.GetExtensions(), c.ProvenanceAt, mediaPtr, scope)
	unmodeled = annotation.MergeUnmodeled(unmodeled, ext)
	diags = append(diags, extDiags...)
	for _, field := range contentOnlyFields(media) {
		at := mediaPtr + ids.Ptr(field.keyword)
		kept, keptDiags := annotation.PreserveNodeInto(&unmodeled,
			annotation.EntryKey(scope, field.keyword), field.node, ir.ReasonNoIRHome, c.ProvenanceAt(at))
		diags = append(diags, keptDiags...)
		if !kept {
			continue
		}
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
			"%s content media type %s has no %s home; kept verbatim under Unmodeled",
			carrier, field.keyword, home))
	}
	diags = append(diags, keepMediaTypeFields(c, &unmodeled, media, mediaPtr, scope)...)
	// Last, so the keys the readers above kept are already recorded and the census
	// leaves them alone: it answers only for what nothing read.
	return examples, unmodeled,
		append(diags, annotation.UnknownKeysUnder(&unmodeled, media, c.ProvenanceAt, mediaPtr, scope)...)
}

// contentOnlyFields returns the Media Type Object fields the IR models at a
// body's content position and gives a parameter or header no home for: the 3.2
// sequential-media fields and the multipart per-part encodings. Such a position
// lowers to one ir.Parameter or ir.Property holding one type, so each is kept
// verbatim instead (GitHub #611).
func contentOnlyFields(media *soa.MediaType) []writtenField {
	core := media.GetCore()
	return []writtenField{
		{"itemSchema", core.ItemSchema.ValueNode},
		{"itemEncoding", core.ItemEncoding.ValueNode},
		{"prefixEncoding", core.PrefixEncoding.ValueNode},
		{"encoding", core.Encoding.ValueNode},
	}
}

// writtenField is one field an object writes, with the parser's own reading of
// the node it was written at: for a value merged in through `<<`, the merged
// node, which a read of the raw mapping does not find.
type writtenField struct {
	keyword string
	node    *yaml.Node
}
