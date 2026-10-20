// Package operation lowers what a document says an API does: its path items,
// webhooks and callbacks, the parameters merged onto each operation, and the
// content of every request body, response and header.
//
// It sits above the schema walk, which it reaches for every type position, and
// above auth, for the requirements an operation names. It reaches nothing above
// itself: the compiler assembles a Document from what LowerService returns.
//
// The recursion is callbacks, each a path item holding operations of its own,
// which is why lowerOperation, lowerCallbacks, lowerCallbackOps and
// mountPathItem cannot be separated. internal/archtest pins that set.
package operation

import (
	"encoding/json/jsontext"
	"slices"
	"strings"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
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
		one, contentDiags := lowerContent(c, ts, anchors, mt, media, pointer, hint)
		diags = append(diags, contentDiags...)
		payload.Contents = append(payload.Contents, one)
	}
	if len(payload.Contents) == 0 {
		return nil, diags
	}
	return payload, diags
}

// keepMediaTypeFields keeps what OpenAPI 3.2 lets a Media Type Object write
// and the parser's model names no field for, at the census's own entries, so
// the census does not call it undefined: a `$ref` (keepMediaTypeRef), or else a
// description (keepMediaTypeDescription). Both are read at every version, as
// load reports them below 3.2 (diag.LaterVersionConstruct).
func keepMediaTypeFields(c lowering.Ctx, u *ir.Unmodeled, media *soa.MediaType, mediaPtr jsontext.Pointer, scope string) []ir.Diagnostic {
	root := media.GetRootNode()
	if ref := annotation.RawChildNode(root, "$ref"); ref != nil {
		return keepMediaTypeRef(c, u, root, ref, mediaPtr, scope)
	}
	return keepMediaTypeDescription(c, u, root, mediaPtr, scope)
}

// keepMediaTypeDescription keeps a Media Type Object's description verbatim,
// with an info. ir.Content has no docs field, hence ReasonNoIRHome.
func keepMediaTypeDescription(c lowering.Ctx, u *ir.Unmodeled, root *yaml.Node, mediaPtr jsontext.Pointer, scope string) []ir.Diagnostic {
	at := mediaPtr + ids.Ptr("description")
	kept, diags := schema.PreserveNode(c, u, annotation.EntryKey(scope, "description"),
		annotation.RawChildNode(root, "description"), ir.ReasonNoIRHome, at)
	if !kept {
		return diags
	}
	return append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
		"a media type's description has no IR home; kept verbatim under Unmodeled"))
}

// keepMediaTypeRef keeps a content entry written as a Reference Object, which
// this compiler does not follow yet (GitHub #856): ref, the `$ref` written on
// root, verbatim at the census's own entry, with a warning that the entry
// lowers as written, and the summary and description a Reference Object may
// write beside it verbatim too.
func keepMediaTypeRef(c lowering.Ctx, u *ir.Unmodeled, root, ref *yaml.Node, mediaPtr jsontext.Pointer, scope string) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, sibling := range []string{"summary", "description"} {
		_, siblingDiags := schema.PreserveNode(c, u, annotation.EntryKey(scope, sibling),
			annotation.RawChildNode(root, sibling), ir.ReasonNoIRHome, mediaPtr+ids.Ptr(sibling))
		diags = append(diags, siblingDiags...)
	}
	at := mediaPtr + ids.Ptr("$ref")
	kept, refDiags := schema.PreserveNode(c, u, annotation.EntryKey(scope, "$ref"), ref, ir.ReasonDegradedLowering, at)
	diags = append(diags, refDiags...)
	if !kept {
		return diags
	}
	return append(diags, c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, at,
		"a media type written as a $ref is not followed by this compiler yet (GitHub #856); the entry "+
			"lowers as written, and the reference is kept verbatim under Unmodeled"))
}

