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
func lowerPayload(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, content *sequencedmap.Map[string, *soa.MediaType], pointer, hint string) (*ir.Payload, []ir.Diagnostic) {
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

// lowerContent lowers one media-type view: its type graph, examples, binary/
// form specialization, sequential-media shape, and extensions.
func lowerContent(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, mt string, media *soa.MediaType, pointer, hint string) (ir.Content, []ir.Diagnostic) {
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
		enc, encDiags := partEncodings(c, ts, anchors, media, mediaPtr, content.Type.Target)
		diags = append(diags, encDiags...)
		if len(enc) > 0 {
			content.Encoding = enc
		}
	}
	diags = append(diags, fillSequential(c, ts, anchors, &content, media, mediaPtr, hint)...)
	ext, extDiags := schema.ExtensionsOf(c, media.GetExtensions(), mediaPtr)
	diags = append(diags, extDiags...)
	if len(ext) > 0 {
		content.Unmodeled = annotation.MergeUnmodeled(content.Unmodeled, ext)
	}
	return content, diags
}

// fillSequential lowers 3.2 sequential-media fields: itemSchema becomes the
// element type, and itemEncoding becomes Content.ItemEncoding, with Multi set
// because the construct describes a repeated tail by definition.
//
// That lowering only holds when no prefixEncoding accompanies it: prefixes make
// itemEncoding govern the items *after* them rather than every item, which a
// single every-item encoding would misstate. Those documents take
// positionalEncoding instead.
func fillSequential(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, content *ir.Content, media *soa.MediaType, mediaPtr, hint string) []ir.Diagnostic {
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
	pe, encDiags := encodingConfig(c, ts, anchors, enc, mediaPtr+ids.Ptr("itemEncoding"))
	pe.Multi = true
	content.ItemEncoding = &pe
	return append(diags, encDiags...)
}

