// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"encoding/json/jsontext"
	"slices"
	"strconv"
	"strings"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
	"github.com/dexpace/morphic/compilers/openapi/internal/service"
	"github.com/dexpace/morphic/ir"
)

// typeSpelling is how a parameter or header stated its type: the schema node,
// the context and pointer it is declared at, the media type serializing it,
// empty for the `schema` spelling, the examples a content-style entry
// declares, and what the elected entry writes beyond its type and the spelling
// the election passed over, for the carrier to merge onto its Unmodeled. A
// content entry written as a $ref declares its schema where the chain ends
// (declaredMedia), in that document's context. carries is the scopes the
// carrier keeps what the spelling's objects write under, nearest first
// (promotion.CarrierScopes).
type typeSpelling struct {
	ctx       service.Ctx
	js        *oas3.JSONSchema[oas3.Referenceable]
	pointer   jsontext.Pointer
	mediaType string
	carries   []string
	unmodeled ir.Unmodeled
	examples  []ir.Example
}

// schemaScope is the scope the carrier keeps what the spelling's schema
// writes under, the last of carries.
func (s typeSpelling) schemaScope() string { return s.carries[len(s.carries)-1] }

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
func electTypeSpelling(serviceContext service.Ctx, loweringState operationLoweringState, js *oas3.JSONSchema[oas3.Referenceable],
	content *sequencedmap.Map[string, *soa.MediaType], root *yaml.Node, at jsontext.Pointer,
	carrier, home string,
) (typeSpelling, []ir.Diagnostic) {
	// A content parameter or header declares exactly one media type;
	// chooseSingleContentEntry takes it, and passedOverEntries keeps and reports what a
	// document declares beyond it, rather than dropping that in silence (GitHub
	// #139).
	var diags []ir.Diagnostic
	mt, media, ok := chooseSingleContentEntry(content)
	if ok {
		mediaPtr := at + ids.Ptr("content", mt)
		elected, electedDiags := contentSpelling(serviceContext, media, mt, mediaPtr, carrier, home)
		diags = append(diags, electedDiags...)
		diags = append(diags, loweringState.kept.once(serviceContext.IDs().Coordinate(at), func() (ir.Unmodeled, []ir.Diagnostic) {
			return passedOverEntries(serviceContext, nodeview.Pairs(nodeview.Child(root, "content")), mt, at)
		})...)
		diags = append(diags, passedOverSpelling(serviceContext, &elected.unmodeled, root, "schema", "content", at)...)
		return elected, diags
	}
	elected := typeSpelling{ctx: serviceContext, js: js, pointer: at + ids.Ptr("schema"), carries: promotion.CarrierScopes("")}
	if js == nil {
		// Neither spelling states a type — a header carrying only a description,
		// or a `content` map naming no usable entry — so there is no winner, and
		// nothing was passed over for one.
		return elected, diags
	}
	diags = append(diags, passedOverSpelling(serviceContext, &elected.unmodeled, root, "content", "schema", at)...)
	return elected, diags
}

