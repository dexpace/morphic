package operation

import (
	"encoding/json/jsontext"
	"slices"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"

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

// partEncodings builds the multipart/form per-part wire config, keyed by each
// body-model property's PropID. A part is included when it carries an explicit
// encoding entry or is itself a repeated (array) or file (binary) part. body is
// the TypeID the content's own schema position lowered to.
//
// It returns the map with no keys yet: keys and structural flags read the model
// behind body, which may be a node a $ref promised and nothing has built yet
// (compile.Types.Promise), so partKeys settles both once every node exists. It
// is nil where the body declares no part; one left with no key reads alike.
func partEncodings(c lowering.Ctx, ts *compile.Types, media *soa.MediaType, mediaPtr jsontext.Pointer, body ir.TypeID) (map[ir.PropID]ir.PartEncoding, ir.Unmodeled, []ir.Diagnostic) {
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
		pe, partUnmodeled, partDiags := buildPartEncoding(c, ts, part, encMap, mediaPtr)
		diags = append(diags, partDiags...)
		// Kept whether or not the part's encoding ends up empty: an entry declaring
		// only x-* lowers to an empty PartEncoding that is left out of the map, and
		// what it did declare would go with it.
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
	members := partMembers(reg, k.body)
	for _, p := range k.parts {
		prop := members[p.name]
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
// contributes. A name declared more than once is one part, at its first
// declaration's place, and every declaration is kept for the structural flags
// (dedupeParts). The branches are the
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
// composed body holds its parts on the models it composes, so a key derived
// from the composed node's pointer would name a property that exists nowhere.
// Deriving is the answer for a body with no model in the IR, such as a schema
// declaring properties beside an enum or scalar type.
func partPropID(prop *ir.Property, d ids.Document, wire string, schemaPtr jsontext.Pointer) ir.PropID {
	if prop != nil {
		return prop.ID
	}
	return d.Prop(schemaPtr + ids.Ptr("properties", wire))
}

// partMembers returns, by wire name, the declaration the flattening of the type
// body denotes takes for each of its members (ir-design §4.3), which is the one
// an encoding key names: what a reader of the body's flattened shape looks up.
// A body standing for no model has none.
func partMembers(reg ir.TypeRegistry, body ir.TypeID) map[string]*ir.Property {
	flat := ir.FlattenedProperties(reg, body)
	members := make(map[string]*ir.Property, len(flat.Properties))
	for _, fp := range flat.Properties {
		members[fp.Property.EffectiveWireName()] = fp.Property
	}
	return members
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
func buildPartEncoding(c lowering.Ctx, ts *compile.Types, part bodyPart, encMap *sequencedmap.Map[string, *soa.Encoding], mediaPtr jsontext.Pointer) (ir.PartEncoding, ir.Unmodeled, []ir.Diagnostic) {
	pe := ir.PartEncoding{}
	var unmodeled ir.Unmodeled
	var diags []ir.Diagnostic
	if encMap != nil {
		if enc, ok := encMap.Get(part.name); ok {
			pe, unmodeled, diags = encodingConfig(c, ts, enc,
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

// maxBodyAliasHops bounds the alias chains bodyModel and partTypeFlags follow
// over the IR (styleguide bounded-everything rule), which the IR permits to be
// cyclic. The composition partMembers reads is ir.FlattenedProperties's to
// bound: it meets each model and alias once.
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
// under this pointer and the keys address none (see
// schema.preserveUnhomedKeywords).
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
// be omitted from the encoding map. It names every field, which
// TestPartEncodingEmpty_ReadsEveryField holds it to: one it skipped would drop
// a part declaring nothing else.
func partEncodingEmpty(pe ir.PartEncoding) bool {
	return len(pe.ContentTypes) == 0 && len(pe.Headers) == 0 &&
		!pe.Multi && !pe.Filename && pe.Style == "" && pe.Explode == nil && !pe.AllowReserved
}