// positionalEncoding keeps 3.2 positional prefixEncoding — and the itemEncoding
// that governs the tail after it — verbatim, with one info diagnostic.
// Content.ItemEncoding states one encoding for every item, so it cannot say
// "these two in order, then the rest alike": lowering only the tail into it
// would drop the prefixes and assert their encoding governs every item. The
// ordinals a positional form needs are a gap the IR can close later, so the
// entries carry ReasonNoIRHome rather than a degraded lowering.
func positionalEncoding(c lowering.Ctx, content *ir.Content, media *soa.MediaType, mediaPtr string) []ir.Diagnostic {
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
func partEncodings(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, media *soa.MediaType, mediaPtr string, body ir.TypeID) (map[ir.PropID]ir.PartEncoding, []ir.Diagnostic) {
	parts := bodyParts(media.GetSchema(), 0)
	if len(parts) == 0 {
		return nil, nil
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
	encMap := media.GetEncoding()
	out := map[ir.PropID]ir.PartEncoding{}
	for _, part := range parts {
		pe, partDiags := buildPartEncoding(c, ts, anchors, part.name, part.schema, encMap, mediaPtr)
		diags = append(diags, partDiags...)
		if partEncodingEmpty(pe) {
			continue
		}
		out[partPropID(ts, body, part.name, schemaPtr)] = pe
	}
	if len(out) == 0 {
		return nil, diags
	}
	return out, diags
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
func partPropID(ts *compile.Types, body ir.TypeID, wire, schemaPtr string) ir.PropID {
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
func buildPartEncoding(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, name string, pjs *oas3.JSONSchema[oas3.Referenceable], encMap *sequencedmap.Map[string, *soa.Encoding], mediaPtr string) (ir.PartEncoding, []ir.Diagnostic) {
	pe := ir.PartEncoding{}
	var diags []ir.Diagnostic
	if encMap != nil {
		if enc, ok := encMap.Get(name); ok {
			pe, diags = encodingConfig(c, ts, anchors, enc, mediaPtr+ids.Ptr("encoding", name))
		}
	}
	if part := schemaOf(pjs); part != nil {
		pe.Multi = schemaIsArray(part)
		pe.Filename = schemaIsFilePart(part)
	}
	return pe, diags
}

// encodingConfig lowers one Encoding object's declared wire config: content
// types, per-part headers, and form-style serialization. The structural flags
// (Multi, Filename) come from the part's own schema, not from here.
func encodingConfig(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, enc *soa.Encoding, encPtr string) (ir.PartEncoding, []ir.Diagnostic) {
	pe := ir.PartEncoding{}
	if enc == nil {
		return pe, nil
	}
	pe.ContentTypes = splitContentTypes(enc.GetContentTypeValue())
	headers, diags := lowerHeaders(c, ts, anchors, enc.GetHeaders(), encPtr)
	pe.Headers = headers
	if enc.Style != nil {
		pe.Style = string(*enc.Style)
	}
	pe.Explode = enc.Explode
	return pe, diags
}

// lowerHeaders lowers a header map into Properties in source order. Each
// entry's own pointer stays its ID and Provenance (two keys $ref'ing the same
// header must not collide), but its schema — and the name hint that schema is
// hoisted under — follow the ref target's declaration instead (issue #107).
func lowerHeaders(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, headers *sequencedmap.Map[string, *soa.ReferencedHeader], basePtr string) ([]ir.Property, []ir.Diagnostic) {
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
		out = append(out, p)
	}
	return out, diags
}

// lowerHeader lowers one header entry into a Property. Its schema goes through
// schema.FillPropertyDetail like a model property's: a header schema declares
// docs, constraints, xml, examples and validation-only keywords the same way,
// and ir.Property has a field for each, so the header path had no reason to drop
// them (GitHub #116).
func lowerHeader(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, h *soa.Header, name, hptr, hdecl string) (ir.Property, []ir.Diagnostic) {
	js, schemaPtr, mediaType, diags := headerSchema(c, h, hdecl)
	headerType, headerDiags := schema.CarriedRef(c, ts, anchors, schema.TopLevelDepth, js, schemaPtr, ids.DeclarationHint(hdecl, name))
	diags = append(diags, headerDiags...)
	p := ir.Property{
		ID:         ids.Prop(hptr),
		Name:       compile.NamingFor(name),
		WireName:   name,
		Type:       headerType,
		Required:   h.GetRequired(),
		Provenance: c.ProvenanceAt(hptr),
	}
	if mediaType != "" {
		// The media type a content-style header serializes its value in, which is
		// what ir.Encoding.MediaType holds. Nothing else on this path writes
		// Property.Encoding, so the content spelling loses nothing the schema
		// spelling keeps.
		p.Encoding = &ir.Encoding{MediaType: mediaType}
	}
	diags = append(diags, schema.FillPropertyDetail(c, ts, anchors, &p, js, schemaPtr)...)
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
func preserveHeaderSerialization(c lowering.Ctx, p *ir.Property, h *soa.Header, hdecl string) []ir.Diagnostic {
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

// headerSchema returns the schema a header declares, the pointer that schema sits
// at, and the media type serializing it — empty for the schema spelling.
//
// OpenAPI lets a header state its type as either `schema` or a `content` map
// holding exactly one entry, and only the first spelling was read: a
// content-style header lowered as if it had no schema at all, discarding its
// type, its constraints and its xml hints together and without a diagnostic
// (GitHub #139). The parameter path already read both (fillParamType), which is
// why request headers never showed the defect.
func headerSchema(c lowering.Ctx, h *soa.Header, hdecl string) (*oas3.JSONSchema[oas3.Referenceable], string, string, []ir.Diagnostic) {
	if js := h.GetSchema(); js != nil {
		return js, hdecl + ids.Ptr("schema"), "", nil
	}
	mt, media, ok, diags := singleContentEntry(c, h.GetContent(), hdecl)
	if !ok {
		return nil, hdecl + ids.Ptr("schema"), "", diags
	}
	return media.GetSchema(), hdecl + ids.Ptr("content", mt, "schema"), mt, diags
}

// singleContentEntry returns the one entry a content-style header or parameter
// declares, and reports a document declaring more than one.
//
// OpenAPI requires exactly one entry at both positions, so only the first can
// lower — ir.Property and ir.Parameter each hold a single type. Taking it in
// silence dropped a declared schema without a word, which is the loss GitHub #139
// fixed at this position in its other spelling; the extras are named instead so
// the document's own error is visible rather than absorbed.
func singleContentEntry(c lowering.Ctx, content *sequencedmap.Map[string, *soa.MediaType], at string) (string, *soa.MediaType, bool, []ir.Diagnostic) {
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
func applyHeaderAnnotations(c lowering.Ctx, p *ir.Property, h *soa.Header, hdecl string) []ir.Diagnostic {
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
	return diags
}

// exampleList lowers a single example node and a plural example map into value
// examples, in source order. An unconvertible node is skipped with a warning
// diagnostic rather than silently. The singular `example` keyword is a bare
// value with nowhere to hang a name or summary, so it lowers to a value alone.
func exampleList(c lowering.Ctx, single *yaml.Node, plural *sequencedmap.Map[string, *soa.ReferencedExample], pointer string) ([]ir.Example, []ir.Diagnostic) {
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
func appendPluralExample(c lowering.Ctx, out []ir.Example, re *soa.ReferencedExample, pointer, name string) ([]ir.Example, []ir.Diagnostic) {
	ex := resolve.Object[soa.Example](re)
	if ex == nil {
		return out, nil
	}
	proto := ir.Example{
		Name:        name,
		Summary:     ex.GetSummary(),
		Description: ex.GetDescription(),
		ExternalURL: ex.GetExternalValue(),
	}
	node := ex.GetValue()
	if node == nil {
		return appendValuelessExample(c, out, proto, pointer, name)
	}
	if re.IsReference() {
		return schema.AppendExample(c, out, proto, node, pointer, "examples", name)
	}
	return schema.AppendExample(c, out, proto, node, pointer, "examples", name, "value")
}

// appendValuelessExample records an entry that declares no inline `value`. The
// spec-legal externalValue form is one of these, and ir.Example.ExternalURL is
// its home, so it is kept whole. Any other value-less entry carries no example
// at all — a 3.2 dataValue/serializedValue, or an empty stub — and is dropped
// with a warning rather than in silence.
func appendValuelessExample(c lowering.Ctx, out []ir.Example, proto ir.Example, pointer, name string) ([]ir.Example, []ir.Diagnostic) {
	if proto.ExternalURL == "" {
		return out, []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct,
			pointer+ids.Ptr("examples", name), "example declares neither value nor externalValue")}
	}
	return append(out, proto), nil
}

// lowerRequestBody lowers an operation's request body onto op.Request and the
// binding's RequestContentTypes. The IR expresses body optionality via presence,
// so a non-required body stays present with its optionality preserved under
// Unmodeled plus one info diagnostic (ir-design §7.2 clarification). opDeclPtr
// is the operation's own declaration pointer, so a $ref'd body interns its
// content once at its component pointer rather than once per mount site
// (issue #107) — and under the component's name, since the operationId hint
// would otherwise name the shared node after one arbitrary referencing site.
func lowerRequestBody(c lowering.Ctx, ts *compile.Types, anchors *schema.AnchorIndex, op *ir.Operation, hb *ir.HTTPBinding, src *soa.Operation, opDeclPtr string) []ir.Diagnostic {
	rb, bodyPtr := resolve.ObjectAt[soa.RequestBody](c.RefScope(), src.GetRequestBody(), opDeclPtr+ids.Ptr("requestBody"))
	if rb == nil {
		return nil
	}
	payload, diags := lowerPayload(c, ts, anchors, rb.GetContent(), bodyPtr, ids.DeclarationHint(bodyPtr, requestBodyHint(src)))
	if payload == nil {
		return diags
	}
	if !rb.GetRequired() {
		schema.Preserve(c, &payload.Unmodeled, "openapi:required", ir.RawValue("false"),
			ir.ReasonNoIRHome, bodyPtr+ids.Ptr("required"))

		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, bodyPtr,
			"request body is not required; optionality kept under Unmodeled"))
	}
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
func bodyModelPointer(ts *compile.Types, body ir.TypeID) (string, bool) {
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
// properties were interned: the ref target's pointer when the media schema is a
// same-document $ref, else localPtr.
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
// the position the reference itself occupies here.
func bodySchemaPointer(c lowering.Ctx, js *oas3.JSONSchema[oas3.Referenceable], localPtr string) string {
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
