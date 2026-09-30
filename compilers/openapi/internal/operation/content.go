// Package operation lowers what a document says an API does: its path items,
// webhooks and callbacks, the parameters merged onto each operation, and the
// content of every request body, response and header.
//
// It sits above the schema walk and reaches down into it for every type
// position it meets, and above auth for the requirements an operation names. It
// reaches nothing above itself: the compiler assembles a Document from what
// LowerService returns rather than the walk writing into one.
//
// The recursion here is callbacks — an operation may declare callbacks, each a
// path item holding operations of its own — which is why lowerOperation,
// lowerCallbacks and lowerCallbackOps cannot be separated. internal/archtest
// pins that set.
package operation

import (
	"context"
	"encoding/json/jsontext"
	"slices"
	"strings"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/marshaller"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// lowerPayload lowers a request/response body's content map into a Payload with
// one Content per media type — all kept, in source order, with no primary-
// content selection (ir-design §7.2). The pointer is the payload owner (the
// response or requestBody); each Content's schema hoists under
// <pointer>/content/<mt>/schema.
func lowerPayload(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, content *sequencedmap.Map[string, *soa.MediaType], pointer jsontext.Pointer, hint string) (*ir.Payload, []ir.Diagnostic) {
	if content == nil || content.Len() == 0 {
		return nil, nil
	}
	var diags []ir.Diagnostic
	payload := &ir.Payload{}
	for mt, media := range content.All() {
		if media == nil {
			continue
		}
		entry, fromRef, entryDiags := contentEntry(c, media, pointer+ids.Ptr("content", mt))
		diags = append(diags, entryDiags...)
		if entry == nil {
			continue
		}
		one, contentDiags := lowerContent(c, ts, anchors, mt, entry, pointer, hint, fromRef)
		diags = append(diags, contentDiags...)
		payload.Contents = append(payload.Contents, one)
	}
	if len(payload.Contents) == 0 {
		return nil, diags
	}
	return payload, diags
}

// contentEntry returns the Media Type Object a content-map entry names: the
// entry itself, or the components/mediaTypes object a 3.2 `$ref` entry
// addresses.
//
// ok reports that the entry was reached through a `$ref`, which is what tells
// the caller two things: the resolved object is what lowers, and the `$ref` key
// itself has been read, so the census must leave it alone — a document that
// defines the reference is not a document with a key its object does not define.
//
// A target this compiler cannot resolve — an external document, a pointer that
// names no mediaTypes entry, nothing at all there, or an entry that is itself
// another `$ref` — leaves the original entry to lower as it did before, so the
// `$ref` is censused and kept verbatim, with one `openapi/unresolved-ref`
// diagnostic saying what was wrong (GitHub #615).
func contentEntry(c lowering.Ctx, media *soa.MediaType, entryPtr jsontext.Pointer) (*soa.MediaType, bool, []ir.Diagnostic) {
	ref, isRef := rawRefOf(media)
	if !isRef || !c.Is32() {
		return media, false, nil
	}
	resolved, ok, message := resolveMediaTypeRef(c, ref)
	if !ok {
		return media, false, []ir.Diagnostic{c.DiagAt(ir.SeverityError, diag.UnresolvedRef, entryPtr,
			"media type $ref %q %s; the entry lowers as written with the reference kept verbatim",
			ref, message)}
	}
	return resolved, true, nil
}

// resolveMediaTypeRef reads the components/mediaTypes object a `$ref` names and
// unmarshals it into a Media Type Object. It reports a message naming what went
// wrong rather than returning an error, because every failure lands in the same
// diagnostic the caller builds.
func resolveMediaTypeRef(c lowering.Ctx, ref string) (*soa.MediaType, bool, string) {
	ptr, internal := c.RefScope().InternalPointer(ref)
	if !internal {
		return nil, false, "does not resolve inside this document"
	}
	kind, name, ok := ids.ComponentEntry(ptr)
	if !ok || kind != ids.MediaTypesKind {
		return nil, false, "does not name a components/" + ids.MediaTypesKind + " entry"
	}
	node := rawComponentNode(c.Doc.GetRootNode(), kind, name)
	if node == nil {
		return nil, false, "names an entry this document does not declare"
	}
	var out soa.MediaType
	errs, err := marshaller.UnmarshalNode(context.Background(), "", node, &out)
	if err != nil {
		return nil, false, "could not be read as a media type object"
	}
	if len(errs) > 0 {
		return nil, false, "is not a media type object: " + diag.OneLine(errs[0])
	}
	if _, chained := rawRefOf(&out); chained {
		// A one-hop reading, deliberately: the shapes are one entry, and following a
		// chain would need the resolver state a standalone node does not carry.
		return nil, false, "names an entry that is itself a $ref, which is not followed"
	}
	return &out, true, ""
}

// rawRefOf returns the `$ref` string a raw object writes, and whether it wrote
// one. Both a content entry and a components/mediaTypes entry are read this way:
// the library's Media Type model has no Reference wrapper, so a `$ref` reaches
// the compiler as a key the model does not define.
func rawRefOf(media *soa.MediaType) (string, bool) {
	node := annotation.RawChildNode(media.GetRootNode(), "$ref")
	if node == nil || node.Value == "" {
		return "", false
	}
	return node.Value, true
}

// rawComponentNode returns the raw node of a /components/<kind>/<name> entry.
func rawComponentNode(root *yaml.Node, kind, name string) *yaml.Node {
	components := annotation.RawChildNode(root, "components")
	return annotation.RawChildNode(annotation.RawChildNode(components, kind), name)
}

// contentDecidedKeys names the content-entry keys a reader has already taken for
// this document, which the census must leave alone. An entry resolved from a
// `$ref` has had its `$ref` read; an entry that was not resolved has not, and
// the warning is owed.
func contentDecidedKeys(fromRef bool) []string {
	if !fromRef {
		return nil
	}
	return []string{"$ref"}
}

// lowerContent lowers one media-type view: its type graph, examples, binary/
// form specialization, sequential-media shape, and extensions.
func lowerContent(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, mt string, media *soa.MediaType, pointer jsontext.Pointer, hint string, fromRef bool) (ir.Content, []ir.Diagnostic) {
	mediaPtr := pointer + ids.Ptr("content", mt)
	mediaType, diags := schema.Ref(c, ts, anchors, schema.TopLevelDepth, media.GetSchema(), mediaPtr+ids.Ptr("schema"), hint)
	content := ir.Content{
		MediaType: mt,
		Type:      mediaType,
	}
	ex, exDiags := exampleList(c, media.GetExample(), media.GetExamples(), mediaPtr)
	diags = append(diags, exDiags...)
	if len(ex) > 0 {
		content.Examples = ex
	}
	switch {
	case isBinaryBody(mt, media.GetSchema()):
		content.File = &ir.FileInfo{IsText: false, ContentTypes: []string{mt}}
		content.Type = ts.PrimRef(ir.PrimBytes)
	case isFormContent(mt):
		enc, encUnmodeled, encDiags := partEncodings(c, ts, anchors, media, mediaPtr, content.Type.Target)
		diags = append(diags, encDiags...)
		if len(enc) > 0 {
			content.Encoding = enc
		}
		content.Unmodeled = annotation.MergeUnmodeled(content.Unmodeled, encUnmodeled)
	}
	diags = append(diags, fillSequential(c, ts, anchors, &content, media, mediaPtr, hint)...)
	ext, extDiags := schema.ExtensionsOf(c, media.GetExtensions(), mediaPtr)
	diags = append(diags, extDiags...)
	if len(ext) > 0 {
		content.Unmodeled = annotation.MergeUnmodeled(content.Unmodeled, ext)
	}
	return content, append(diags,
		annotation.UnknownKeysDecided(&content.Unmodeled, media, c.ProvenanceAt, mediaPtr, "",
			contentDecidedKeys(fromRef))...)
}

// fillSequential lowers 3.2 sequential-media fields: itemSchema becomes the
// element type, and itemEncoding becomes Content.ItemEncoding, with Multi set
// because the construct describes a repeated tail by definition.
//
// That lowering only holds when no prefixEncoding accompanies it: prefixes make
// itemEncoding govern the items *after* them rather than every item, which a
// single every-item encoding would misstate. Those documents take
// positionalEncoding instead.
func fillSequential(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, content *ir.Content, media *soa.MediaType, mediaPtr jsontext.Pointer, hint string) []ir.Diagnostic {
	var diags []ir.Diagnostic
	if item := media.GetItemSchema(); item != nil {
		ref, itemDiags := schema.Ref(c, ts, anchors, schema.TopLevelDepth, item, mediaPtr+ids.Ptr("itemSchema"), compile.SubHint(hint, "item"))
		diags = append(diags, itemDiags...)
		content.Item = &ref
	}
	if len(media.GetPrefixEncoding()) > 0 {
		return append(diags, positionalEncoding(c, content, media, mediaPtr)...)
	}
	enc := media.GetItemEncoding()
	if enc == nil {
		return diags
	}
	pe, unmodeled, encDiags := encodingConfig(c, ts, anchors, enc, mediaPtr+ids.Ptr("itemEncoding"), "itemEncoding")
	pe.Multi = true
	content.ItemEncoding = &pe
	content.Unmodeled = annotation.MergeUnmodeled(content.Unmodeled, unmodeled)
	return append(diags, encDiags...)
}

// positionalEncoding keeps 3.2 positional prefixEncoding — and the itemEncoding
// that governs the tail after it — verbatim, with one info diagnostic.
// Content.ItemEncoding states one encoding for every item, so it cannot say
// "these two in order, then the rest alike": lowering only the tail into it
// would drop the prefixes and assert their encoding governs every item. The
// ordinals a positional form needs are a gap the IR can close later, so the
// entries carry ReasonNoIRHome rather than a degraded lowering.
func positionalEncoding(c lowering.Ctx, content *ir.Content, media *soa.MediaType, mediaPtr jsontext.Pointer) []ir.Diagnostic {
	root := media.GetRootNode()
	// The announcement follows prefixEncoding, the construct that brought this
	// lowering here: an itemEncoding beside it is optional, so its absence must not
	// suppress the message, and its own conversion failure reports separately.
	at := mediaPtr + ids.Ptr("prefixEncoding")
	kept, diags := schema.PreserveNode(c, &content.Unmodeled, "openapi:prefixEncoding",
		annotation.RawChildNode(root, "prefixEncoding"), ir.ReasonNoIRHome, at)
	_, itemDiags := schema.PreserveNode(c, &content.Unmodeled, "openapi:itemEncoding",
		annotation.RawChildNode(root, "itemEncoding"), ir.ReasonNoIRHome, mediaPtr+ids.Ptr("itemEncoding"))
	diags = append(diags, itemDiags...)
	if !kept {
		// Reaching here means prefixEncoding is declared — that is the only reason
		// this lowering runs — yet nothing of it was written. Unlike every other
		// preservation site, an empty payload here cannot mean "there was no
		// construct", so it is reported rather than passed over (GitHub #144).
		return append(diags, c.DiagAt(ir.SeverityError, diag.UnpreservableConstruct, at,
			"prefixEncoding is declared but its source node could not be read; it is "+
				"represented in the IR in no form at all"))
	}
	return append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, mediaPtr,
		"prefixEncoding is positional and has no per-item IR home; it and any itemEncoding are kept under Unmodeled"))
}

