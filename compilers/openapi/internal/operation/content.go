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
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
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
	kept, diags := annotation.PreserveNodeInto(u, annotation.EntryKey(scope, "description"),
		annotation.RawChildNode(root, "description"), ir.ReasonNoIRHome, c.ProvenanceAt(at))
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
		_, siblingDiags := annotation.PreserveNodeInto(u, annotation.EntryKey(scope, sibling),
			annotation.RawChildNode(root, sibling), ir.ReasonNoIRHome, c.ProvenanceAt(mediaPtr+ids.Ptr(sibling)))
		diags = append(diags, siblingDiags...)
	}
	at := mediaPtr + ids.Ptr("$ref")
	kept, refDiags := annotation.PreserveNodeInto(u, annotation.EntryKey(scope, "$ref"), ref,
		ir.ReasonDegradedLowering, c.ProvenanceAt(at))
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
	case isBinaryBody(c.Bindings(), mt, media.GetSchema()):
		content.File = &ir.FileInfo{IsText: false, ContentTypes: []string{mt}}
		content.Type = ts.PrimRef(ir.PrimBytes)
	case isFormContent(mt):
		enc, encUnmodeled, encDiags := partEncodings(c, ts, anchors, media, mediaPtr, content.Type.Target)
		diags = append(diags, encDiags...)
		if enc != nil {
			content.Encoding = enc
		}
		content.Unmodeled = annotation.MergeUnmodeled(content.Unmodeled, encUnmodeled)
	}
	diags = append(diags, fillSequential(c, ts, anchors, &content, media, mediaPtr, hint)...)
	ext, extDiags := annotation.ExtensionsFrom(media.GetExtensions(), c.ProvenanceAt, mediaPtr)
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
	kept, diags := annotation.PreserveNodeInto(&content.Unmodeled, "openapi:prefixEncoding",
		annotation.RawChildNode(root, "prefixEncoding"), ir.ReasonNoIRHome, c.ProvenanceAt(at))
	_, itemDiags := annotation.PreserveNodeInto(&content.Unmodeled, "openapi:itemEncoding",
		annotation.RawChildNode(root, "itemEncoding"), ir.ReasonNoIRHome, c.ProvenanceAt(mediaPtr+ids.Ptr("itemEncoding")))
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
//
// It returns the map with no keys yet: keys and structural flags read the model
// behind body, which may be a node a $ref promised and nothing has built yet
// (compile.Types.Promise), so partKeys settles both once every node exists. It
// is nil where the body declares no part; one left with no key reads alike.
func partEncodings(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, media *soa.MediaType, mediaPtr jsontext.Pointer, body ir.TypeID) (map[ir.PropID]ir.PartEncoding, ir.Unmodeled, []ir.Diagnostic) {
	walk := walkBodyParts(c.Bindings(), media.GetSchema())
	parts := walk.parts
	cutDiags := diagPartWalkCut(c, walk, mediaPtr+ids.Ptr("schema"))
	if len(parts) == 0 {
		return nil, nil, cutDiags
	}
	diags := cutDiags
	var unmodeled ir.Unmodeled
	encMap := media.GetEncoding()
	fallbackDoc, fallback := bodySchemaPointer(c, media.GetSchema(), mediaPtr+ids.Ptr("schema"))
	keys := partKeys{c: c, body: body, fallbackDoc: fallbackDoc, fallback: fallback}
	for _, part := range parts {
		pe, partUnmodeled, partDiags := buildPartEncoding(c, ts, anchors, part, encMap, mediaPtr)
		diags = append(diags, partDiags...)
		// Kept whether or not the part's encoding ends up empty: an entry declaring
		// only allowReserved or only x-* lowers to an empty PartEncoding that is left
		// out of the map, and what it did declare would go with it.
		unmodeled = annotation.MergeUnmodeled(unmodeled, partUnmodeled)
		keys.parts = append(keys.parts, keyedPart{name: part.name, encoding: pe})
	}
	keys.out = make(map[ir.PropID]ir.PartEncoding, len(keys.parts))
	ts.Defer(keys)
	return keys.out, unmodeled, diags
}

// partKeys keys a form body's part encodings by the PropID of the property each
// names (partPropID), once the registry is whole (compile.Settlement): out is
// the map the body's Content holds, and Settle fills it with each part left
// carrying something (partFlags). Keys follow where the model behind body was
// interned (bodyModel), its document and pointer, so they align with that
// model's property IDs (invariant 2/3), else fallback in fallbackDoc, where the
// body's schema names (bodySchemaPointer). Asking the IR keeps this in step
// with schemaOf, which reads a $ref chain's end. c is the context the content
// lowered in.
type partKeys struct {
	c           lowering.Ctx
	out         map[ir.PropID]ir.PartEncoding
	body        ir.TypeID
	fallbackDoc ids.Document
	fallback    jsontext.Pointer
	parts       []keyedPart
}

