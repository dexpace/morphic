package openapi

import (
	"slices"
	"strings"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/ir"
)

// lowerPayload lowers a request/response body's content map into a Payload with
// one Content per media type — all kept, in source order, with no primary-
// content selection (ir-design §7.2). The pointer is the payload owner (the
// response or requestBody); each Content's schema hoists under
// <pointer>/content/<mt>/schema.
func (l *lowerer) lowerPayload(content *sequencedmap.Map[string, *soa.MediaType], pointer, hint string) *ir.Payload {
	if content == nil || content.Len() == 0 {
		return nil
	}
	payload := &ir.Payload{}
	for mt, media := range content.All() {
		if media == nil {
			continue
		}
		payload.Contents = append(payload.Contents, l.lowerContent(mt, media, pointer, hint))
	}
	if len(payload.Contents) == 0 {
		return nil
	}
	return payload
}

// lowerContent lowers one media-type view: its type graph, examples, binary/
// form specialization, sequential-media shape, and extensions.
func (l *lowerer) lowerContent(mt string, media *soa.MediaType, pointer, hint string) ir.Content {
	mediaPtr := pointer + ptr("content", mt)
	c := ir.Content{
		MediaType: mt,
		Type:      l.schemaRef(media.GetSchema(), mediaPtr+ptr("schema"), hint),
	}
	if ex := l.mediaExamples(media, mediaPtr); len(ex) > 0 {
		c.Examples = ex
	}
	switch {
	case isBinaryBody(mt, media.GetSchema()):
		c.File = &ir.FileInfo{IsText: false, ContentTypes: []string{mt}}
		c.Type = l.primRef(ir.PrimBytes)
	case isFormContent(mt):
		if enc := l.partEncodings(media, mediaPtr, c.Type.Target); len(enc) > 0 {
			c.Encoding = enc
		}
	}
	l.fillSequential(&c, media, mediaPtr, hint)
	if ext := l.extensions(media.GetExtensions(), mediaPtr); len(ext) > 0 {
		c.Unmodeled = mergeUnmodeled(c.Unmodeled, ext)
	}
	return c
}

// fillSequential lowers 3.2 sequential-media fields: itemSchema becomes the
// element type, and itemEncoding becomes Content.ItemEncoding, with Multi set
// because the construct describes a repeated tail by definition.
//
// That lowering only holds when no prefixEncoding accompanies it: prefixes make
// itemEncoding govern the items *after* them rather than every item, which a
// single every-item encoding would misstate. Those documents take
// positionalEncoding instead.
func (l *lowerer) fillSequential(c *ir.Content, media *soa.MediaType, mediaPtr, hint string) {
	if item := media.GetItemSchema(); item != nil {
		ref := l.schemaRef(item, mediaPtr+ptr("itemSchema"), hint+"_item")
		c.Item = &ref
	}
	if len(media.GetPrefixEncoding()) > 0 {
		l.positionalEncoding(c, media, mediaPtr)
		return
	}
	enc := media.GetItemEncoding()
	if enc == nil {
		return
	}
	pe := l.encodingConfig(enc, mediaPtr+ptr("itemEncoding"))
	pe.Multi = true
	c.ItemEncoding = &pe
}

// positionalEncoding keeps 3.2 positional prefixEncoding — and the itemEncoding
// that governs the tail after it — verbatim, with one info diagnostic.
// Content.ItemEncoding states one encoding for every item, so it cannot say
// "these two in order, then the rest alike": lowering only the tail into it
// would drop the prefixes and assert their encoding governs every item. The
// ordinals a positional form needs are a gap the IR can close later, so the
// entries carry ReasonNoIRHome rather than a degraded lowering.
func (l *lowerer) positionalEncoding(c *ir.Content, media *soa.MediaType, mediaPtr string) {
	root := media.GetRootNode()
	// The announcement follows prefixEncoding, the construct that brought this
	// lowering here: an itemEncoding beside it is optional, so its absence must not
	// suppress the message, and its own conversion failure reports separately.
	at := mediaPtr + ptr("prefixEncoding")
	kept := l.preserveNode(&c.Unmodeled, "openapi:prefixEncoding", rawChildNode(root, "prefixEncoding"),
		ir.ReasonNoIRHome, at)
	l.preserveNode(&c.Unmodeled, "openapi:itemEncoding", rawChildNode(root, "itemEncoding"),
		ir.ReasonNoIRHome, mediaPtr+ptr("itemEncoding"))
	if !kept {
		// Reaching here means prefixEncoding is declared — that is the only reason
		// this lowering runs — yet nothing of it was written. Unlike every other
		// preservation site, an empty payload here cannot mean "there was no
		// construct", so it is reported rather than passed over (GitHub #144).
		l.diag(ir.SeverityError, codeUnpreservableConstruct, at,
			"prefixEncoding is declared but its source node could not be read; it is "+
				"represented in the IR in no form at all")
		return
	}
	l.diag(ir.SeverityInfo, codeDegradedConstruct, mediaPtr,
		"prefixEncoding is positional and has no per-item IR home; it and any itemEncoding are kept under Unmodeled")
}