// partEncodings builds the multipart/form per-part wire config, keyed by each
// body-model property's PropID. A part is included when it carries an explicit
// encoding entry or is itself a repeated (array) or file (binary) part. body is
// the TypeID the content's own schema position lowered to.
func partEncodings(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, media *soa.MediaType, mediaPtr jsontext.Pointer, body ir.TypeID) (map[ir.PropID]ir.PartEncoding, ir.Unmodeled, []ir.Diagnostic) {
	parts := bodyParts(media.GetSchema(), 0)
	if len(parts) == 0 {
		return nil, nil, nil
	}
	// Key by the pointer the body model was interned at, not by mediaPtr, so the
	// keys align with that model's property IDs (invariant 2/3). Asking the IR is
	// what keeps this in step with schemaOf, which reads the end of a $ref chain
	// while a pointer cut from the ref string names only its first hop.
	schemaPtr, ok := bodyModelPointer(ts, body)
	if !ok {
		schemaPtr = bodySchemaPointer(c, media.GetSchema(), mediaPtr+ids.Ptr("schema"))
	}
	var diags []ir.Diagnostic
	var unmodeled ir.Unmodeled
	encMap := media.GetEncoding()
	out := map[ir.PropID]ir.PartEncoding{}
	for _, part := range parts {
		pe, partUnmodeled, partDiags := buildPartEncoding(c, ts, anchors, part.name, part.schema, encMap, mediaPtr)
		diags = append(diags, partDiags...)
		// Before the emptiness check, not after it: an entry declaring only
		// allowReserved or only x-* lowers to an empty PartEncoding that is left out
		// of the map, and what it did declare would go with it.
		unmodeled = annotation.MergeUnmodeled(unmodeled, partUnmodeled)
		if partEncodingEmpty(pe) {
			continue
		}
		out[partPropID(ts, body, part.name, schemaPtr)] = pe
	}
	if len(out) == 0 {
		return nil, unmodeled, diags
	}
	return out, unmodeled, diags
}

// bodyPart is one multipart part: the name keying its encoding entry, and the
// schema whose shape decides the structural flags.
type bodyPart struct {
	name   string
	schema *oas3.JSONSchema[oas3.Referenceable]
}