// keyedPart is one part's name and the encoding it carries, its flags read off
// the schemas declaring it.
type keyedPart struct {
	name     string
	encoding ir.PartEncoding
}

// Settle keys each part's encoding, writing no node of reg. It reads the
// property each part names as the merge settled it, which Finish has done by
// then: a fold is a completion (compile.Types.Complete).
func (k partKeys) Settle(reg ir.TypeRegistry) []ir.Diagnostic {
	node := func(id ir.TypeID) (ir.TypeDef, bool) {
		td, ok := reg[id]
		return td, ok
	}
	doc, schemaPtr := k.fallbackDoc, k.fallback
	if model, ok := bodyModel(node, k.body); ok {
		doc, schemaPtr = lowering.IDsAt(k.c, model.Source), model.Pointer
	}
	for _, p := range k.parts {
		prop, _ := propByWire(node, k.body, p.name, 0)
		pe := partFlags(node, prop, p.encoding)
		if partEncodingEmpty(pe) {
			continue
		}
		k.out[partPropID(prop, doc, p.name, schemaPtr)] = pe
	}
	return nil
}

// bodyPart is one multipart part: the name keying its encoding entry, and every
// schema declaring it, which decide the structural flags when the body's type
// cannot.
type bodyPart struct {
	name    string
	schemas []*oas3.JSONSchema[oas3.Referenceable]
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
	// capped marks an inline branch nested past schema.MaxAllOfFlattenDepth
	// whose own allOf entries no path entered.
	capped bool
	// hopsSpent marks a $ref target past maxPartRefHops that no path entered.
	hopsSpent bool
}

// partVisits is what one body's part walk has met: the fewest hops each target
// was entered at, and the targets met past a bound.
type partVisits struct {
	// refs says where each $ref ends, as the lowering read it (schemaOf).
	refs    *typed.Bindings
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
		if cs := schemaOf(v.refs, c); cs != nil {
			v.capped[cs] = true
		}
	}
}

// walkBodyParts runs bodyParts over one body, whose $refs end where refs says,
// and reports a bound only for a target that no path entered.
func walkBodyParts(refs *typed.Bindings, js *oas3.JSONSchema[oas3.Referenceable]) partWalk {
	v := partVisits{
		refs:    refs,
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
// contributes. The first declaration of a name wins its encoding key; every one
// is kept for the structural flags (dedupeParts). The branches are the
// lowering's own (schema.FlattenedAllOf); a $ref branch is followed here, up to
// maxPartRefHops. v records the fewest hops each target was entered at, so one
// met again by no shorter path yields no parts, and the bounds that cut the
// walk. `allOf: [{$ref: Form}]` enumerates Form's parts (GitHub #140).
func bodyParts(js *oas3.JSONSchema[oas3.Referenceable], hops int, v partVisits) partWalk {
	s := schemaOf(v.refs, js)
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
		w.parts = append(w.parts, propertyParts(schemaOf(v.refs, br.Schema))...)
		if br.Capped {
			v.markCapped(schemaOf(v.refs, br.Schema))
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
		out = append(out, bodyPart{name: name, schemas: []*oas3.JSONSchema[oas3.Referenceable]{pjs}})
	}
	return out
}

// diagPartWalkCut reports the bounds that cut a body's part walk short, so an
// encoding entry for a part past one is not dropped without a word.
func diagPartWalkCut(c lowering.Ctx, w partWalk, at jsontext.Pointer) []ir.Diagnostic {
	var out []ir.Diagnostic
	if w.capped {
		out = append(out, c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, at,
			"form body composition is nested past the flattening depth of %d, so a part reached only below it has no encoding entry",
			schema.MaxAllOfFlattenDepth))
	}
	if w.hopsSpent {
		out = append(out, c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, at,
			"form body composition follows $refs past %d hops, so a part reached only beyond them has no encoding entry",
			maxPartRefHops))
	}
	return out
}

// dedupeParts keeps the first entry of each part name, in order, and collects
// the schemas of its later declarations onto it. A name redeclared across allOf
// branches is one part on the wire, so it must key one encoding entry and read
// its flags from every declaration rather than depend on which branch was
// walked last.
func dedupeParts(parts []bodyPart) []bodyPart {
	seen := make(map[string]int, len(parts))
	out := make([]bodyPart, 0, len(parts))
	for _, part := range parts {
		if i, ok := seen[part.name]; ok {
			out[i].schemas = append(out[i].schemas, part.schemas...)
			continue
		}
		seen[part.name] = len(out)
		out = append(out, part)
	}
	return out
}

