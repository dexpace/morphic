package openapi

import (
	"strings"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"
	yaml "gopkg.in/yaml.v3"

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
	if ex := l.mediaExamples(media); len(ex) > 0 {
		c.Examples = ex
	}
	switch {
	case isBinaryBody(mt, media.GetSchema()):
		c.File = &ir.FileInfo{IsText: false, ContentTypes: []string{mt}}
		c.Type = l.primRef(ir.PrimBytes)
	case isFormContent(mt):
		if enc := l.partEncodings(media, mediaPtr); len(enc) > 0 {
			c.Encoding = enc
		}
	}
	l.fillSequential(&c, media, mediaPtr, hint)
	ext, diags := extensionsFrom(media.GetExtensions())
	l.diags = append(l.diags, diags...)
	if len(ext) > 0 {
		c.Extensions = mergeExtensions(c.Extensions, ext)
	}
	return c
}

// fillSequential lowers 3.2 sequential-media fields: itemSchema becomes the
// element type; itemEncoding has no per-property structural home, so it is
// preserved verbatim under Extensions with one info diagnostic.
func (l *lowerer) fillSequential(c *ir.Content, media *soa.MediaType, mediaPtr, hint string) {
	if item := media.GetItemSchema(); item != nil {
		ref := l.schemaRef(item, mediaPtr+ptr("itemSchema"), hint+"_item")
		c.Item = &ref
	}
	if media.GetItemEncoding() == nil {
		return
	}
	raw := nodeToRaw(rawChildNode(media.GetRootNode(), "itemEncoding"))
	if raw == nil {
		return
	}
	if c.Extensions == nil {
		c.Extensions = ir.Extensions{}
	}
	c.Extensions["openapi:itemEncoding"] = raw
	l.diags = append(l.diags, diagf(ir.SeverityInfo, codeDegradedConstruct,
		ir.Provenance{Source: l.srcIndex, Pointer: mediaPtr},
		"3.2 itemEncoding preserved under extensions; per-item multipart encoding is out of model"))
}

// partEncodings builds the multipart/form per-part wire config, keyed by each
// body-model property's PropID. A part is included when it carries an explicit
// encoding entry or is itself a repeated (array) or file (binary) part.
func (l *lowerer) partEncodings(media *soa.MediaType, mediaPtr string) map[string]ir.PartEncoding {
	model := schemaOf(media.GetSchema())
	if model == nil {
		return nil
	}
	props := model.GetProperties()
	if props == nil || props.Len() == 0 {
		return nil
	}
	schemaPtr := mediaPtr + ptr("schema")
	encMap := media.GetEncoding()
	out := map[string]ir.PartEncoding{}
	for name, pjs := range props.All() {
		pe := l.buildPartEncoding(name, pjs, encMap, mediaPtr)
		if partEncodingEmpty(pe) {
			continue
		}
		out[string(propID(schemaPtr+ptr("properties", name)))] = pe
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
		if enc, ok := encMap.Get(name); ok && enc != nil {
			pe.ContentTypes = splitContentTypes(enc.GetContentTypeValue())
			pe.Headers = l.lowerHeaders(enc.GetHeaders(), mediaPtr+ptr("encoding", name))
			if enc.Style != nil {
				pe.Style = string(*enc.Style)
			}
			pe.Explode = enc.Explode
		}
	}
	if part := schemaOf(pjs); part != nil {
		pe.Multi = schemaIsArray(part)
		pe.Filename = schemaIsFilePart(part)
	}
	return pe
}

// lowerHeaders lowers a header map (response headers or per-part encoding
// headers) into Properties in source order, each identified by its pointer.
func (l *lowerer) lowerHeaders(headers *sequencedmap.Map[string, *soa.ReferencedHeader], basePtr string) []ir.Property {
	if headers == nil || headers.Len() == 0 {
		return nil
	}
	out := make([]ir.Property, 0, headers.Len())
	for name, rh := range headers.All() {
		h := resolveHeader(rh)
		if h == nil {
			continue
		}
		hptr := basePtr + ptr("headers", name)
		out = append(out, ir.Property{
			ID:         propID(hptr),
			Name:       ir.Naming{Source: name, Canonical: canonicalWords(name)},
			WireName:   name,
			Type:       l.schemaRef(h.GetSchema(), hptr+ptr("schema"), name),
			Required:   h.GetRequired(),
			Provenance: ir.Provenance{Source: l.srcIndex, Pointer: hptr},
		})
	}
	return out
}