// lowerContent lowers one media-type view: its type graph, examples, binary/
// form specialization, sequential-media shape, and extensions.
func lowerContent(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, mt string, media *soa.MediaType, pointer jsontext.Pointer, hint string) (ir.Content, []ir.Diagnostic) {
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
	diags = append(diags, keepMediaTypeFields(c, &content.Unmodeled, media, mediaPtr, "")...)
	return content, append(diags,
		annotation.UnknownKeysIn(&content.Unmodeled, media, c.ProvenanceAt, mediaPtr)...)
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
	walk := walkBodyParts(media.GetSchema())
	parts := walk.parts
	cutDiags := diagPartWalkCut(c, walk, mediaPtr+ids.Ptr("schema"))
	if len(parts) == 0 {
		return nil, nil, cutDiags
	}
	// Key by the pointer the body model was interned at, not by mediaPtr, so the
	// keys align with that model's property IDs (invariant 2/3). Asking the IR is
	// what keeps this in step with schemaOf, which reads the end of a $ref chain
	// while a pointer cut from the ref string names only its first hop.
	schemaPtr, ok := bodyModelPointer(ts, body)
	if !ok {
		schemaPtr = bodySchemaPointer(c, media.GetSchema(), mediaPtr+ids.Ptr("schema"))
	}
	diags := cutDiags
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

// maxPartRefHops bounds how many $ref targets bodyParts follows down a
// composition. Inline wrappers are not counted: schema.FlattenedAllOf reads them,
// with the cap the lowering uses. A target is entered again only when a shorter
// path reaches it, so an allOf cycle (GitHub #642) costs one visit, and the
// bound is reported only for a target no path reaches within it.
const maxPartRefHops = 16

// partWalk is what bodyParts found, and which bound cut it short.
type partWalk struct {
	parts []bodyPart
	// capped marks an inline branch nested past schema.AllOfFlattenDepth whose
	// own allOf entries no path entered.
	capped bool
	// hopsSpent marks a $ref target past maxPartRefHops that no path entered.
	hopsSpent bool
}

// partVisits is what one body's part walk has met: the fewest hops each target
// was entered at, and the targets met past a bound.
type partVisits struct {
	entered map[*oas3.Schema]int
	cut     map[*oas3.Schema]bool
	// capped holds the allOf entries of an inline branch past the flattening
	// depth, which the walk did not follow.
	capped map[*oas3.Schema]bool
}

// markCapped records the allOf entries of b, an inline branch past the
// flattening depth, as targets the walk did not follow.
func (v partVisits) markCapped(b *oas3.Schema) {
	for _, c := range b.GetAllOf() {
		if cs := schemaOf(c); cs != nil {
			v.capped[cs] = true
		}
	}
}

// walkBodyParts runs bodyParts over one body and reports a bound only for a
// target that no path entered.
func walkBodyParts(js *oas3.JSONSchema[oas3.Referenceable]) partWalk {
	v := partVisits{
		entered: map[*oas3.Schema]int{},
		cut:     map[*oas3.Schema]bool{},
		capped:  map[*oas3.Schema]bool{},
	}
	w := bodyParts(js, 0, v)
	for s := range v.cut {
		if _, ok := v.entered[s]; !ok {
			w.hopsSpent = true
		}
	}
	for s := range v.capped {
		if _, ok := v.entered[s]; !ok {
			w.capped = true
		}
	}
	return w
}

// bodyParts returns the parts a multipart body declares, in source order: the
// properties written on the schema itself, then those each allOf branch
// contributes, with the first declaration of a name winning. The branches are
// the lowering's own (schema.FlattenedAllOf); a $ref branch is followed here, up
// to maxPartRefHops. v records the fewest hops each target was entered at: one
// met again by no shorter path yields no parts. The bounds that cut the walk are
// recorded on v.
//
// A body that composes rather than declares — `allOf: [{$ref: Form}]` — still
// enumerates the same parts (GitHub #140).
func bodyParts(js *oas3.JSONSchema[oas3.Referenceable], hops int, v partVisits) partWalk {
	s := schemaOf(js)
	if s == nil {
		return partWalk{}
	}
	if hops > maxPartRefHops {
		v.cut[s] = true
		return partWalk{}
	}
	if prev, ok := v.entered[s]; ok && prev <= hops {
		return partWalk{}
	}
	v.entered[s] = hops
	w := partWalk{parts: propertyParts(s)}
	for _, br := range schema.FlattenedAllOf(s) {
		if br.Ref {
			sub := bodyParts(br.Schema, hops+1, v)
			w.parts = append(w.parts, sub.parts...)
			continue
		}
		w.parts = append(w.parts, propertyParts(schemaOf(br.Schema))...)
		if br.Capped {
			v.markCapped(schemaOf(br.Schema))
		}
	}
	w.parts = dedupeParts(w.parts)
	return w
}

// propertyParts returns the properties s declares itself, in source order.
func propertyParts(s *oas3.Schema) []bodyPart {
	if s == nil || s.GetProperties() == nil {
		return nil
	}
	var out []bodyPart
	for name, pjs := range s.GetProperties().All() {
		out = append(out, bodyPart{name: name, schema: pjs})
	}
	return out
}

// diagPartWalkCut reports the bounds that cut a body's part walk short, so an
// encoding entry for a part past one is not dropped without a word.
func diagPartWalkCut(c lowering.Ctx, w partWalk, at jsontext.Pointer) []ir.Diagnostic {
	var out []ir.Diagnostic
	if w.capped {
		out = append(out, c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, at,
			"multipart body composition is nested past the flattening depth of %d, so a part reached only below it has no encoding entry",
			schema.AllOfFlattenDepth))
	}
	if w.hopsSpent {
		out = append(out, c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, at,
			"multipart body composition follows $refs past %d hops, so a part reached only beyond them has no encoding entry",
			maxPartRefHops))
	}
	return out
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
// The IR is asked first because §4.3 stores only a model's own properties: a
// composed body holds its parts on the Base it inherits them from, so a key
// derived from the composed node's pointer would name a property that exists
// nowhere. Deriving is the answer for a body with no model in the IR, such as a
// schema declaring properties beside an enum or scalar type.
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
// IR holds: `allowReserved`, which ir.PartEncoding has no field for, the
// object's own x-*, and the keys the specification defines for no encoding at
// all. Otherwise documents differing only in those compile to one IR (GitHub
// #291).
//
// Entries ride on the owning ir.Content, since PartEncoding carries no
// Unmodeled map, keyed under scope: "encoding/<part>" or "itemEncoding". One
// content can hold several such entries in the same map, so the part tells them
// apart.
//
// allowReserved carries ReasonNoIRHome and announces itself only when something
// was written, as preserveHeaderSerialization does.
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