// partPropID returns the ID of prop, the property carrying the given wire name
// on the model the body stands for, falling back to deriving one under
// schemaPtr in d when prop is nil.
//
// The IR is asked first because §4.3 stores only a model's own properties: a
// composed body holds its parts on the Base it inherits them from, so a key
// derived from the composed node's pointer would name a property that exists
// nowhere. Deriving is the answer for a body with no model in the IR, such as a
// schema declaring properties beside an enum or scalar type.
func partPropID(prop *ir.Property, d ids.Document, wire string, schemaPtr jsontext.Pointer) ir.PropID {
	if prop != nil {
		return prop.ID
	}
	return d.Prop(schemaPtr + ids.Ptr("properties", wire))
}

// propByWire searches the type body denotes for a property with the given wire
// name: what it declares itself, then what it inherits through Base and mixes in
// through Mixins, following the alias scalars a $ref-with-siblings position
// hoists on the way. depth bounds the walk against a chain no source can spell.
func propByWire(node func(ir.TypeID) (ir.TypeDef, bool), body ir.TypeID, wire string, depth int) (*ir.Property, bool) {
	if depth > maxBodyAliasHops {
		return nil, false
	}
	td, found := node(body)
	if !found {
		return nil, false
	}
	switch t := td.(type) {
	case *ir.Model:
		for i := range t.Properties {
			if t.Properties[i].WireName == wire {
				return &t.Properties[i], true
			}
		}
		return propInComposition(node, t, wire, depth)
	case *ir.Scalar:
		if t.Base == nil {
			return nil, false
		}
		return propByWire(node, t.Base.Target, wire, depth+1)
	default:
		return nil, false
	}
}

// propInComposition searches a model's Base and Mixins, in that order.
func propInComposition(node func(ir.TypeID) (ir.TypeDef, bool), m *ir.Model, wire string, depth int) (*ir.Property, bool) {
	if m.Base != nil {
		if p, ok := propByWire(node, m.Base.Target, wire, depth+1); ok {
			return p, true
		}
	}
	for _, mixin := range m.Mixins {
		if p, ok := propByWire(node, mixin.Target, wire, depth+1); ok {
			return p, true
		}
	}
	return nil, false
}

// partFlags returns pe with the structural flags of the part prop holds, read
// off the type the allOf merge settled on (partTypeFlags). Where the body model
// has no such property, or its type is a kind partTypeFlags cannot decide, pe
// keeps the flags read off every schema declaring the part.
func partFlags(node func(ir.TypeID) (ir.TypeDef, bool), prop *ir.Property, pe ir.PartEncoding) ir.PartEncoding {
	if prop == nil {
		return pe
	}
	if multi, file, decided := partTypeFlags(node, prop.Type); decided {
		pe.Multi, pe.Filename = multi, file
	}
	return pe
}

// partTypeFlags reads the structural flags of a part from its type: multi when
// it is a list or tuple, file when it is bytes, following the alias scalars a
// hoisted position or `format: byte` puts over the primitive. decided reports
// that the walk judged the type; false means the type is a kind it cannot
// answer for. The walk is bounded by maxBodyAliasHops.
func partTypeFlags(node func(ir.TypeID) (ir.TypeDef, bool), ref ir.TypeRef) (multi, file, decided bool) {
	id := ref.Target
	for range maxBodyAliasHops {
		td, found := node(id)
		if !found {
			return false, false, false
		}
		switch t := td.(type) {
		case *ir.List, *ir.Tuple:
			return true, false, true
		case *ir.Primitive:
			return false, t.Prim == ir.PrimBytes, true
		case *ir.Scalar:
			if t.Base == nil {
				return false, false, true
			}
			id = t.Base.Target
		default:
			return false, false, false
		}
	}
	return false, false, true
}

// buildPartEncoding assembles one part's PartEncoding: explicit encoding config
// (content types, headers, style, explode) merged with the structural flags Multi
// (array part) and Filename (binary/file part), read off every schema declaring
// the part. partKeys reads them off the part's merged type instead where it can
// (partFlags).
func buildPartEncoding(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, part bodyPart, encMap *sequencedmap.Map[string, *soa.Encoding], mediaPtr jsontext.Pointer) (ir.PartEncoding, ir.Unmodeled, []ir.Diagnostic) {
	pe := ir.PartEncoding{}
	var unmodeled ir.Unmodeled
	var diags []ir.Diagnostic
	if encMap != nil {
		if enc, ok := encMap.Get(part.name); ok {
			pe, unmodeled, diags = encodingConfig(c, ts, anchors, enc,
				mediaPtr+ids.Ptr("encoding", part.name), ids.Scope("encoding", part.name))
		}
	}
	for _, js := range part.schemas {
		if ps := schemaOf(c.Bindings(), js); ps != nil {
			pe.Multi = pe.Multi || schemaIsArray(ps)
			pe.Filename = pe.Filename || schemaIsFilePart(ps)
		}
	}
	return pe, unmodeled, diags
}