// contentSpelling is the typeSpelling of media, the elected entry of a
// parameter's or header's content map, written under mt at mediaPtr: its
// schema, examples and residue as it declares them (declaredMedia). One
// written as a $ref that resolves to nothing states no type, as one naming no
// schema does, but still names its media type.
func contentSpelling(serviceContext service.Ctx, media *soa.MediaType, mt string, mediaPtr jsontext.Pointer,
	carrier, home string,
) (typeSpelling, []ir.Diagnostic) {
	elected := typeSpelling{ctx: serviceContext, pointer: mediaPtr + ids.Ptr("schema"), mediaType: mt,
		carries: promotion.CarrierScopes(mt)}
	mediaDecl, found := declaredMedia(serviceContext, media, mediaPtr)
	if !found {
		return elected, nil
	}
	scope := ids.Scope("content", mt)
	examples, residue, diags := contentEntryFields(mediaDecl, scope, carrier, home)
	if mediaDecl.use != nil {
		// The carrier has no docs for what the entry's chain writes beside its
		// $ref, so its summary and description are kept with its other keys.
		beside, besideDiags := service.Beside[soa.MediaType](mediaDecl.entryCtx, mediaDecl.use, mediaDecl.entryPtr, false)
		residue = annotation.MergeUnmodeled(residue, annotation.Under(scope, beside))
		diags = append(diags, besideDiags...)
	}
	elected.ctx, elected.js, elected.pointer = mediaDecl.ctx, mediaDecl.media.GetSchema(), mediaDecl.pointer+ids.Ptr("schema")
	elected.unmodeled, elected.examples = residue, examples
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
// document lowers as well as an election can make it.
func passedOverSpelling(serviceContext service.Ctx, unmodeled *ir.Unmodeled, root *yaml.Node, passed, elected string, at jsontext.Pointer) []ir.Diagnostic {
	pointer := at + ids.Ptr(passed)
	kept, diags := annotation.PreserveNodeInto(unmodeled, "openapi:"+passed,
		nodeview.Child(root, passed), ir.ReasonDegradedLowering, serviceContext.Place().At(pointer))
	if !kept {
		return diags
	}
	return append(diags, serviceContext.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, pointer,
		"a parameter or header declares either schema or content, not both; this one declares "+
			"both, so it lowered as its %s, with %s kept verbatim under Unmodeled", elected, passed))
}

// chooseSingleContentEntry returns the one entry a content-style header or parameter
// lowers through, or false where no entry holds one.
//
// OpenAPI requires exactly one entry at both positions, and ir.Property and
// ir.Parameter each hold a single type, so a document declaring more needs an
// election. It takes the lowest key as written, which no declaration order can
// change: the specification ranks none, and the entry a run lowers must not
// follow the order of a map.
func chooseSingleContentEntry(content *sequencedmap.Map[string, *soa.MediaType]) (string, *soa.MediaType, bool) {
	var elected string
	var chosen *soa.MediaType
	for mt, media := range content.All() {
		if media != nil && (chosen == nil || mt < elected) {
			elected, chosen = mt, media
		}
	}
	return elected, chosen, chosen != nil
}

// passedOverEntries returns each entry of written, a content map's pairs, but
// elected, kept verbatim under openapi:<its coordinate>, and reports them once at
// the map, sorted, so the report follows no declaration order.
//
// They are the Document's rather than the carrier's (keptOnce): a carrier
// a $ref reaches lowers once per reference, and an entry belongs to the
// declaration, which no node stands for. So what they cost is what the source
// wrote, and none need dropping.
//
// The reason and severity are passedOverSpelling's. An entry interns no node, and
// --explain does not read Unmodeled (GitHub #693), so only the warning names it.
func passedOverEntries(serviceContext service.Ctx, written []nodeview.Pair, elected string, at jsontext.Pointer) (ir.Unmodeled, []ir.Diagnostic) {
	passed := slices.DeleteFunc(slices.SortedFunc(slices.Values(written), byKey),
		func(entry nodeview.Pair) bool { return entry.Key == elected })
	var entries ir.Unmodeled
	var diags []ir.Diagnostic
	var kept []string
	for _, entry := range passed {
		pointer := at + ids.Ptr("content", entry.Key)
		ok, keptDiags := annotation.PreserveNodeInto(&entries, "openapi:"+serviceContext.IDs().Coordinate(pointer),
			entry.Val, ir.ReasonDegradedLowering, serviceContext.Place().At(pointer))
		diags = append(diags, keptDiags...)
		if ok {
			kept = append(kept, entry.Key)
		}
	}
	if len(kept) == 0 {
		return entries, diags
	}
	return entries, append(diags, serviceContext.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, at+ids.Ptr("content"),
		"a content-style header or parameter must declare exactly one media type; "+
			"%q is lowered and %s kept verbatim under the document's Unmodeled", elected, namedEntries(kept)))
}