// maxPartCompositionDepth bounds the allOf walk bodyParts makes. A $ref cycle is
// refused at load, so no source document can spell a composition that deep; the
// bound is what keeps the walk terminating without relying on that.
const maxPartCompositionDepth = 16

// bodyParts returns the parts a multipart body declares, in source order: the
// properties written on the schema itself, then those each allOf branch
// contributes, with the first declaration of a name winning.
//
// A body that composes rather than declares — `allOf: [{$ref: Form}]` — has no
// properties of its own, so reading only those found nothing to key against and
// discarded the whole encoding block without a word (GitHub #140). The two
// spellings describe the same wire format, so they must enumerate the same parts.
func bodyParts(js *oas3.JSONSchema[oas3.Referenceable], depth int) []bodyPart {
	if depth > maxPartCompositionDepth {
		return nil
	}
	s := schemaOf(js)
	if s == nil {
		return nil
	}
	var out []bodyPart
	if props := s.GetProperties(); props != nil {
		for name, pjs := range props.All() {
			out = append(out, bodyPart{name: name, schema: pjs})
		}
	}
	for _, branch := range s.GetAllOf() {
		out = append(out, bodyParts(branch, depth+1)...)
	}
	return dedupeParts(out)
}

// dedupeParts keeps the first declaration of each part name, in order. A name
// redeclared across allOf branches is one part on the wire, so it must key one
// encoding entry rather than depend on which branch was walked last.
func dedupeParts(parts []bodyPart) []bodyPart {
	seen := make(map[string]bool, len(parts))
	out := make([]bodyPart, 0, len(parts))
	for _, part := range parts {
		if seen[part.name] {
			continue
		}
		seen[part.name] = true
		out = append(out, part)
	}
	return out
}

// partPropID returns the ID of the property carrying the given wire name on the
// model body stands for, falling back to deriving one under schemaPtr.
//
// The IR is asked first because §4.3 stores only a model's *own* properties:
// a composed body holds its parts on the Base it inherits them from, so a key
// derived from the composed node's pointer would name a property that exists
// nowhere. Deriving one remains the answer for a body the IR holds no model for
// — a contradictory schema declaring properties beside an enum or a scalar type —
// where no property was lowered for any pointer to name.
func partPropID(ts *compile.Types, body ir.TypeID, wire string, schemaPtr jsontext.Pointer) ir.PropID {
	if id, ok := propIDByWire(ts, body, wire, 0); ok {
		return id
	}
	return ids.Prop(schemaPtr + ids.Ptr("properties", wire))
}

// propIDByWire searches the type body denotes for a property with the given wire
// name: what it declares itself, then what it inherits through Base and mixes in
// through Mixins, following the alias scalars a $ref-with-siblings position
// hoists on the way. depth bounds the walk against a chain no source can spell.
func propIDByWire(ts *compile.Types, body ir.TypeID, wire string, depth int) (ir.PropID, bool) {
	if depth > maxBodyAliasHops {
		return "", false
	}
	td, found := ts.Node(body)
	if !found {
		return "", false
	}
	switch t := td.(type) {
	case *ir.Model:
		for _, p := range t.Properties {
			if p.WireName == wire {
				return p.ID, true
			}
		}
		return propIDInComposition(ts, t, wire, depth)
	case *ir.Scalar:
		if t.Base == nil {
			return "", false
		}
		return propIDByWire(ts, t.Base.Target, wire, depth+1)
	default:
		return "", false
	}
}

// propIDInComposition searches a model's Base and Mixins, in that order.
func propIDInComposition(ts *compile.Types, m *ir.Model, wire string, depth int) (ir.PropID, bool) {
	if m.Base != nil {
		if id, ok := propIDByWire(ts, m.Base.Target, wire, depth+1); ok {
			return id, true
		}
	}
	for _, mixin := range m.Mixins {
		if id, ok := propIDByWire(ts, mixin.Target, wire, depth+1); ok {
			return id, true
		}
	}
	return "", false
}

// buildPartEncoding assembles one part's PartEncoding: explicit encoding config
// (content types, headers, style, explode) merged with the structural flags Multi
// (array part) and Filename (binary/file part).
func buildPartEncoding(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, name string, pjs *oas3.JSONSchema[oas3.Referenceable], encMap *sequencedmap.Map[string, *soa.Encoding], mediaPtr jsontext.Pointer) (ir.PartEncoding, ir.Unmodeled, []ir.Diagnostic) {
	pe := ir.PartEncoding{}
	var unmodeled ir.Unmodeled
	var diags []ir.Diagnostic
	if encMap != nil {
		if enc, ok := encMap.Get(name); ok {
			pe, unmodeled, diags = encodingConfig(c, ts, anchors, enc,
				mediaPtr+ids.Ptr("encoding", name), ids.Scope("encoding", name))
		}
	}
	if part := schemaOf(pjs); part != nil {
		pe.Multi = schemaIsArray(part)
		pe.Filename = schemaIsFilePart(part)
	}
	return pe, unmodeled, diags
}

// encodingConfig lowers one Encoding object's declared wire config: content
// types, per-part headers, and form-style serialization. The structural flags
// (Multi, Filename) come from the part's own schema, not from here.
//
// It returns what the object declared that PartEncoding has no field for as a
// second value rather than writing it, because PartEncoding carries no
// Unmodeled map of its own; scope is where that belongs on the owning Content
// (see encodingUnmodeled).
func encodingConfig(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, enc *soa.Encoding, encPtr jsontext.Pointer, scope string) (ir.PartEncoding, ir.Unmodeled, []ir.Diagnostic) {
	pe := ir.PartEncoding{}
	if enc == nil {
		return pe, nil, nil
	}
	pe.ContentTypes = splitContentTypes(enc.GetContentTypeValue())
	headers, diags := lowerHeaders(c, ts, anchors, enc.GetHeaders(), encPtr)
	pe.Headers = headers
	if enc.Style != nil {
		pe.Style = string(*enc.Style)
	}
	pe.Explode = enc.Explode
	unmodeled, encDiags := encodingUnmodeled(c, enc, encPtr, scope)
	return pe, unmodeled, append(diags, encDiags...)
}