// mediaExamples lowers a media type's single and plural example values.
func (l *lowerer) mediaExamples(media *soa.MediaType) []ir.Example {
	return l.exampleList(media.GetExample(), media.GetExamples())
}

// exampleList lowers a single example node and a plural example map into value
// examples, in source order; unconvertible nodes are skipped.
func (l *lowerer) exampleList(single *yaml.Node, plural *sequencedmap.Map[string, *soa.ReferencedExample]) []ir.Example {
	var out []ir.Example
	if single != nil {
		if v, err := valueFromNode(single); err == nil {
			out = append(out, ir.Example{Value: &v})
		}
	}
	if plural == nil {
		return out
	}
	for _, re := range plural.All() {
		ex := resolveExample(re)
		if ex == nil {
			continue
		}
		node := ex.GetValue()
		if node == nil {
			continue
		}
		if v, err := valueFromNode(node); err == nil {
			out = append(out, ir.Example{Value: &v})
		}
	}
	return out
}

// lowerRequestBody lowers an operation's request body onto op.Request and the
// binding's RequestContentTypes. The IR expresses body optionality via presence,
// so a non-required body stays present with its optionality preserved under
// Extensions plus one info diagnostic (ir-design §7.2 clarification).
func (l *lowerer) lowerRequestBody(op *ir.Operation, hb *ir.HTTPBinding, src *soa.Operation, opPointer string) {
	rb := resolveRequestBody(src.GetRequestBody())
	if rb == nil {
		return
	}
	bodyPtr := opPointer + ptr("requestBody")
	payload := l.lowerPayload(rb.GetContent(), bodyPtr, requestBodyHint(src))
	if payload == nil {
		return
	}
	if !rb.GetRequired() {
		if payload.Extensions == nil {
			payload.Extensions = ir.Extensions{}
		}
		payload.Extensions["openapi:required"] = ir.RawValue("false")
		l.diags = append(l.diags, diagf(ir.SeverityInfo, codeDegradedConstruct,
			ir.Provenance{Source: l.srcIndex, Pointer: bodyPtr},
			"request body is not required; optionality preserved under extensions"))
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
	for _, part := range strings.Split(raw, ",") {
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
	for _, t := range s.GetType() {
		if t == st {
			return true
		}
	}
	return false
}

// schemaOf returns the concrete Schema of a schema-or-ref-or-bool position, or
// nil for a boolean schema or an absent one.
func schemaOf(js *oas3.JSONSchema[oas3.Referenceable]) *oas3.Schema {
	if js == nil || !js.IsSchema() {
		return nil
	}
	return js.GetSchema()
}

// partEncodingEmpty reports whether a PartEncoding carries no information and can
// be omitted from the encoding map.
func partEncodingEmpty(pe ir.PartEncoding) bool {
	return len(pe.ContentTypes) == 0 && len(pe.Headers) == 0 &&
		!pe.Multi && !pe.Filename && pe.Style == "" && pe.Explode == nil
}

// resolveRequestBody returns the concrete RequestBody of a reference-or-inline
// entry.
func resolveRequestBody(rrb *soa.ReferencedRequestBody) *soa.RequestBody {
	if rrb == nil {
		return nil
	}
	if obj := rrb.GetObject(); obj != nil {
		return obj
	}
	return rrb.GetResolvedObject()
}

// resolveExample returns the concrete Example of a reference-or-inline entry.
func resolveExample(re *soa.ReferencedExample) *soa.Example {
	if re == nil {
		return nil
	}
	if obj := re.GetObject(); obj != nil {
		return obj
	}
	return re.GetResolvedObject()
}