// nestedEncodings keeps the nested Encoding Objects OpenAPI 3.2 lets an
// encoding write, verbatim under the scope the part's own entries ride on, one
// info each (GitHub #615). Recording them at the census's own key and pointer
// keeps the census from calling them undefined, at every version, as load
// reports them below 3.2 (diag.LaterVersionConstruct).
//
// ReasonNoIRHome, since PartEncoding could grow the fields. Nothing is lowered:
// a part inside a part has no shape here, and keeping the source is lossless.
func nestedEncodings(c lowering.Ctx, out *ir.Unmodeled, enc *soa.Encoding, encPtr jsontext.Pointer, scope string) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, keyword := range nestedEncodingFields {
		at := encPtr + ids.Ptr(keyword)
		kept, keptDiags := schema.PreserveNode(c, out, annotation.EntryKey(scope, keyword),
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
		p, headerDiags := lowerHeader(lowering.Within[soa.Header](c, rh), ts, anchors, h, name, hptr, hdecl)
		diags = append(diags, headerDiags...)
		diags = append(diags, reservedHeaderEntryDiag(c, name, hptr)...)
		// A summary or description beside a $ref of the entry's chain documents
		// this header rather than the declaration it names (GitHub #610).
		p.Docs = resolve.UseDocs[soa.Header](rh, p.Docs)
		out = append(out, p)
	}
	return out, diags
}