// encodingUnmodeled keeps what an Encoding Object declares that nothing in the
// IR holds: `allowReserved`, which ir.PartEncoding has no field for even though
// its neighbours style and explode do, the object's own x-*, and the keys the
// specification defines for no encoding at all. Neither allowReserved nor the
// extensions had reached an IR field, an Unmodeled entry or a diagnostic, so two
// documents differing only in them compiled to one IR (GitHub #291).
//
// Both ride on the owning ir.Content, since PartEncoding carries no Unmodeled
// map, keyed under scope — "encoding/<part>" or "itemEncoding". One content can
// hold an entry per multipart part plus a sequential item's, and they reach the
// same map, so the part is what tells them apart.
//
// allowReserved carries ReasonNoIRHome and announces itself only when something
// was written, the shape preserveHeaderSerialization already uses for the pair
// beside it.
func encodingUnmodeled(c lowering.Ctx, enc *soa.Encoding, encPtr jsontext.Pointer, scope string) (ir.Unmodeled, []ir.Diagnostic) {
	var out ir.Unmodeled
	at := encPtr + ids.Ptr("allowReserved")
	kept, diags := schema.PreserveNode(c, &out, "openapi:"+scope+"/allowReserved",
		annotation.RawChildNode(enc.GetRootNode(), "allowReserved"), ir.ReasonNoIRHome, at)
	if kept {
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
			"encoding allowReserved has no ir.PartEncoding home; kept verbatim under Unmodeled"))
	}
	diags = append(diags, nestedEncodings(c, &out, enc, encPtr, scope)...)
	ext, extDiags := schema.ExtensionsIn(c, enc.GetExtensions(), encPtr, scope)
	out = annotation.MergeUnmodeled(out, ext)
	diags = append(diags, extDiags...)
	return out, append(diags, annotation.UnknownKeysUnder(&out, enc, c.ProvenanceAt, encPtr, scope)...)
}

// nestedEncodingFields are the OpenAPI 3.2 Encoding Object fields that carry a
// nested Encoding Object: the object's own encoding map, the positional prefix
// encodings, and the item encoding governing what follows them. This compiler
// lowers an Encoding Object to ir.PartEncoding, which has no encoding fields of
// its own, so each reaches the IR in no modelled form.
var nestedEncodingFields = []string{"encoding", "prefixEncoding", "itemEncoding"}

// nestedEncodings keeps the nested Encoding Objects a 3.2 document writes,
// verbatim under the same scope the part's own entries ride on, one info each
// (GitHub #615). Recording them at the key and pointer the census itself uses is
// what suppresses the `unknown-object-key` warning a 3.2 document used to draw
// three of, without suppressing anything below 3.2 — where these keys are
// misspellings and the warning is owed.
//
// ReasonNoIRHome rather than a boundary: PartEncoding could grow the fields, and
// the entries are the promotion path. Nothing is lowered from them, because a
// nested encoding describes a part inside a part and this compiler has no shape
// for it — keeping the source is lossless and takes no position on how it would
// lower.
func nestedEncodings(c lowering.Ctx, out *ir.Unmodeled, enc *soa.Encoding, encPtr jsontext.Pointer, scope string) []ir.Diagnostic {
	if !c.Is32() {
		return nil
	}
	var diags []ir.Diagnostic
	for _, keyword := range nestedEncodingFields {
		at := encPtr + ids.Ptr(keyword)
		kept, keptDiags := schema.PreserveNode(c, out, "openapi:"+scope+"/"+ids.Scope(keyword),
			annotation.RawChildNode(enc.GetRootNode(), keyword), ir.ReasonNoIRHome, at)
		diags = append(diags, keptDiags...)
		if !kept {
			continue
		}
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
			"encoding %s has no ir.PartEncoding home; kept verbatim under Unmodeled", keyword))
	}
	return diags
}

// lowerHeaders lowers a header map into Properties in source order. Each
// entry's own pointer stays its ID and Provenance (two keys $ref'ing the same
// header must not collide), but its schema — and the name hint that schema is
// hoisted under — follow the ref target's declaration instead (issue #107).
func lowerHeaders(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, headers *sequencedmap.Map[string, *soa.ReferencedHeader], basePtr jsontext.Pointer) ([]ir.Property, []ir.Diagnostic) {
	if headers == nil || headers.Len() == 0 {
		return nil, nil
	}
	var diags []ir.Diagnostic
	out := make([]ir.Property, 0, headers.Len())
	for name, rh := range headers.All() {
		hptr := basePtr + ids.Ptr("headers", name)
		h, hdecl := resolve.ObjectAt[soa.Header](c.RefScope(), rh, hptr)
		if h == nil {
			continue
		}
		p, headerDiags := lowerHeader(c, ts, anchors, h, name, hptr, hdecl)
		diags = append(diags, headerDiags...)
		diags = append(diags, reservedHeaderEntryDiag(c, name, hptr)...)
		// A header entry written as a Reference Object keeps its own summary and
		// description: they describe this entry rather than the header declaration
		// it names (GitHub #610).
		p.Docs = resolve.RefDocs(rh, p.Docs)
		out = append(out, p)
	}
	return out, diags
}

// reservedHeaderEntryDiag reports a headers-map entry OpenAPI says SHALL be
// ignored. Both maps this lowering serves reserve Content-Type and nothing else:
// a response's (§4.8.17), whose media type its own `content` map already names,
// and an encoding's (§4.8.15), which the encoding's own `contentType` describes
// separately. The comparison is case-insensitive because HTTP field names are.
//
// It reports at the entry's own pointer rather than the declaration's, because
// the reserved thing is the key the header is mapped under, not the header
// object: two keys $ref'ing one component are two declarations, and only the one
// spelled Content-Type is reserved. That is the opposite choice from
// preserveHeaderSerialization, which keeps keywords the header object itself
// writes and so records them at the declaration.
//
// This is the headers-map half of the rule; reservedHeaderParamDiag is the
// parameter half. The header still lowers: see diag.ReservedHeaderName for why
// keeping it and reporting it is the choice, rather than dropping it here.
func reservedHeaderEntryDiag(c lowering.Ctx, name string, hptr jsontext.Pointer) []ir.Diagnostic {
	if !strings.EqualFold(name, "Content-Type") {
		return nil
	}
	return []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.ReservedHeaderName, hptr,
		"header %q is reserved: OpenAPI says a Content-Type entry in a headers map SHALL be "+
			"ignored, so it is lowered as declared and left for the emitter to suppress", name)}
}