// encodingConfig lowers one Encoding object's declared wire config: content
// types, per-part headers, and form-style serialization. The structural flags
// (Multi, Filename) come from the part, not from here.
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
	kept, diags := annotation.PreserveNodeInto(&out, "openapi:"+scope+"/allowReserved",
		annotation.RawChildNode(enc.GetRootNode(), "allowReserved"), ir.ReasonNoIRHome, c.ProvenanceAt(at))
	if kept {
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
			"encoding allowReserved has no ir.PartEncoding home; kept verbatim under Unmodeled"))
	}
	diags = append(diags, nestedEncodings(c, &out, enc, encPtr, scope)...)
	ext, extDiags := annotation.ExtensionsUnder(enc.GetExtensions(), c.ProvenanceAt, encPtr, scope)
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
		kept, keptDiags := annotation.PreserveNodeInto(out, annotation.EntryKey(scope, keyword),
			annotation.RawChildNode(enc.GetRootNode(), keyword), ir.ReasonNoIRHome, c.ProvenanceAt(at))
		diags = append(diags, keptDiags...)
		if !kept {
			continue
		}
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
			"encoding %s has no ir.PartEncoding home; kept verbatim under Unmodeled", keyword))
	}
	return diags
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
func isBinaryBody(refs *typed.Bindings, mt string, js *oas3.JSONSchema[oas3.Referenceable]) bool {
	s := schemaOf(refs, js)
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
// following a $ref to the schema its chain ends on, as refs resolved it, so
// binary/file detection and part enumeration see the referent's type and
// properties rather than the bare ref (which carries none). It returns nil for
// a boolean schema or an absent one.
func schemaOf(refs *typed.Bindings, js *oas3.JSONSchema[oas3.Referenceable]) *oas3.Schema {
	if js == nil || !js.IsSchema() {
		return nil
	}
	s := js.GetSchema()
	if s != nil && s.Ref != nil {
		if end := refs.End(js); end != nil {
			return end.GetSchema()
		}
	}
	return s
}

// maxBodyAliasHops bounds every chain of Scalar bases this file's walks follow
// (styleguide bounded-everything rule). A loop of $refs lowers to no alias
// chain, so only an absurdly long one reaches it.
const maxBodyAliasHops = 64

// bodyModel returns where the model behind body was interned, its document
// and pointer, following the alias scalars a $ref-with-siblings position
// hoists to reach it. That pointer, in that document, is the one whose
// /properties/<name> children minted the model's PropIDs, so an encoding key
// derived from it addresses a property that exists.
//
// It reports ok=false for a body that stands for no model at all — a primitive,
// an enum, an opaque scalar — where no pointer would name a property either.
func bodyModel(node func(ir.TypeID) (ir.TypeDef, bool), body ir.TypeID) (ir.Provenance, bool) {
	id := body
	for range maxBodyAliasHops {
		td, found := node(id)
		if !found {
			return ir.Provenance{}, false
		}
		switch t := td.(type) {
		case *ir.Model:
			return t.Provenance, true
		case *ir.Scalar:
			if t.Base == nil {
				return ir.Provenance{}, false
			}
			id = t.Base.Target
		default:
			return ir.Provenance{}, false
		}
	}
	return ir.Provenance{}, false
}

// bodySchemaPointer returns the document and pointer partEncodings keys
// encoding entries by when the body has no model in the IR (bodyModel): where
// the media schema's $ref places what it names (resolve.Scope.Target), in
// whichever document, else localPtr in c's. Such a schema declares properties
// beside a contradictory `enum` or scalar `type`, so no property was interned
// under this pointer and the keys address none (see preserveUnhomedKeywords).
func bodySchemaPointer(c lowering.Ctx, js *oas3.JSONSchema[oas3.Referenceable], localPtr jsontext.Pointer,
) (ids.Document, jsontext.Pointer) {
	if js == nil || !resolve.IsRefSite(js, js.GetSchema()) {
		return c.IDs(), localPtr
	}
	at, ok := c.RefScope().Target(js)
	if !ok {
		return c.IDs(), localPtr
	}
	in, ok := lowering.InDocument(c, at.Doc)
	if !ok {
		return c.IDs(), localPtr
	}
	return in.IDs(), at.Pointer
}

// partEncodingEmpty reports whether a PartEncoding carries no information and can
// be omitted from the encoding map.
func partEncodingEmpty(pe ir.PartEncoding) bool {
	return len(pe.ContentTypes) == 0 && len(pe.Headers) == 0 &&
		!pe.Multi && !pe.Filename && pe.Style == "" && pe.Explode == nil
}