// namedEntries lists keys for one warning: each quoted, so a key cannot break the
// line or read as two, and past annotation.MaxUnknownKeys a count of the rest,
// since the document sets how many there are.
func namedEntries(keys []string) string {
	named := keys[:min(len(keys), annotation.MaxUnknownKeys)]
	quoted := make([]string, len(named))
	for i, key := range named {
		quoted[i] = strconv.Quote(key)
	}
	list := strings.Join(quoted, ", ")
	if rest := len(keys) - len(named); rest > 0 {
		return list + " and " + strconv.Itoa(rest) + " more"
	}
	return list
}

// byKey orders two pairs of one mapping, whose keys differ.
func byKey(a, b nodeview.Pair) int { return strings.Compare(a.Key, b.Key) }

// contentEntryFields returns everything the Media Type Object mediaDeclValue declares at a
// parameter's or header's elected `content` position beyond the one type that
// position lowers: its example/examples, its x-* and undeclared keys, and the
// fields contentOnlyFields names, none of which may vanish without a
// diagnostic (GitHub #611).
//
// scope is the content entry's own path, so several media types and the
// enclosing object cannot collide on one key. carrier and home name the
// position in each content-only field's info, with ReasonNoIRHome.
func contentEntryFields(mediaDeclValue mediaDecl, scope, carrier, home string) ([]ir.Example, ir.Unmodeled, []ir.Diagnostic) {
	serviceContext, media, mediaPtr := mediaDeclValue.ctx, mediaDeclValue.media, mediaDeclValue.pointer
	examples, diags := exampleList(serviceContext, media.GetExample(), media.GetExamples(), mediaPtr)
	var unmodeled ir.Unmodeled
	ext, extDiags := annotation.ExtensionsUnder(media.GetExtensions(), serviceContext.Place(), mediaPtr, scope)
	unmodeled = annotation.MergeUnmodeled(unmodeled, ext)
	diags = append(diags, extDiags...)
	for _, field := range contentOnlyFields(media) {
		at := mediaPtr + ids.Ptr(field.keyword)
		kept, keptDiags := annotation.PreserveNodeInto(&unmodeled,
			annotation.EntryKey(scope, field.keyword), field.node, ir.ReasonNoIRHome, serviceContext.Place().At(at))
		diags = append(diags, keptDiags...)
		if !kept {
			continue
		}
		diags = append(diags, serviceContext.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
			"%s content media type %s has no %s home; kept verbatim under Unmodeled",
			carrier, field.keyword, home))
	}
	// Last, so the keys the readers above kept are already recorded and the census
	// leaves them alone: it answers only for what nothing read.
	return examples, unmodeled,
		append(diags, annotation.UnknownKeysUnder(&unmodeled, media, serviceContext.Place(), mediaPtr, scope)...)
}

// contentOnlyFields returns the Media Type Object fields the IR models at a
// body's content position and gives a parameter or header no home for: the 3.2
// sequential-media fields, the multipart per-part encodings and the entry's
// description, which the parser's model names no field for. Such a position
// lowers to one ir.Parameter or ir.Property holding one type, so each is kept
// verbatim instead (GitHub #611).
func contentOnlyFields(media *soa.MediaType) []writtenField {
	core := media.GetCore()
	return []writtenField{
		{"itemSchema", core.ItemSchema.ValueNode},
		{"itemEncoding", core.ItemEncoding.ValueNode},
		{"prefixEncoding", core.PrefixEncoding.ValueNode},
		{"encoding", core.Encoding.ValueNode},
		{"description", nodeview.Child(media.GetRootNode(), "description")},
	}
}

// writtenField is one field an object writes, with the parser's own reading of
// the node it was written at: for a value merged in through `<<`, the merged
// node, which a read of the raw mapping does not find.
type writtenField struct {
	keyword string
	node    *yaml.Node
}