// lowerHeader lowers one header entry into a Property. Its schema goes through
// schema.FillPropertyDetail like a model property's: a header schema declares
// docs, constraints, xml, examples and validation-only keywords the same way,
// and ir.Property has a field for each, so the header path had no reason to drop
// them (GitHub #116).
func lowerHeader(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, h *soa.Header, name string, hptr, hdecl jsontext.Pointer) (ir.Property, []ir.Diagnostic) {
	elected, diags := electTypeSpelling(c, h.GetSchema(), h.GetContent(), h.GetRootNode(), hdecl,
		"header", "ir.Property")
	// name is this entry's map key, which names the shared node after this mount
	// when the header is declared under another response (GitHub #433).
	headerType, headerDiags := schema.CarriedRef(c.NamingByReferenceAt(hptr, hdecl), ts, anchors,
		schema.TopLevelDepth, elected.js, elected.pointer, ids.DeclarationHint(hdecl, name))
	diags = append(diags, headerDiags...)
	p := ir.Property{
		ID:         ids.Prop(hptr),
		Name:       compile.NamingFor(name),
		WireName:   name,
		Type:       headerType,
		Required:   h.GetRequired(),
		Provenance: c.ProvenanceAt(hptr),
		Unmodeled:  elected.unmodeled,
	}
	if elected.mediaType != "" {
		// The media type a content-style header serializes its value in, which is
		// what ir.Encoding.MediaType holds. Nothing else on this path writes
		// Property.Encoding, so the content spelling loses nothing the schema
		// spelling keeps.
		p.Encoding = &ir.Encoding{MediaType: elected.mediaType}
	}
	diags = append(diags, schema.FillPropertyDetail(c, ts, anchors, &p, elected.js, elected.pointer)...)
	// The media type object's own examples are more specific than the schema's,
	// which FillPropertyDetail has just recorded, so they are applied after it.
	if len(elected.examples) > 0 {
		p.Examples = elected.examples
	}
	diags = append(diags, applyHeaderAnnotations(c, &p, h, hdecl)...)
	return p, append(diags, preserveHeaderSerialization(c, &p, h, hdecl)...)
}

// preserveHeaderSerialization keeps the two serialization controls a header
// object declares. OpenAPI §4.8.21 lets a header write `style` and `explode`,
// and explode governs how an array or object header value is written on the
// wire — a declared wire fact rather than a hint — but ir.Property has a field
// for neither. ir.PartEncoding does, and that is a multipart part's own config,
// not a header's; ir.Encoding, the one thing hanging off a Property here, names
// a value-encoding scheme rather than a parameter style. So they are kept
// verbatim instead of dropped, with ReasonNoIRHome since the IR can close the
// gap by adding the fields, exactly as a parameter's xml hints are kept.
//
// A header that declares neither records nothing: RawChildNode returns nil for
// an absent keyword and PreserveNode keeps nothing for a nil node.
func preserveHeaderSerialization(c lowering.Ctx, p *ir.Property, h *soa.Header, hdecl jsontext.Pointer) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, keyword := range []string{"style", "explode"} {
		at := hdecl + ids.Ptr(keyword)
		kept, keptDiags := schema.PreserveNode(c, &p.Unmodeled, "openapi:"+keyword,
			annotation.RawChildNode(h.GetRootNode(), keyword), ir.ReasonNoIRHome, at)
		diags = append(diags, keptDiags...)
		if !kept {
			continue
		}
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
			"header %s has no ir.Property home; kept verbatim under Unmodeled", keyword))
	}
	return diags
}

// contentOnlyFields are the Media Type Object fields the IR models at a body's
// content position and gives a parameter or header no home for: the 3.2
// sequential-media fields, and the multipart per-part encoding block. The
// position lowers to one ir.Parameter or ir.Property holding one type and no
// item or encoding fields, so each of these is kept verbatim instead of dropped
// — the same one-field read electTypeSpelling used to make, widened from the
// media type's schema to the whole object (GitHub #611).
var contentOnlyFields = []string{"itemSchema", "itemEncoding", "prefixEncoding", "encoding"}

// contentEntryFields returns everything a Media Type Object declares at a
// parameter's or header's elected `content` position beyond the one type that
// position lowers: its example/examples, its x-* and undeclared keys, and the
// fields contentOnlyFields names.
//
// Nothing read any of them, so a document writing `{content: {application/json:
// {schema, example, x-note}}}` lost the example and the extension with no field,
// no Unmodeled entry and no diagnostic — and a parser-modelled field like
// itemSchema produced no census warning either, so it vanished in silence twice
// over (GitHub #611). scope is the content entry's own path, so several media
// types — and the enclosing object's own entries — cannot collide on one key.
//
// carrier and home name the position in the one info per content-only field,
// which is a gap the IR can close by growing the field: ReasonNoIRHome rather
// than a boundary.
func contentEntryFields(c lowering.Ctx, media *soa.MediaType, mediaPtr jsontext.Pointer,
	scope, carrier, home string,
) ([]ir.Example, ir.Unmodeled, []ir.Diagnostic) {
	examples, diags := exampleList(c, media.GetExample(), media.GetExamples(), mediaPtr)
	var unmodeled ir.Unmodeled
	ext, extDiags := schema.ExtensionsIn(c, media.GetExtensions(), mediaPtr, scope)
	unmodeled = annotation.MergeUnmodeled(unmodeled, ext)
	diags = append(diags, extDiags...)
	for _, keyword := range contentOnlyFields {
		at := mediaPtr + ids.Ptr(keyword)
		kept, keptDiags := schema.PreserveNode(c, &unmodeled,
			"openapi:"+scope+"/"+ids.Scope(keyword),
			annotation.RawChildNode(media.GetRootNode(), keyword), ir.ReasonNoIRHome, at)
		diags = append(diags, keptDiags...)
		if !kept {
			continue
		}
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
			"%s content media type %s has no %s home; kept verbatim under Unmodeled",
			carrier, keyword, home))
	}
	// Last, so the keys the readers above kept are already recorded and the census
	// leaves them alone: it answers only for what nothing read.
	return examples, unmodeled,
		append(diags, annotation.UnknownKeysUnder(&unmodeled, media, c.ProvenanceAt, mediaPtr, scope)...)
}

// typeSpelling is how a parameter or header stated its type: the schema node,
// the pointer that node sits at, the media type serializing it — empty for the
// `schema` spelling — the examples a content-style entry declares, and whatever
// the election passed over, for the carrier at this position to merge onto its
// own Unmodeled.
type typeSpelling struct {
	js        *oas3.JSONSchema[oas3.Referenceable]
	pointer   jsontext.Pointer
	mediaType string
	unmodeled ir.Unmodeled
	examples  []ir.Example
}