// reservedHeaderEntryDiag reports a headers-map entry OpenAPI says SHALL be
// ignored. Both maps this lowering serves, a response's (§4.8.17) and an
// encoding's (§4.8.15), reserve Content-Type and nothing else, because each has
// its own place to state a media type. The comparison is case-insensitive, as
// HTTP field names are.
//
// It reports at the entry's own pointer, not the declaration's: the reserved
// thing is the key the header is mapped under, so of two keys $ref'ing one
// component only the one spelled Content-Type is reserved.
//
// This is the headers-map half of the rule; reservedHeaderParamDiag is the
// parameter half. The header still lowers (see diag.ReservedHeaderName).
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
	// when the header is declared under another response (GitHub #433). A
	// component header has no owning response, so a schema's $ref into it leaves
	// the subtree named by that reference (GitHub #747).
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
	diags = append(diags, applyHeaderAnnotations(c, &p, h, hdecl, elected.examples)...)
	return p, append(diags, preserveHeaderSerialization(c, &p, h, hdecl)...)
}

// preserveHeaderSerialization keeps the two serialization controls a header
// object declares. OpenAPI §4.8.21 lets a header write `style` and `explode`,
// and explode governs how an array or object value is written on the wire, but
// ir.Property has a field for neither. ir.PartEncoding does, but that is a
// multipart part's config, and ir.Encoding names a value-encoding scheme, not a
// style. They are kept verbatim with ReasonNoIRHome, since the IR can close the
// gap by adding the fields, as with a parameter's xml hints.
//
// A header that declares neither records nothing.
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

// positionExamples returns the examples a parameter or header writes for
// itself: its own, then those of the media type its content spelling elected.
// Together they replace the schema's, as OpenAPI says a position's example
// overrides its schema's, but neither replaces the other: OpenAPI orders them
// nowhere, and keeping one would drop the other in silence (GitHub #611).
func positionExamples(own, media []ir.Example) []ir.Example {
	return slices.Concat(own, media)
}