// partEncodings builds the multipart/form per-part wire config, keyed by each
// body-model property's PropID. A part is included when it carries an explicit
// encoding entry or is itself a repeated (array) or file (binary) part. body is
// the TypeID the content's own schema position lowered to.
func (l *lowerer) partEncodings(media *soa.MediaType, mediaPtr string, body ir.TypeID) map[ir.PropID]ir.PartEncoding {
	model := schemaOf(media.GetSchema())
	if model == nil {
		return nil
	}
	props := model.GetProperties()
	if props == nil || props.Len() == 0 {
		return nil
	}
	// Key by the pointer the body model was interned at, not by mediaPtr, so the
	// keys align with that model's property IDs (invariant 2/3). Asking the IR is
	// what keeps this in step with schemaOf, which reads the end of a $ref chain
	// while a pointer cut from the ref string names only its first hop.
	schemaPtr, ok := l.bodyModelPointer(body)
	if !ok {
		schemaPtr = l.bodySchemaPointer(media.GetSchema(), mediaPtr+ptr("schema"))
	}
	encMap := media.GetEncoding()
	out := map[ir.PropID]ir.PartEncoding{}
	for name, pjs := range props.All() {
		pe := l.buildPartEncoding(name, pjs, encMap, mediaPtr)
		if partEncodingEmpty(pe) {
			continue
		}
		out[propID(schemaPtr+ptr("properties", name))] = pe
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// buildPartEncoding assembles one part's PartEncoding: explicit encoding config
// (content types, headers, style, explode) merged with the structural flags Multi
// (array part) and Filename (binary/file part).
func (l *lowerer) buildPartEncoding(name string, pjs *oas3.JSONSchema[oas3.Referenceable], encMap *sequencedmap.Map[string, *soa.Encoding], mediaPtr string) ir.PartEncoding {
	pe := ir.PartEncoding{}
	if encMap != nil {
		if enc, ok := encMap.Get(name); ok {
			pe = l.encodingConfig(enc, mediaPtr+ptr("encoding", name))
		}
	}
	if part := schemaOf(pjs); part != nil {
		pe.Multi = schemaIsArray(part)
		pe.Filename = schemaIsFilePart(part)
	}
	return pe
}

// encodingConfig lowers one Encoding object's declared wire config: content
// types, per-part headers, and form-style serialization. The structural flags
// (Multi, Filename) come from the part's own schema, not from here.
func (l *lowerer) encodingConfig(enc *soa.Encoding, encPtr string) ir.PartEncoding {
	pe := ir.PartEncoding{}
	if enc == nil {
		return pe
	}
	pe.ContentTypes = splitContentTypes(enc.GetContentTypeValue())
	pe.Headers = l.lowerHeaders(enc.GetHeaders(), encPtr)
	if enc.Style != nil {
		pe.Style = string(*enc.Style)
	}
	pe.Explode = enc.Explode
	return pe
}

// lowerHeaders lowers a header map into Properties in source order. Each
// entry's own pointer stays its ID and Provenance (two keys $ref'ing the same
// header must not collide), but its schema — and the name hint that schema is
// hoisted under — follow the ref target's declaration instead (issue #107).
func (l *lowerer) lowerHeaders(headers *sequencedmap.Map[string, *soa.ReferencedHeader], basePtr string) []ir.Property {
	if headers == nil || headers.Len() == 0 {
		return nil
	}
	out := make([]ir.Property, 0, headers.Len())
	for name, rh := range headers.All() {
		hptr := basePtr + ptr("headers", name)
		h, hdecl := resolveRefAt[soa.Header](l, rh, hptr)
		if h == nil {
			continue
		}
		out = append(out, l.lowerHeader(h, name, hptr, hdecl))
	}
	return out
}

// lowerHeader lowers one header entry into a Property. Its schema goes through
// fillPropertyDetail like a model property's: a header schema declares docs,
// constraints, xml, examples and validation-only keywords the same way, and
// ir.Property has a field for each, so the header path had no reason to drop
// them (GitHub #116).
func (l *lowerer) lowerHeader(h *soa.Header, name, hptr, hdecl string) ir.Property {
	js, schemaPtr, mediaType := l.headerSchema(h, hdecl)
	p := ir.Property{
		ID:         propID(hptr),
		Name:       compile.NamingFor(name),
		WireName:   name,
		Type:       l.carriedSchemaRef(js, schemaPtr, declarationHint(hdecl, name)),
		Required:   h.GetRequired(),
		Provenance: ir.Provenance{Source: l.srcIndex, Pointer: hptr},
	}
	if mediaType != "" {
		// The media type a content-style header serializes its value in, which is
		// what ir.Encoding.MediaType holds. Nothing else on this path writes
		// Property.Encoding, so the content spelling loses nothing the schema
		// spelling keeps.
		p.Encoding = &ir.Encoding{MediaType: mediaType}
	}
	l.fillPropertyDetail(&p, js, schemaPtr)
	l.applyHeaderAnnotations(&p, h, hdecl)
	return p
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
func (l *lowerer) headerSchema(h *soa.Header, hdecl string) (*oas3.JSONSchema[oas3.Referenceable], string, string) {
	if js := h.GetSchema(); js != nil {
		return js, hdecl + ptr("schema"), ""
	}
	mt, media, ok := l.singleContentEntry(h.GetContent(), hdecl)
	if !ok {
		return nil, hdecl + ptr("schema"), ""
	}
	return media.GetSchema(), hdecl + ptr("content", mt, "schema"), mt
}

// singleContentEntry returns the one entry a content-style header or parameter
// declares, and reports a document declaring more than one.
//
// OpenAPI requires exactly one entry at both positions, so only the first can
// lower — ir.Property and ir.Parameter each hold a single type. Taking it in
// silence dropped a declared schema without a word, which is the loss GitHub #139
// fixed at this position in its other spelling; the extras are named instead so
// the document's own error is visible rather than absorbed.
func (l *lowerer) singleContentEntry(content *sequencedmap.Map[string, *soa.MediaType], at string) (string, *soa.MediaType, bool) {
	if content == nil || content.Len() == 0 {
		return "", nil, false
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
	if len(ignored) > 0 {
		l.diag(ir.SeverityWarning, codeDegradedConstruct, at+ptr("content"),
			"a content-style header or parameter must declare exactly one media type; "+
				"%q is lowered and %s ignored", first, strings.Join(ignored, ", "))
	}
	return first, chosen, chosen != nil
}

// applyHeaderAnnotations overlays the annotations the header object writes on
// itself onto p, after its schema's. A header carries both, and the header's
// own are the more specific of the two — they describe this header rather than
// the type it happens to be.
func (l *lowerer) applyHeaderAnnotations(p *ir.Property, h *soa.Header, hdecl string) {
	if d := h.GetDescription(); d != "" {
		p.Docs.Description = d
	}
	if h.GetDeprecated() {
		p.Deprecation = &ir.Deprecation{}
	}
	if ex := l.exampleList(h.GetExample(), h.GetExamples(), hdecl); len(ex) > 0 {
		p.Examples = ex
	}
	p.Unmodeled = mergeUnmodeled(p.Unmodeled, l.extensions(h.GetExtensions(), hdecl))
}

// mediaExamples lowers a media type's single and plural example values.
func (l *lowerer) mediaExamples(media *soa.MediaType, pointer string) []ir.Example {
	return l.exampleList(media.GetExample(), media.GetExamples(), pointer)
}

// exampleList lowers a single example node and a plural example map into value
// examples, in source order. An unconvertible node is skipped with a warning
// diagnostic rather than silently. The singular `example` keyword is a bare
// value with nowhere to hang a name or summary, so it lowers to a value alone.
func (l *lowerer) exampleList(single *yaml.Node, plural *sequencedmap.Map[string, *soa.ReferencedExample], pointer string) []ir.Example {
	var out []ir.Example
	if single != nil {
		out = l.appendExample(out, ir.Example{}, single, pointer, "example")
	}
	if plural == nil {
		return out
	}
	for name, re := range plural.All() {
		out = l.appendPluralExample(out, re, pointer, name)
	}
	return out
}

// appendPluralExample lowers one named entry of a plural `examples` map with the
// annotations that surround its value: the map key names the example, and its
// summary and description travel with it. An entry written as a $ref holds no
// value of its own — the value lives in the referenced component — so its
// diagnostic is stamped at the reference site rather than at a `value` node this
// entry never had; an inline entry is stamped at its own `value`. Only this hop
// is de-referenced: an enclosing $ref'd response or parameter is already
// flattened into pointer.
func (l *lowerer) appendPluralExample(out []ir.Example, re *soa.ReferencedExample, pointer, name string) []ir.Example {
	ex := resolveRef[soa.Example](re)
	if ex == nil {
		return out
	}
	proto := ir.Example{
		Name:        name,
		Summary:     ex.GetSummary(),
		Description: ex.GetDescription(),
		ExternalURL: ex.GetExternalValue(),
	}
	node := ex.GetValue()
	if node == nil {
		return l.appendValuelessExample(out, proto, pointer, name)
	}
	if re.IsReference() {
		return l.appendExample(out, proto, node, pointer, "examples", name)
	}
	return l.appendExample(out, proto, node, pointer, "examples", name, "value")
}

// appendValuelessExample records an entry that declares no inline `value`. The
// spec-legal externalValue form is one of these, and ir.Example.ExternalURL is
// its home, so it is kept whole. Any other value-less entry carries no example
// at all — a 3.2 dataValue/serializedValue, or an empty stub — and is dropped
// with a warning rather than in silence.
func (l *lowerer) appendValuelessExample(out []ir.Example, proto ir.Example, pointer, name string) []ir.Example {
	if proto.ExternalURL == "" {
		l.diag(ir.SeverityWarning, codeDegradedConstruct, pointer+ptr("examples", name),
			"example declares neither value nor externalValue")
		return out
	}
	return append(out, proto)
}

// lowerRequestBody lowers an operation's request body onto op.Request and the
// binding's RequestContentTypes. The IR expresses body optionality via presence,
// so a non-required body stays present with its optionality preserved under
// Unmodeled plus one info diagnostic (ir-design §7.2 clarification). opDeclPtr
// is the operation's own declaration pointer, so a $ref'd body interns its
// content once at its component pointer rather than once per mount site
// (issue #107) — and under the component's name, since the operationId hint
// would otherwise name the shared node after one arbitrary referencing site.
func (l *lowerer) lowerRequestBody(op *ir.Operation, hb *ir.HTTPBinding, src *soa.Operation, opDeclPtr string) {
	rb, bodyPtr := resolveRefAt[soa.RequestBody](l, src.GetRequestBody(), opDeclPtr+ptr("requestBody"))
	if rb == nil {
		return
	}
	payload := l.lowerPayload(rb.GetContent(), bodyPtr, declarationHint(bodyPtr, requestBodyHint(src)))
	if payload == nil {
		return
	}
	if !rb.GetRequired() {
		l.preserve(&payload.Unmodeled, "openapi:required", ir.RawValue("false"),
			ir.ReasonNoIRHome, bodyPtr+ptr("required"))
		l.diag(ir.SeverityInfo, codeDegradedConstruct, bodyPtr,
			"request body is not required; optionality kept under Unmodeled")
	}
	op.Request = payload
	hb.RequestContentTypes = contentTypeKeys(rb.GetContent())
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
func (l *lowerer) bodyModelPointer(body ir.TypeID) (string, bool) {
	id := body
	for range maxBodyAliasHops {
		td, found := l.types.Node(id)
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
// The document half of the $ref decides, via internalPointer, rather than being
// cut off and discarded. A fragment lifted from a ref into another document
// would otherwise become an identity in *this* one, naming whichever local
// schema happened to share the path — a property of a different document
// addressed as if it were ours. localPtr is the honest fallback there: it is
// the position the reference itself occupies here.
func (l *lowerer) bodySchemaPointer(js *oas3.JSONSchema[oas3.Referenceable], localPtr string) string {
	if js == nil || !isRefSite(js, js.GetSchema()) {
		return localPtr
	}
	if pointer, ok := l.internalPointer(js.GetRef().String()); ok {
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