// electTypeSpelling picks the spelling a parameter or header states its type
// with, and keeps the other verbatim where the document writes both. root is the
// declaring object's node and at is the pointer it sits at.
//
// OpenAPI says a parameter — and a header, which follows the parameter rules —
// MUST contain either a `schema` property or a `content` property, but not both.
// Neither position can lower both, since ir.Parameter and ir.Property each hold
// one type, so a document writing both needs the election §4.8 already applies
// to competing keywords elsewhere (schema.dispatchOf): one form lowers and every
// passed-over one is kept verbatim beside it rather than dropped.
//
// `content` wins because it is the more expressive of the two. A media-type
// entry carries a schema *and* the media type serializing it, and both have IR
// homes at these positions — HTTPParamBinding.ContentType and
// Property.Encoding.MediaType — so electing it keeps in modelled form what
// electing `schema` would push into an opaque Unmodeled payload: a declared wire
// fact the IR does hold. The specification is no help in choosing: 3.1 names
// `schema` first in the very sentence forbidding both and 3.2 names `content`
// first, and a prohibition states no precedence in either order.
//
// The rule is unconditional, so it holds even where the elected entry states no
// schema of its own: `{schema: {type: integer}, content: {application/json: {}}}`
// lowers to `any` with the integer kept beside it, rather than to the integer.
// Electing per entry instead would recover that one case and cost the property
// the election exists for — two documents alike but for whether an entry names a
// schema would elect different spellings, which is the shape of the bug being
// fixed. Both keywords together is a document OpenAPI forbids; what matters is
// that neither is dropped and the choice does not turn on how it was written.
//
// The two positions used to disagree, and only one of the orders was a decision:
// fillParamType read `content` first from the start, while the header path read
// `schema` first because it read nothing else until a content arm was appended
// below it (GitHub #139). One order now governs both (GitHub #320).
func electTypeSpelling(c lowering.Ctx, js *oas3.JSONSchema[oas3.Referenceable],
	content *sequencedmap.Map[string, *soa.MediaType], root *yaml.Node, at jsontext.Pointer,
	carrier, home string,
) (typeSpelling, []ir.Diagnostic) {
	// A content parameter or header declares exactly one media type;
	// singleContentEntry takes it and reports a document that declares more,
	// rather than dropping the extras in silence (GitHub #139).
	mt, media, ok, diags := singleContentEntry(c, content, at)
	if ok {
		mediaPtr := at + ids.Ptr("content", mt)
		scope := ids.Scope("content", mt)
		examples, residue, residueDiags := contentEntryFields(c, media, mediaPtr, scope, carrier, home)
		diags = append(diags, residueDiags...)
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
// records nothing and says nothing: RawChildNode returns nil for an absent
// keyword and PreserveNode keeps nothing for a nil node.
//
// ReasonDegradedLowering, as recordSkippedFamilies uses for the keyword families
// its own election passes over — the position lowered to one of two co-declared
// forms with the other kept beside it. Warning rather than the info announcing a
// conjunction JSON Schema allows, because this is one OpenAPI forbids: the same
// severity singleContentEntry reports a content map of more than one entry at,
// for the same reason. Not an error, since the document lowers as well as an
// election can make it and harness.Check stops at the first error diagnostic,
// which would hide every later finding in the same spec.
func passedOverSpelling(c lowering.Ctx, u *ir.Unmodeled, root *yaml.Node, passed, elected string, at jsontext.Pointer) []ir.Diagnostic {
	pointer := at + ids.Ptr(passed)
	kept, diags := schema.PreserveNode(c, u, "openapi:"+passed,
		annotation.RawChildNode(root, passed), ir.ReasonDegradedLowering, pointer)
	if !kept {
		return diags
	}
	return append(diags, c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, pointer,
		"a parameter or header declares either schema or content, not both; this one declares "+
			"both, so it lowered as its %s, with %s kept verbatim under Unmodeled", elected, passed))
}

// singleContentEntry returns the one entry a content-style header or parameter
// declares, and reports a document declaring more than one.
//
// OpenAPI requires exactly one entry at both positions, so only the first can
// lower — ir.Property and ir.Parameter each hold a single type. Taking it in
// silence dropped a declared schema without a word, which is the loss GitHub #139
// fixed at this position in its other spelling; the extras are named instead so
// the document's own error is visible rather than absorbed.
func singleContentEntry(c lowering.Ctx, content *sequencedmap.Map[string, *soa.MediaType], at jsontext.Pointer) (string, *soa.MediaType, bool, []ir.Diagnostic) {
	if content == nil || content.Len() == 0 {
		return "", nil, false, nil
	}
	var first string
	var chosen *soa.MediaType
	ignored := make([]string, 0, content.Len()-1)
	for mt, media := range content.All() {
		if chosen == nil {
			first, chosen = mt, media
			continue
		}
		ignored = append(ignored, mt)
	}
	var diags []ir.Diagnostic
	if len(ignored) > 0 {
		diags = append(diags, c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, at+ids.Ptr("content"),
			"a content-style header or parameter must declare exactly one media type; "+
				"%q is lowered and %s ignored", first, strings.Join(ignored, ", ")))
	}
	return first, chosen, chosen != nil, diags
}

// applyHeaderAnnotations overlays the annotations the header object writes on
// itself onto p, after its schema's. A header carries both, and the header's
// own are the more specific of the two — they describe this header rather than
// the type it happens to be.
func applyHeaderAnnotations(c lowering.Ctx, p *ir.Property, h *soa.Header, hdecl jsontext.Pointer) []ir.Diagnostic {
	if d := h.GetDescription(); d != "" {
		p.Docs.Description = d
	}
	if h.GetDeprecated() {
		p.Deprecation = &ir.Deprecation{}
	}
	ex, diags := exampleList(c, h.GetExample(), h.GetExamples(), hdecl)
	if len(ex) > 0 {
		p.Examples = ex
	}
	hExt, extDiags := schema.ExtensionsOf(c, h.GetExtensions(), hdecl)
	diags = append(diags, extDiags...)
	p.Unmodeled = annotation.MergeUnmodeled(p.Unmodeled, hExt)
	diags = append(diags, annotation.UnknownKeysIn(&p.Unmodeled, h, c.ProvenanceAt, hdecl)...)
	return append(diags, c.PromoteDeprecation(p.Unmodeled, p.Deprecation, &p.Provenance)...)
}