// writtenField is one field an object writes, with the parser's own reading of
// the node it was written at: for a value merged in through `<<`, the merged
// node, which a read of the raw mapping does not find.
type writtenField struct {
	keyword string
	node    *yaml.Node
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

// contentEntryFields returns everything a Media Type Object declares at a
// parameter's or header's elected `content` position beyond the one type that
// position lowers: its example/examples, its x-* and undeclared keys, and the
// fields contentOnlyFields names. Nothing read them before, so they vanished
// without a diagnostic (GitHub #611).
//
// scope is the content entry's own path, so several media types and the
// enclosing object cannot collide on one key. carrier and home name the
// position in each content-only field's info, with ReasonNoIRHome.
func contentEntryFields(c lowering.Ctx, media *soa.MediaType, mediaPtr jsontext.Pointer,
	scope, carrier, home string,
) ([]ir.Example, ir.Unmodeled, []ir.Diagnostic) {
	examples, diags := exampleList(c, media.GetExample(), media.GetExamples(), mediaPtr)
	var unmodeled ir.Unmodeled
	ext, extDiags := schema.ExtensionsIn(c, media.GetExtensions(), mediaPtr, scope)
	unmodeled = annotation.MergeUnmodeled(unmodeled, ext)
	diags = append(diags, extDiags...)
	for _, field := range contentOnlyFields(media) {
		at := mediaPtr + ids.Ptr(field.keyword)
		kept, keptDiags := schema.PreserveNode(c, &unmodeled,
			annotation.EntryKey(scope, field.keyword), field.node, ir.ReasonNoIRHome, at)
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
// records nothing and says nothing.
//
// The reason is ReasonDegradedLowering, as recordSkippedFamilies uses for the
// keyword families its own election passes over. The severity is warning, not
// the info announcing a conjunction JSON Schema allows, because OpenAPI forbids
// this one; it matches singleContentEntry's. It is not an error: the document
// lowers as well as an election can make it, and harness.Check stops at the
// first error diagnostic, hiding later findings.
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
// the type it happens to be. media is what its elected content entry wrote.
func applyHeaderAnnotations(c lowering.Ctx, p *ir.Property, h *soa.Header, hdecl jsontext.Pointer, media []ir.Example) []ir.Diagnostic {
	if d := h.GetDescription(); d != "" {
		p.Docs.Description = d
	}
	if h.GetDeprecated() {
		p.Deprecation = &ir.Deprecation{}
	}
	own, diags := exampleList(c, h.GetExample(), h.GetExamples(), hdecl)
	if ex := positionExamples(own, media); len(ex) > 0 {
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
	out, exDiags := appendExampleValue(c, out, proto, ex, re, pointer, decl, name)
	return out, append(diags, exDiags...)
}

// appendExampleValue appends the entry under the annotations proto already
// carries. Its data lowers into Value from the spelling it was written in,
// `value` or 3.2's `dataValue` (GitHub #612). A spelling ir.Example has no field
// for is kept verbatim at decl, the entry's declaration, rather than dropped:
// 3.2's `serializedValue` whatever it is written beside, and a `dataValue`
// written beside `value`, which the parser reports. An entry with no value in
// any spelling is an empty stub, dropped with a warning.
func appendExampleValue(c lowering.Ctx, out []ir.Example, proto ir.Example, ex *soa.Example,
	re *soa.ReferencedExample, pointer, decl jsontext.Pointer, name string,
) ([]ir.Example, []ir.Diagnostic) {
	serialized, diags := keepExampleSpelling(c, &proto.Unmodeled, ex.GetCore().SerializedValue.ValueNode,
		"serializedValue", ir.ReasonNoIRHome, decl, "serializedValue has no ir.Example home")
	value, data := ex.GetValue(), ex.GetDataValue()
	switch {
	case value != nil:
		if data != nil {
			_, dataDiags := keepExampleSpelling(c, &proto.Unmodeled, data, "dataValue",
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
		return schema.AppendExample(c, out, proto, node, pointer, "examples", name)
	}
	return schema.AppendExample(c, out, proto, node, pointer, "examples", name, keyword)
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
	kept, diags := schema.PreserveNode(c, u, annotation.EntryKey("", keyword), node, reason, at)
	if !kept {
		return false, diags
	}
	return true, append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
		"%s; kept verbatim under Unmodeled", why))
}

// lowerRequestBody lowers an operation's request body onto op.Request and the
// binding's RequestContentTypes. Payload.Required is always set, because an
// undeclared `required` means false by the specification's own default, and nil
// would report the format as unable to express optionality.
//
// opDeclPtr is the operation's own declaration pointer. A $ref'd body interns
// its content once at its declaration, not once per mount site, under the
// component's name rather than the operationId hint (issue #107). A body $ref'd
// from another operation has no component name, so the lowering names by
// reference and leaves the owning operation to settle it (GitHub #433).
func lowerRequestBody(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, op *ir.Operation, hb *ir.HTTPBinding, src *soa.Operation, opDeclPtr jsontext.Pointer) []ir.Diagnostic {
	usePtr := opDeclPtr + ids.Ptr("requestBody")
	rb, bodyPtr := resolve.ObjectAt[soa.RequestBody](c.RefScope(), src.GetRequestBody(), usePtr)
	if rb == nil {
		return nil
	}
	c = lowering.Within[soa.RequestBody](c, src.GetRequestBody())
	// requestBodyHint spells this operation's ID, which names the shared node
	// after this mount when the body is declared under another operation. Marking
	// the lowering lets that operation's own pass replace the placeholder, in
	// whichever order the two run (GitHub #433). A component body has no owning
	// operation, so a schema's $ref into it leaves the subtree named by that
	// reference (GitHub #747).
	payload, diags := lowerPayload(c.NamingByReferenceAt(usePtr, bodyPtr), ts, anchors, rb.GetContent(),
		bodyPtr, ids.DeclarationHint(bodyPtr, requestBodyHint(src)))
	if payload == nil {
		// A body lowering no media type is still the body: whether it must be
		// sent, its description and its x-* stay on a Payload with no Contents.
		payload = &ir.Payload{}
		diags = append(diags, emptyContentDiags(c, rb, bodyPtr)...)
	}
	// A Request Body Object's description documents the body rather than any
	// one media type in it (GitHub #609), and a summary or description beside a
	// $ref of its chain documents this use of it (GitHub #610).
	payload.Docs = resolve.UseDocs[soa.RequestBody](src.GetRequestBody(), ir.Docs{Description: rb.GetDescription()})
	payload.Required = new(rb.GetRequired())
	// soa.RequestBody exposes no GetExtensions at this library version, so the
	// field is read directly — as XMLHints already reads its own.
	bodyExt, bodyExtDiags := schema.ExtensionsOf(c, rb.Extensions, bodyPtr)
	payload.Unmodeled = annotation.MergeUnmodeled(payload.Unmodeled, bodyExt)
	diags = append(diags, bodyExtDiags...)
	diags = append(diags, annotation.UnknownKeysIn(&payload.Unmodeled, rb, c.ProvenanceAt, bodyPtr)...)
	op.Request = payload
	hb.RequestContentTypes = contentTypeKeys(rb.GetContent())
	return diags
}

// emptyContentDiags reports rb's content map written empty, at the map, or
// nothing; the design record is on diag.EmptyRequestContent. The condition is
// the parser's own for reading the map as missing, which the load phase drops.
func emptyContentDiags(c lowering.Ctx, rb *soa.RequestBody, bodyPtr jsontext.Pointer) []ir.Diagnostic {
	if !rb.GetCore().Content.Present || rb.GetContent().Len() != 0 {
		return nil
	}
	return []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.EmptyRequestContent, bodyPtr+ids.Ptr("content"),
		"request body's content map is empty, so it names no media type to send the body as")}
}

// contentTypeKeys returns a content map's media-type keys in source order —
// the request content priority order.
func contentTypeKeys(content *sequencedmap.Map[string, *soa.MediaType]) []string {
	if content == nil || content.Len() == 0 {
		return nil
	}
	return slices.Collect(content.Keys())
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
	mt = lowering.NormalizeMediaType(mt)
	return strings.HasPrefix(mt, "multipart/") || mt == "application/x-www-form-urlencoded"
}

// isBinaryBody reports whether a content entry is a binary file body: a
// string+binary schema, or an absent schema on application/octet-stream.
func isBinaryBody(mt string, js *oas3.JSONSchema[oas3.Referenceable]) bool {
	s := schemaOf(js)
	if s == nil {
		return lowering.NormalizeMediaType(mt) == "application/octet-stream"
	}
	return schemaIsBinary(s)
}

// schemaIsBinary reports whether a schema is a string+binary body.
func schemaIsBinary(s *oas3.Schema) bool {
	return s.GetFormat() == "binary" && slices.Contains(s.GetType(), oas3.SchemaTypeString)
}

// schemaIsFilePart reports whether a multipart part schema is a file
// (string+binary or string+byte).
func schemaIsFilePart(s *oas3.Schema) bool {
	f := s.GetFormat()
	if f != "binary" && f != "byte" {
		return false
	}
	return slices.Contains(s.GetType(), oas3.SchemaTypeString)
}

// schemaIsArray reports whether a schema declares the array type (a repeated
// multipart part).
func schemaIsArray(s *oas3.Schema) bool {
	return slices.Contains(s.GetType(), oas3.SchemaTypeArray)
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

// bodySchemaPointer returns the pointer partEncodings keys encoding entries by
// when the body has no model in the IR (bodyModelPointer): the ref target's,
// when resolve.Scope.InternalPointer reads one from the media schema's $ref,
// else localPtr. Such a schema declares properties beside a contradictory
// `enum` or scalar `type`, so no property was interned under this pointer and
// the keys address none (see preserveUnhomedKeywords).
//
// InternalPointer refuses a ref into another document, whose fragment would
// otherwise become an identity in this one and name whichever local schema
// shared the path, and a non-UTF-8 fragment, which no PropID can carry (GitHub
// #520). localPtr applies in both cases.
func bodySchemaPointer(c lowering.Ctx, js *oas3.JSONSchema[oas3.Referenceable], localPtr jsontext.Pointer) jsontext.Pointer {
	if js == nil || !resolve.IsRefSite(js, js.GetSchema()) {
		return localPtr
	}
	if pointer, ok := c.RefScope().TargetPointer(js, js.GetRef().String()); ok {
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