// exampleList lowers a single example node and a plural example map into value
// examples, in source order. An unconvertible node is skipped with a warning
// diagnostic rather than silently. The singular `example` keyword is a bare
// value with nowhere to hang a name or summary, so it lowers to a value alone.
func exampleList(c lowering.Ctx, single *yaml.Node, plural *sequencedmap.Map[string, *soa.ReferencedExample], pointer jsontext.Pointer) ([]ir.Example, []ir.Diagnostic) {
	var out []ir.Example
	var diags []ir.Diagnostic
	if single != nil {
		var exDiags []ir.Diagnostic
		out, exDiags = schema.AppendExample(c, out, ir.Example{}, single, pointer, "example")
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
// entry never had; an inline entry is stamped at its own `value`. Only this hop
// is de-referenced: an enclosing $ref'd response or parameter is already
// flattened into pointer.
func appendPluralExample(c lowering.Ctx, out []ir.Example, re *soa.ReferencedExample, pointer jsontext.Pointer, name string) ([]ir.Example, []ir.Diagnostic) {
	// The declaration pointer, not the entry's, is where an Example Object's own
	// keywords are written: a $ref entry holds none of them. ir.Example carries an
	// Unmodeled map, so neither the object's x-* nor its undeclared keys need a
	// scope.
	ex, decl := resolve.ObjectAt[soa.Example](c.RefScope(), re, pointer+ids.Ptr("examples", name))
	if ex == nil {
		return out, nil
	}
	ext, diags := schema.ExtensionsOf(c, ex.GetExtensions(), decl)
	diags = append(diags, annotation.UnknownKeysIn(&ext, ex, c.ProvenanceAt, decl)...)
	// An entry written as a Reference Object carries its summary and description
	// beside the $ref, and they override the declaration's; the fold reads the two
	// through the same helper the other positions use (GitHub #610).
	docs := resolve.RefDocs(re, ir.Docs{Summary: ex.GetSummary(), Description: ex.GetDescription()})
	proto := ir.Example{
		Name:        name,
		Summary:     docs.Summary,
		Description: docs.Description,
		ExternalURL: ex.GetExternalValue(),
		Unmodeled:   ext,
	}
	out, exDiags := appendExampleValue(c, out, proto, ex, re, pointer, name)
	return out, append(diags, exDiags...)
}

// appendExampleValue appends the entry's value under the annotations proto
// already carries, choosing the spelling the entry wrote it with: the 3.1
// `value`, the 3.2 `dataValue` — the same example in data form, and the
// parser's values.Value is a yaml node, so it lowers and is diagnosed exactly as
// `value` is — the spec-legal `externalValue`, and last the 3.2 `serializedValue`
// beside that.
func appendExampleValue(c lowering.Ctx, out []ir.Example, proto ir.Example, ex *soa.Example,
	re *soa.ReferencedExample, pointer jsontext.Pointer, name string,
) ([]ir.Example, []ir.Diagnostic) {
	if node := ex.GetValue(); node != nil {
		return appendExampleData(c, out, proto, re, node, pointer, name, "value")
	}
	// dataValue is not dropped: it carries the example's data, ir.Example.Value is
	// its home, and the parser models the field so the census never saw it either
	// (GitHub #612).
	if data := ex.GetDataValue(); data != nil {
		return appendExampleData(c, out, proto, re, data, pointer, name, "dataValue")
	}
	if proto.ExternalURL != "" {
		return append(out, proto), nil
	}
	return appendSerializedExample(c, out, proto, ex, pointer, name)
}

// appendExampleData lowers one example node under keyword — the spelling it was
// written with — stamping the failure pointer where the value is written: at the
// reference site for a $ref entry, which holds no node of its own, and at its own
// keyword for an inline one.
func appendExampleData(c lowering.Ctx, out []ir.Example, proto ir.Example,
	re *soa.ReferencedExample, node *yaml.Node, pointer jsontext.Pointer, name, keyword string,
) ([]ir.Example, []ir.Diagnostic) {
	if re.IsReference() {
		return schema.AppendExample(c, out, proto, node, pointer, "examples", name)
	}
	return schema.AppendExample(c, out, proto, node, pointer, "examples", name, keyword)
}

// appendSerializedExample records an entry that declares no value, no dataValue
// and no externalValue. The 3.2 `serializedValue` is a single-format
// serialization spelling of the example the entry's own data form would carry,
// and ir.Example has no field for it: a typed field would put a format-specific
// representation on a neutral node with no other consumer, so it is kept
// verbatim with ReasonNoIRHome and announced, which leaves the promotion path
// open (GitHub #612, ir-design §12).
//
// The node's presence is the decision, not the getter: keeping the raw node is
// what makes the entry survive at all, and an entry that declares none of the
// four spells a genuinely empty stub, which keeps today's warning. An entry
// that reached here declaring one the raw mapping does not present — a key
// merged in through `<<` — is reported by PreserveNode itself.
func appendSerializedExample(c lowering.Ctx, out []ir.Example, proto ir.Example, ex *soa.Example,
	pointer jsontext.Pointer, name string,
) ([]ir.Example, []ir.Diagnostic) {
	at := pointer + ids.Ptr("examples", name, "serializedValue")
	kept, diags := schema.PreserveNode(c, &proto.Unmodeled, "openapi:serializedValue",
		annotation.RawChildNode(ex.GetRootNode(), "serializedValue"), ir.ReasonNoIRHome, at)
	if !kept {
		return out, append(diags, c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct,
			pointer+ids.Ptr("examples", name), "example declares neither value nor externalValue"))
	}
	return append(out, proto), append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
		"serializedValue has no ir.Example home; kept verbatim under Unmodeled"))
}

// lowerRequestBody lowers an operation's request body onto op.Request and the
// binding's RequestContentTypes. Body optionality lands on Payload.Required,
// always set here because OpenAPI always states it — an undeclared `required`
// means false by the specification's own default, not silence, so leaving the
// field nil would report the format as unable to express optionality. opDeclPtr
// is the operation's own declaration pointer, so a $ref'd body interns its
// content once at its component pointer rather than once per mount site
// (issue #107) — and under the component's name, since the operationId hint
// would otherwise name the shared node after one arbitrary referencing site.
//
// A body $ref'd from anywhere else takes the second half of that rule: the
// pointer is some other operation's, which DeclarationHint has no name for, so
// the lowering names by reference and leaves the owning operation to settle it
// (GitHub #433).
func lowerRequestBody(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, op *ir.Operation, hb *ir.HTTPBinding, src *soa.Operation, opDeclPtr jsontext.Pointer) []ir.Diagnostic {
	usePtr := opDeclPtr + ids.Ptr("requestBody")
	rb, bodyPtr := resolve.ObjectAt[soa.RequestBody](c.RefScope(), src.GetRequestBody(), usePtr)
	if rb == nil {
		return nil
	}
	// requestBodyHint spells this operation's ID, which names the shared node
	// after this mount when the body is declared under another operation. Marking
	// the lowering lets that operation's own pass replace the placeholder, in
	// whichever order the two run (GitHub #433).
	payload, diags := lowerPayload(c.NamingByReferenceAt(usePtr, bodyPtr), ts, anchors, rb.GetContent(),
		bodyPtr, ids.DeclarationHint(bodyPtr, requestBodyHint(src)))
	if payload == nil {
		return diags
	}
	// The body's own documentation describes the body rather than any media type
	// inside it, and ir.Payload is where a request body's facts land. The parser
	// models the field, so the unknown-key census never saw it either: a
	// `description` here reached no field, no Unmodeled entry and no diagnostic
	// (GitHub #609). A summary or description written beside a `$ref` to the body
	// overrides the declaration's, through the same fold every other position uses
	// (GitHub #610). Written only when something landed, so a body stating neither
	// keeps Docs nil — the same three-state reading Required takes.
	bodyDocs := resolve.RefDocs(src.GetRequestBody(), ir.Docs{Description: rb.GetDescription()})
	if bodyDocs.Summary != "" || bodyDocs.Description != "" {
		payload.Docs = &bodyDocs
	}
	required := rb.GetRequired()
	payload.Required = &required
	// soa.RequestBody exposes no GetExtensions at this library version, so the
	// field is read directly — as XMLHints already reads its own. Both reads sit
	// after the payload guard because ir.Payload is the body's only carrier: a
	// request body declaring no content lowers to nothing to hang them on, and
	// OpenAPI makes content REQUIRED there, so such a body is a defect in the
	// document rather than a shape this compiler has to place.
	bodyExt, bodyExtDiags := schema.ExtensionsOf(c, rb.Extensions, bodyPtr)
	payload.Unmodeled = annotation.MergeUnmodeled(payload.Unmodeled, bodyExt)
	diags = append(diags, bodyExtDiags...)
	diags = append(diags, annotation.UnknownKeysIn(&payload.Unmodeled, rb, c.ProvenanceAt, bodyPtr)...)
	op.Request = payload
	hb.RequestContentTypes = contentTypeKeys(rb.GetContent())
	return diags
}

// contentTypeKeys returns a content map's media-type keys in source order —
// the request content priority order.
func contentTypeKeys(content *sequencedmap.Map[string, *soa.MediaType]) []string {
	if content == nil || content.Len() == 0 {
		return nil
	}
	keys := make([]string, 0, content.Len())
	for mt := range content.All() {
		keys = append(keys, mt)
	}
	return keys
}

// requestBodyHint derives an anonymous-type naming hint for a request body from
// the operationId, falling back to "request".
func requestBodyHint(src *soa.Operation) string {
	if id := src.GetOperationID(); id != "" {
		return id + "_request"
	}
	return "request"
}

// splitContentTypes splits an encoding contentType value on "," into trimmed,
// non-empty media types.
func splitContentTypes(raw string) []string {
	if raw == "" {
		return nil
	}
	var out []string
	for part := range strings.SplitSeq(raw, ",") {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// isFormContent reports whether a media type is multipart or url-encoded form
// content, whose parts carry per-property encoding.
func isFormContent(mt string) bool {
	return strings.HasPrefix(mt, "multipart/") || mt == "application/x-www-form-urlencoded"
}

// isBinaryBody reports whether a content entry is a binary file body: a
// string+binary schema, or an absent schema on application/octet-stream.
func isBinaryBody(mt string, js *oas3.JSONSchema[oas3.Referenceable]) bool {
	s := schemaOf(js)
	if s == nil {
		return mt == "application/octet-stream"
	}
	return schemaIsBinary(s)
}

// schemaIsBinary reports whether a schema is a string+binary body.
func schemaIsBinary(s *oas3.Schema) bool {
	return s.GetFormat() == "binary" && schemaHasType(s, oas3.SchemaTypeString)
}

// schemaIsFilePart reports whether a multipart part schema is a file
// (string+binary or string+byte).
func schemaIsFilePart(s *oas3.Schema) bool {
	f := s.GetFormat()
	if f != "binary" && f != "byte" {
		return false
	}
	return schemaHasType(s, oas3.SchemaTypeString)
}

// schemaIsArray reports whether a schema declares the array type (a repeated
// multipart part).
func schemaIsArray(s *oas3.Schema) bool {
	return schemaHasType(s, oas3.SchemaTypeArray)
}

// schemaHasType reports whether a schema's declared type set contains st.
func schemaHasType(s *oas3.Schema, st oas3.SchemaType) bool {
	return slices.Contains(s.GetType(), st)
}

// schemaOf returns the concrete Schema of a schema-or-ref-or-bool position,
// following a $ref to its resolved target so binary/file detection and part
// enumeration see the referent's type and properties rather than the bare ref
// (which carries none). It returns nil for a boolean schema or an absent one.
func schemaOf(js *oas3.JSONSchema[oas3.Referenceable]) *oas3.Schema {
	if js == nil || !js.IsSchema() {
		return nil
	}
	s := js.GetSchema()
	if s != nil && s.Ref != nil {
		if resolved := js.GetResolvedSchema(); resolved != nil {
			return resolved.GetSchema()
		}
	}
	return s
}

// maxBodyAliasHops bounds the alias chain bodyModelPointer follows. A $ref cycle
// is refused at load, so no source document can spell a chain that long — the
// bound is what keeps the walk terminating without relying on that.
const maxBodyAliasHops = 64

// bodyModelPointer returns the pointer the model behind body was interned at,
// following the alias scalars a $ref-with-siblings position hoists to reach it.
// That pointer is the one whose /properties/<name> children minted the model's
// PropIDs, so an encoding key derived from it addresses a property that exists.
//
// It reports ok=false for a body that stands for no model at all — a primitive,
// an enum, an opaque scalar — where no pointer would name a property either.
func bodyModelPointer(ts *compile.Types, body ir.TypeID) (jsontext.Pointer, bool) {
	id := body
	for range maxBodyAliasHops {
		td, found := ts.Node(id)
		if !found {
			return "", false
		}
		switch t := td.(type) {
		case *ir.Model:
			return t.Provenance.Pointer, true
		case *ir.Scalar:
			if t.Base == nil {
				return "", false
			}
			id = t.Base.Target
		default:
			return "", false
		}
	}
	return "", false
}

// bodySchemaPointer returns the JSON pointer under which a body schema's
// properties were interned: the ref target's pointer when
// resolve.Scope.InternalPointer reads one from the media schema's $ref, else
// localPtr.
//
// It is the fallback for a body the IR gives no model for (bodyModelPointer),
// where the schema declares properties that nothing in the IR holds — a
// contradictory `enum` or scalar `type` beside them — and no pointer can name a
// property that was never lowered.
//
// The document half of the $ref decides, via resolve.Scope.InternalPointer, rather than being
// cut off and discarded. A fragment lifted from a ref into another document
// would otherwise become an identity in *this* one, naming whichever local
// schema happened to share the path — a property of a different document
// addressed as if it were ours. localPtr is the honest fallback there: it is
// the position the reference itself occupies here. It is for a fragment that
// decodes to bytes that are not UTF-8 too, which no key here spells and no
// PropID can carry (GitHub #520).
func bodySchemaPointer(c lowering.Ctx, js *oas3.JSONSchema[oas3.Referenceable], localPtr jsontext.Pointer) jsontext.Pointer {
	if js == nil || !resolve.IsRefSite(js, js.GetSchema()) {
		return localPtr
	}
	if pointer, ok := c.RefScope().InternalPointer(js.GetRef().String()); ok {
		return pointer
	}
	return localPtr
}

// partEncodingEmpty reports whether a PartEncoding carries no information and can
// be omitted from the encoding map.
func partEncodingEmpty(pe ir.PartEncoding) bool {
	return len(pe.ContentTypes) == 0 && len(pe.Headers) == 0 &&
		!pe.Multi && !pe.Filename && pe.Style == "" && pe.Explode == nil
}
