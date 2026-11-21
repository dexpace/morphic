// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
	"maps"
	"slices"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/compilers/openapi/internal/service"
	"github.com/dexpace/morphic/compilers/openapi/internal/specrules"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// partEncodings builds a form body's per-part wire config, keyed by the PropertyID
// of the body property each part names (docs/compilers/openapi.md#multipart-encoding),
// a part with an entry, a default content type or a flag; keepUnkeyedEncodings
// keeps whole an entry naming none. body is the TypeID the schema lowered to.
//
// It returns the parts' and catch-alls' maps, empty, and the content's
// Unmodeled: what they hold reads the type behind body, which a $ref may have
// promised (compile.Types.ReserveNodeAtCoordinate), so partKeys settles them
// and each entry's records once every node exists, for every body stating a
// schema.
func partEncodings(serviceContext service.Ctx, loweringState operationLoweringState, media *soa.MediaType, mt string, mediaPtr jsontext.Pointer, body ir.TypeID) (
	map[ir.PropertyID]ir.PartEncoding, map[ir.TypeID]ir.CatchAllParts, ir.Unmodeled, []ir.Diagnostic,
) {
	bodyWalk := walkBodyParts(serviceContext.Ctx, media.GetSchema())
	parts := bodyWalk.parts
	unmodeled, keptDiags := keepUnkeyedEncodings(serviceContext, media, mediaPtr, bodyWalk, unkeyedFormEntry)
	diags := append(diagPartWalkCut(serviceContext, bodyWalk, mediaPtr+ids.Ptr("schema")), keptDiags...)
	if media.GetSchema() == nil {
		return nil, nil, unmodeled, diags
	}
	encMap := media.GetEncoding()
	keys := partKeys{
		c: serviceContext, body: body, schema: mediaPtr + ids.Ptr("schema"), mediaType: service.NormalizeMediaType(mt),
		catchAllValues: bodyWalk.catchAllValues,
	}
	for _, part := range parts {
		kp, partDiags := lowerPart(serviceContext, loweringState, part, encMap, mediaPtr, keys.mediaType)
		diags = append(diags, partDiags...)
		keys.parts = append(keys.parts, kp)
		if kp.entry != nil && unmodeled == nil {
			// The content holds this map, which Settle writes each entry's records to.
			unmodeled = ir.Unmodeled{}
		}
	}
	keys.kept = unmodeled
	keys.out = make(map[ir.PropertyID]ir.PartEncoding, len(keys.parts))
	keys.catchAll = map[ir.TypeID]ir.CatchAllParts{}
	loweringState.types.Defer(keys)
	return keys.out, keys.catchAll, unmodeled, diags
}

// lowerPart lowers the entry of part, a part of a form body under mt, a
// normalized media type, and reads whether it takes its default content type
// (keyedPart).
func lowerPart(serviceContext service.Ctx, loweringState operationLoweringState, part bodyPart, encMap *sequencedmap.Map[string, *soa.Encoding], mediaPtr jsontext.Pointer, mt string) (keyedPart, []ir.Diagnostic) {
	pe, partUnmodeled, diags := buildPartEncoding(serviceContext, loweringState, part.name, encMap, mediaPtr, mt)
	kp := keyedPart{
		part:      part,
		encoding:  pe,
		unmodeled: partUnmodeled,
		at:        mediaPtr + ids.Ptr("encoding", part.name),
		defaulted: true,
	}
	if enc, ok := encMap.Get(part.name); ok && enc != nil {
		kp.entry = enc.GetRootNode()
		kp.defaulted = takesDefault(service.EncodingRules(serviceContext), enc, mt)
		diags = append(diags, unknownNestedKeys(serviceContext, part, kp.entry, kp.at)...)
	}
	return kp, diags
}

// unknownNestedKeys warns of each key of the nested encoding the entry of part,
// written at at, writes, which encodingConfig keeps verbatim, naming no
// property the value it applies to declares (unknownEncodingKey): OpenAPI 3.2
// applies one as the Media Type Object's encoding is applied, to each item of
// an array part. Where the walk of a declaration leaves a key undecided
// (partWalk.decidesKeys), no key is judged.
func unknownNestedKeys(serviceContext service.Ctx, part bodyPart, entry *yaml.Node, at jsontext.Pointer) []ir.Diagnostic {
	nested := nodeview.Pairs(nodeview.Child(entry, "encoding"))
	if len(nested) == 0 {
		return nil
	}
	declared := map[string]bool{}
	for _, js := range part.schemas {
		if s := schemaOf(serviceContext.Bindings(), js); isArraySchema(s) {
			js = s.GetItems()
		}
		partWalk := walkBodyParts(serviceContext.Ctx, js)
		if !partWalk.decidesKeys() {
			return nil
		}
		for _, bodyPart := range partWalk.parts {
			declared[bodyPart.name] = true
		}
	}
	var diags []ir.Diagnostic
	for _, pair := range nested {
		if !declared[pair.Key] {
			diags = append(diags, unknownEncodingKey(serviceContext, pair.Key, "part", at+ids.Ptr("encoding", pair.Key)))
		}
	}
	return diags
}

// takesDefault reports whether a part whose Encoding Object is enc, under mt, a
// normalized media type, takes the content type OpenAPI gives it by default
// (docs/compilers/openapi.md#multipart-encoding): enc writes no contentType,
// even an empty one, and arranges nothing (arranges), since an arrangement
// replaces the content type, implicit or explicit.
func takesDefault(rules specrules.EncodingRules, enc *soa.Encoding, mt string) bool {
	return enc.ContentType == nil && !arranges(rules, enc, mt)
}

// unkeyedFormEntry, unkeyedFileEntry and unkeyedOtherEntry say why an encoding
// entry keys nothing, in its info: the first on a form body, where the walk may
// not tell a misspelt key from a catch-all's, the second on a file body, the
// third under a media type that is neither multipart nor form.
const (
	unkeyedFormEntry  = "matches no property read from the form body's schema"
	unkeyedFileEntry  = "belongs to a body that is file content, which has no parts"
	unkeyedOtherEntry = "belongs to a media type that is neither multipart nor form, where it has no effect"
)

// keepUnkeyedEncodings keeps whole each entry of media's encoding map whose key
// names none of the parts partWalkValue found, under Unmodeled at
// annotation.EntryKey("encoding", key), with one info each, which why completes
// (docs/compilers/openapi.md#multipart-encoding). Where the walk decides the
// key names no property it also warns (unknownEncodingKey), save outside form
// content, where load reports the whole field. Such an entry, and any of a body
// with no parts, has no effect, so is out of scope; one the walk cannot
// decide, as a catch-all may take it, has no IR home. An entry that is no
// object, which load reports, is skipped.
func keepUnkeyedEncodings(serviceContext service.Ctx, media *soa.MediaType, mediaPtr jsontext.Pointer, partWalkValue partWalk, why string) (ir.Unmodeled, []ir.Diagnostic) {
	walked := make(map[string]struct{}, len(partWalkValue.parts))
	for _, part := range partWalkValue.parts {
		walked[part.name] = struct{}{}
	}
	var kept ir.Unmodeled
	var diags []ir.Diagnostic
	for name, enc := range media.GetEncoding().All() {
		if _, isPart := walked[name]; isPart {
			continue
		}
		at := mediaPtr + ids.Ptr("encoding", name)
		// An entry the walk decides keys nothing, every entry of a body with no
		// parts among them, has no effect where it is written; one a catch-all
		// may take has no IR home yet. Under a media type that is neither
		// multipart nor form, load reports the whole field as having no effect.
		reason := ir.ReasonNoIRHome
		if partWalkValue.decidesKeys() {
			reason = ir.ReasonOutOfScope
		}
		written, keepDiags := keepEntry(serviceContext, &kept, name, enc.GetRootNode(), at, why, reason)
		diags = append(diags, keepDiags...)
		if written && partWalkValue.decidesKeys() && why != unkeyedOtherEntry {
			diags = append(diags, unknownEncodingKey(serviceContext, name, "body", at))
		}
	}
	return kept, diags
}

// keepEntry keeps entry, the encoding entry named name written at at, whole
// under unmodeled at annotation.EntryKey("encoding", name) for reason, with one
// info saying, as why completes it, why Content.Encoding has no key for it. The
// model reads an entry that is no mapping as one with no node, so nothing is
// kept for it and load's error is its one finding. written reports whether the
// entry was kept.
func keepEntry(serviceContext service.Ctx, unmodeled *ir.Unmodeled, name string, entry *yaml.Node, at jsontext.Pointer, why string,
	reason ir.UnmodeledReason,
) (written bool, diags []ir.Diagnostic) {
	written, diags = annotation.PreserveNodeInto(unmodeled, annotation.EntryKey("encoding", name), entry,
		reason, serviceContext.Place().At(at))
	if !written {
		return false, diags
	}
	return true, append(diags, serviceContext.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
		"encoding entry %q %s, so Content.Encoding has no key for it; kept verbatim under Unmodeled", name, why))
}

// unknownEncodingKey reports the encoding entry named name, written at at, whose
// key names no property the schema of owner, its body or its part, declares,
// which OpenAPI requires of every key and gives no effect where it does not
// hold.
func unknownEncodingKey(serviceContext service.Ctx, name, owner string, at jsontext.Pointer) ir.Diagnostic {
	return serviceContext.DiagAt(ir.SeverityWarning, diag.UnknownEncodingKey, at,
		"encoding entry %q names no property the %s's schema declares, which OpenAPI requires of an encoding key, "+
			"so the entry has no effect", name, owner)
}

// partKeys keys a form body's part encodings once the registry is whole
// (compile.Settlement): Settle fills out, the body's Content.Encoding, with an
// entry for each declaration the body's type holds of a part's name
// (partMembers) left carrying something, and catchAll, its CatchAllEncoding
// (settleCatchAlls). kept is the content's Unmodeled, which takes each entry's
// records where its part is keyed, and the entry whole where it is not. c is
// the context the content lowered in, schema where its schema is written,
// mediaType its normalized media type, and catchAllValues the catch-alls'
// value schemas the part walk met.
type partKeys struct {
	c              service.Ctx
	out            map[ir.PropertyID]ir.PartEncoding
	catchAll       map[ir.TypeID]ir.CatchAllParts
	kept           ir.Unmodeled
	body           ir.TypeID
	schema         jsontext.Pointer
	mediaType      string
	parts          []keyedPart
	catchAllValues []*oas3.JSONSchema[oas3.Referenceable]
}

// keyedPart is one part: the encoding its entry declares and what that entry
// keeps of its own, the entry as written and where, and whether it takes the
// default content type (takesDefault).
type keyedPart struct {
	unmodeled ir.Unmodeled
	entry     *yaml.Node
	at        jsontext.Pointer
	part      bodyPart
	encoding  ir.PartEncoding
	defaulted bool
}

// unkeyedPartEntry says, in its info, why the entry of a part the walk found
// keys nothing: the body's type holds no property of that name, as one
// declaring properties beside a keyword lowering it to no model does.
const unkeyedPartEntry = "names a part the form body's schema declares, but the type its schema lowered to holds no property of that name"

// Settle keys each part's encoding, writing no node of reg. It reads each
// property a part names as the merge settled it, which Finish has done by then:
// a fold is a completion (compile.Types.QueueSettlementBeforeDeferredOnes).
func (k partKeys) Settle(_ context.Context, reg ir.TypeRegistry) []ir.Diagnostic {
	node := func(id ir.TypeID) (ir.TypeDef, bool) {
		td, ok := reg[id]
		return td, ok
	}
	members, complete, unread := partMembers(reg, k.body)
	var diags []ir.Diagnostic
	if !complete {
		diags = append(diags, k.c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, k.schema,
			"form body's type nests past %d unions, so a part only a union below them declares has no Content.Encoding key", maxPartUnions))
	}
	for _, unreadNarrowing := range unread {
		diags = append(diags, k.c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, k.schema,
			"form body's type %q narrows a union whose shapes are not read (%s), so a part only its variants declare "+
				"has no Content.Encoding key", unreadNarrowing.model, unreadNarrowing.why))
	}
	for _, keyedPart := range k.parts {
		decls := members[keyedPart.part.name]
		if len(decls) == 0 {
			diags = append(diags, k.keepWhole(keyedPart)...)
			continue
		}
		maps.Copy(k.kept, keyedPart.unmodeled)
		schemas := declarationSchemas(k.c, keyedPart.part, decls)
		for _, property := range decls {
			if pe := k.declared(node, keyedPart, bodyPart{name: keyedPart.part.name, schemas: schemas[property.ID]}, property); !partEncodingEmpty(pe) {
				k.out[property.ID] = pe
			}
		}
	}
	k.settleCatchAlls(reg, node)
	return append(diags, k.partless(node)...)
}

// partless reports a multipart/form-data or url-encoded body whose type, read
// through its unions, stands for no model, no map, no object value and not the
// top type, so it names no part to send (docs/compilers/openapi.md#multipart-encoding). A
// union read past its bound, or a type reaching no node, which load reports,
// is not judged.
func (k partKeys) partless(node func(ir.TypeID) (ir.TypeDef, bool)) []ir.Diagnostic {
	if k.mediaType != "multipart/form-data" && k.mediaType != "application/x-www-form-urlencoded" {
		return nil
	}
	holders, complete := memberHolders(node, k.body)
	if !complete || slices.ContainsFunc(holders, func(typeID ir.TypeID) bool { return mayHoldParts(bodyNode(node, typeID)) }) {
		return nil
	}
	return []ir.Diagnostic{k.c.DiagAt(ir.SeverityWarning, diag.PartlessFormBody, k.schema,
		"form body's schema declares no object, so it names no part to send; lowered as declared")}
}

// mayHoldParts reports whether td, a form body's type or a variant of it, may
// hold a part: a model, a map, an object value, whose members name its parts,
// or the top type, or no node, which another finding reports.
func mayHoldParts(td ir.TypeDef) bool {
	switch t := td.(type) {
	case *ir.Model, *ir.MapT, nil:
		return true
	case *ir.Literal:
		return t.Value.Kind == ir.ValueObject
	case *ir.Primitive:
		return t.PrimitiveKind == ir.PrimitiveAny
	default:
		return false
	}
}

// declared returns the encoding of keyedPartValue as property, one of the
// declarations the body's type holds of its name, declares it: the entry
// written, with the structural flags and the default content type read off
// property and part, the schemas declaring it (declarationSchemas), so a part
// two variants declare apart reads as each declares it.
func (k partKeys) declared(node func(ir.TypeID) (ir.TypeDef, bool), keyedPartValue keyedPart, part bodyPart, property *ir.Property) ir.PartEncoding {
	one := []*ir.Property{property}
	multipart := isMultipart(k.mediaType)
	unions := partUnionsOf(k.c)
	pe := partFlags(unions, node, k.c.Bindings(), one, part, keyedPartValue.encoding, multipart)
	pe = readPartFile(k.c, part, keyedPartValue.encoding.ContentTypes, multipart).apply(pe)
	if !keyedPartValue.defaulted {
		return pe
	}
	return withDefaultContentType(unions, node, service.EncodingRules(k.c), property, readContentEncoding(k.c, part.schemas), pe)
}

// declarationSchemas returns, for each of decls, the declarations the body's
// type holds of part's name, the schemas of part declaring it, in source
// order: the one written where it is (declaredProperty), and every one written
// where none of decls is, which a declaration the merge folded into one of
// them, an allOf entry's, is. So one variant's declaration is read apart
// from another's, and each schema is placed once.
func declarationSchemas(serviceContext service.Ctx, part bodyPart, decls []*ir.Property) map[ir.PropertyID][]*oas3.JSONSchema[oas3.Referenceable] {
	out := make(map[ir.PropertyID][]*oas3.JSONSchema[oas3.Referenceable], len(decls))
	if len(decls) < 2 {
		for _, property := range decls {
			out[property.ID] = part.schemas
		}
		return out
	}
	held := make(map[ir.PropertyID]bool, len(decls))
	for _, property := range decls {
		held[property.ID] = true
	}
	for _, js := range part.schemas {
		if id, ok := declaredProperty(serviceContext, js); ok && held[id] {
			out[id] = append(out[id], js)
			continue
		}
		for _, property := range decls {
			out[property.ID] = append(out[property.ID], js)
		}
	}
	return out
}

// declaredProperty returns the ID of the property whose schema is js, as the
// schema lowering names one by where it is read, and false where the bindings
// do not place js in a document the compile read. Where they place it
// nowhere, the zero position names the source, which is read and not used.
func declaredProperty(serviceContext service.Ctx, js *oas3.JSONSchema[oas3.Referenceable]) (ir.PropertyID, bool) {
	at, placed := serviceContext.Bindings().Site(js)
	dc, read := service.InDocument(serviceContext, at.Doc)
	return dc.IDs().Property(at.Pointer), placed && read
}

// keepWhole keeps the entry of keyedPartValue, a part the body's type holds no
// property for, whole, as keepUnkeyedEncodings keeps one naming no part. Where
// the entry cannot be kept, what its lowering kept of it stands instead, so
// what the lowering announced holds. A part with no entry keeps nothing: the
// schema lowering kept the properties declaring it, which say what its flags
// would.
func (k partKeys) keepWhole(keyedPartValue keyedPart) []ir.Diagnostic {
	if keyedPartValue.entry == nil {
		return nil
	}
	kept := k.kept // the map partEncodings made, which the content holds
	written, diags := keepEntry(k.c, &kept, keyedPartValue.part.name, keyedPartValue.entry, keyedPartValue.at, unkeyedPartEntry, ir.ReasonNoIRHome)
	if !written {
		maps.Copy(k.kept, keyedPartValue.unmodeled)
	}
	return diags
}

// bodyPart is one form part: the name keying its encoding entry, and every
// schema declaring it, off which what the body's type cannot tell is read:
// whether it is a file as a body's schema is (readPartFile), whether it
// repeats where its type cannot decide (declaredMulti), and its
// contentEncoding.
type bodyPart struct {
	name    string
	schemas []*oas3.JSONSchema[oas3.Referenceable]
}

// maxPartRefHops bounds how many $ref targets and union branches bodyParts
// enters down a composition. Inline allOf wrappers are not counted:
// schema.FlattenedAllOf reads them, with the cap the lowering uses. A target is
// entered again only when a shorter path reaches it, so an allOf cycle (GitHub
// #642) costs one visit, and the bound is reported only for a target no path
// reaches within it.
const maxPartRefHops = 16

// partWalk is what bodyParts found, which bound cut it short, and what keeps
// it from deciding that a key names no property (decidesKeys).
type partWalk struct {
	parts []bodyPart
	// catchAllValues are the value schemas of the catch-alls the walk met:
	// each additionalProperties, unevaluatedProperties and patternProperties
	// entry admitting a member, in walk order.
	catchAllValues []*oas3.JSONSchema[oas3.Referenceable]
	// capped marks an inline entry nested past statement.MaxFlattenDepth
	// whose own allOf entries no path entered.
	capped bool
	// hopsSpent marks a $ref target or union branch past maxPartRefHops that
	// no path entered.
	hopsSpent bool
	// open marks a schema the walk entered that admits members it does not
	// name (admitsMembers) or declares properties where the walk reads none
	// (declaresUnread).
	open bool
}

// decidesKeys reports whether the walk read every property the body's schema
// can declare, and no catch-all admits a member it does not name, so a key
// naming none of its parts names no property at all.
func (w partWalk) decidesKeys() bool { return !w.capped && !w.hopsSpent && !w.open }

// partVisits is what one body's part walk has met: the fewest hops each target
// was entered at, the targets met past a bound, and whether a schema entered
// leaves a key undecided (partWalk.open).
type partVisits struct {
	// c is the run's context: where each $ref ends, as the lowering read it
	// (schemaOf), and the statement the lowering flattens an allOf of.
	c       lowering.Ctx
	entered map[*oas3.Schema]int
	cut     map[*oas3.Schema]bool
	// capped holds the allOf entries of an inline entry past the flattening
	// depth, which the walk did not follow.
	capped map[*oas3.Schema]bool
	open   *bool
	// catchAllValues collects partWalk.catchAllValues.
	catchAllValues *[]*oas3.JSONSchema[oas3.Referenceable]
}

// markCapped records the allOf entries of b, an inline entry past the
// flattening depth, as targets the walk did not follow.
func (v partVisits) markCapped(b *oas3.Schema) {
	for _, c := range b.GetAllOf() {
		if cs := schemaOf(v.c.Bindings(), c); cs != nil {
			v.capped[cs] = true
		}
	}
}

// walkBodyParts runs bodyParts over one body, whose $refs end where refs says,
// and reports a bound only for a target that no path entered.
func walkBodyParts(loweringContext lowering.Ctx, js *oas3.JSONSchema[oas3.Referenceable]) partWalk {
	open := false
	var catchAllValues []*oas3.JSONSchema[oas3.Referenceable]
	partVisitsValue := partVisits{
		c:              loweringContext,
		entered:        map[*oas3.Schema]int{},
		cut:            map[*oas3.Schema]bool{},
		capped:         map[*oas3.Schema]bool{},
		open:           &open,
		catchAllValues: &catchAllValues,
	}
	partWalkValue := bodyParts(js, 0, partVisitsValue)
	for s := range partVisitsValue.cut {
		if _, ok := partVisitsValue.entered[s]; !ok {
			partWalkValue.hopsSpent = true
		}
	}
	for s := range partVisitsValue.capped {
		if _, ok := partVisitsValue.entered[s]; !ok {
			partWalkValue.capped = true
		}
	}
	partWalkValue.open, partWalkValue.catchAllValues = open, catchAllValues
	return partWalkValue
}

// bodyParts returns the parts a form body declares: the properties written on
// the schema, then each allOf entry's, then each oneOf and anyOf branch's, of
// the schema or an inline allOf entry. A name declared more than once is one
// part, at its first declaration's place, with every declaration kept
// (dedupeParts). The allOf entries are the lowering's own
// (schema.FlattenedAllOf); a $ref branch and a union branch are followed here,
// up to maxPartRefHops. v records the fewest hops each target was entered at,
// so one met again by no shorter path yields no parts, and the bounds that cut
// the walk.
func bodyParts(js *oas3.JSONSchema[oas3.Referenceable], hops int, partVisitsValue partVisits) partWalk {
	s := schemaOf(partVisitsValue.c.Bindings(), js)
	if s == nil || hops > maxPartRefHops {
		if s != nil {
			partVisitsValue.cut[s] = true
		}
		return partWalk{}
	}
	if prev, ok := partVisitsValue.entered[s]; ok && prev <= hops {
		return partWalk{}
	}
	partVisitsValue.entered[s] = hops
	partVisitsValue.note(js, s)
	partWalkValue := partWalk{parts: propertyParts(s)}
	branches := []*oas3.Schema{s}
	for _, br := range schema.FlattenedAllOf(partVisitsValue.c, statementOf(partVisitsValue.c, js)) {
		if br.Ref {
			partWalkValue.parts = append(partWalkValue.parts, bodyParts(statement.ParsedAt(br.Schema), hops+1, partVisitsValue).parts...)
			continue
		}
		bs := schemaOf(partVisitsValue.c.Bindings(), statement.ParsedAt(br.Schema))
		partVisitsValue.note(statement.ParsedAt(br.Schema), bs)
		partWalkValue.parts = append(partWalkValue.parts, propertyParts(bs)...)
		branches = append(branches, bs)
		if br.Capped {
			partVisitsValue.markCapped(bs)
		}
	}
	for _, b := range branches {
		for _, union := range slices.Concat(b.GetOneOf(), b.GetAnyOf()) {
			partWalkValue.parts = append(partWalkValue.parts, bodyParts(union, hops+1, partVisitsValue).parts...)
		}
	}
	partWalkValue.parts = dedupeParts(partWalkValue.parts)
	return partWalkValue
}

// note records in v.open that s, the schema js is entered as, leaves a key
// undecided: it admits members it names no property for (admitsMembers), or js
// or s declares properties where the walk reads none (declaresUnread). It
// collects the value schemas of the catch-alls s holds in v.catchAllValues.
func (v partVisits) note(js *oas3.JSONSchema[oas3.Referenceable], s *oas3.Schema) {
	*v.catchAllValues = append(*v.catchAllValues, catchAllValues(v.c.Statements(), s)...)
	if s != nil && (admitsMembers(v.c.Statements(), s) || declaresUnread(v.c, js, s)) {
		*v.open = true
	}
}

// admitsMembers reports whether s holds a catch-all for members it names no
// property for: an additionalProperties or unevaluatedProperties other than
// false, or a patternProperties. A key such a member could have is not shown
// to name nothing, though OpenAPI asks a key to name a declared property.
func admitsMembers(memoizingSchemaReader *statement.MemoizingSchemaReader, s *oas3.Schema) bool {
	return admitting(memoizingSchemaReader, s.GetAdditionalProperties()) || admitting(memoizingSchemaReader, s.GetUnevaluatedProperties()) ||
		s.GetPatternProperties().Len() > 0
}

// admitting reports whether js, a catch-all's schema as memoizingSchemaReader
// reads it, admits a member: it is written, and is not the false schema.
func admitting(memoizingSchemaReader *statement.MemoizingSchemaReader, js *oas3.JSONSchema[oas3.Referenceable]) bool {
	truth := memoizingSchemaReader.Of(js).Truth
	return truth != statement.Unwritten && !truth.ReadsFalse()
}

// catchAllValues returns the value schemas of the catch-alls s, which may be
// nil, holds that admit a member (admitting), additionalProperties,
// unevaluatedProperties and each patternProperties entry, in that order.
func catchAllValues(memoizingSchemaReader *statement.MemoizingSchemaReader, s *oas3.Schema) []*oas3.JSONSchema[oas3.Referenceable] {
	var out []*oas3.JSONSchema[oas3.Referenceable]
	for _, js := range []*oas3.JSONSchema[oas3.Referenceable]{s.GetAdditionalProperties(), s.GetUnevaluatedProperties()} {
		if admitting(memoizingSchemaReader, js) {
			out = append(out, js)
		}
	}
	for _, js := range s.GetPatternProperties().All() {
		out = append(out, js)
	}
	return out
}

// declaresUnread reports whether a property can be declared, or a member
// admitted, where the part walk reads none: under what s, the end of the chain
// of $refs js starts, writes that it does not read (writesUnread), past a $ref
// resolving to nothing, and beside a $ref on the chain, which the walk reads
// to its end (schemaOf).
func declaresUnread(loweringContext lowering.Ctx, js *oas3.JSONSchema[oas3.Referenceable], s *oas3.Schema) bool {
	if writesUnread(s, "") {
		return true
	}
	refs := loweringContext.Bindings()
	for range maxBodyAliasHops {
		keyword := refs.RefKeyword(js)
		if keyword == "" {
			return false
		}
		site := js.GetSchema()
		if refs.End(js) == nil || writesUnread(site, keyword) || admitsMembers(loweringContext.Statements(), site) || len(site.GetAllOf()) > 0 ||
			len(site.GetOneOf()) > 0 || len(site.GetAnyOf()) > 0 || site.GetProperties().Len() > 0 {
			return true
		}
		js = refs.Declared(js)
	}
	return true
}

// writesUnread reports whether s writes a keyword that can declare a property
// the part walk does not read: if, then, else, dependentSchemas or a
// $dynamicRef, unless that is the reference keyword s is a reference site under
// (typed.Bindings.RefKeyword), which the walk follows.
func writesUnread(s *oas3.Schema, reference string) bool {
	return s.GetIf() != nil || s.GetThen() != nil || s.GetElse() != nil || s.GetDependentSchemas().Len() > 0 ||
		reference != "$dynamicRef" && nodeview.Child(s.GetRootNode(), "$dynamicRef") != nil
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

// diagPartWalkCut reports the bounds that cut a body's part walk short, so a
// part past one, which gets no Content.Encoding key and no structural flags,
// is not lost without a word; an entry written for it is kept whole
// (keepUnkeyedEncodings).
func diagPartWalkCut(serviceContext service.Ctx, partWalkValue partWalk, at jsontext.Pointer) []ir.Diagnostic {
	var out []ir.Diagnostic
	if partWalkValue.capped {
		out = append(out, serviceContext.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, at,
			"form body composition is nested past the flattening depth of %d, so a part reached only below it has no Content.Encoding key",
			statement.MaxFlattenDepth))
	}
	if partWalkValue.hopsSpent {
		out = append(out, serviceContext.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, at,
			"form body composition follows $refs and union branches past %d hops, so a part reached only beyond them has no Content.Encoding key",
			maxPartRefHops))
	}
	return out
}

// dedupeParts keeps the first entry of each part name, in order, and collects
// the schemas of its later declarations onto it. A name redeclared across allOf
// or union branches is one part on the wire, so it must key one encoding entry
// and read its flags from every declaration rather than depend on which branch
// was walked last.
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

// partMembers returns, by wire name, the declarations the flattening of the
// type body denotes takes for each member, each an encoding key
// (docs/ir/payloads.md): a union body's variants' too, through nested unions,
// and each shape's of a body or variant narrowing a union (shapeFlattenings),
// in that order. complete is false past maxPartUnions unions, where a member
// only a union beyond them declares is missing, and unread names each model
// whose shapes were not read.
func partMembers(reg ir.TypeRegistry, body ir.TypeID) (members map[string][]*ir.Property, complete bool, unread []unreadNarrowing) {
	node := func(id ir.TypeID) (ir.TypeDef, bool) {
		td, ok := reg[id]
		return td, ok
	}
	holders, complete := memberHolders(node, body)
	members = map[string][]*ir.Property{}
	for _, holder := range holders {
		flat := ir.FlattenedProperties(reg, holder)
		shapes, why := shapeFlattenings(reg, holder, flat, maxNarrowedSteps)
		if why != nil {
			unread = append(unread, *why)
		}
		for _, flattening := range append([]ir.Flattening{flat}, shapes...) {
			for _, fp := range flattening.Properties {
				name := fp.Property.EffectiveWireName()
				if !slices.ContainsFunc(members[name], func(property *ir.Property) bool { return property.ID == fp.Property.ID }) {
					members[name] = append(members[name], fp.Property)
				}
			}
		}
	}
	return members, complete, unread
}

// maxNarrowedSteps bounds the steps ir.NarrowedShapes takes for one holder of
// a form body's parts (styleguide bounded-everything rule), the bound the
// emitter plan gives it (plan.MaxComposedSize), which pass.Validate's reading
// of the same keys must not fall below.
const maxNarrowedSteps = 100_000

// unreadNarrowing is a model holding a form body's parts that narrows a union
// whose shapes partMembers did not read, and why.
type unreadNarrowing struct {
	model ir.TypeID
	why   string
}

// shapeFlattenings returns the flattening of each shape the model holder
// takes where it narrows a union (ir.NarrowedShapes), whose own flattening is
// flat: none where flat lists no conjunct naming no model, as no union is then
// reached. why names a union narrowed whose shapes are not listed, as
// Narrowing.Undistributed says or past limit steps.
func shapeFlattenings(reg ir.TypeRegistry, holder ir.TypeID, flat ir.Flattening, limit int) ([]ir.Flattening, *unreadNarrowing) {
	if len(flat.Unflattened) == 0 {
		return nil, nil
	}
	narrowing, _, ok := ir.NarrowedShapes(reg, holder, limit)
	switch {
	case !ok:
		return nil, &unreadNarrowing{model: holder, why: fmt.Sprintf("past %d steps", limit)}
	case narrowing.Undistributed != "":
		return nil, &unreadNarrowing{model: holder, why: string(narrowing.Undistributed)}
	}
	out := make([]ir.Flattening, 0, len(narrowing.Shapes))
	for _, shape := range narrowing.Shapes {
		out = append(out, shape.Flattening)
	}
	return out, nil
}

// memberHolders returns the types whose members a body typed body has: body
// itself, or, where it stands for a union, its variants that stand for none,
// in source order, each union read once, depth first, so a union naming
// itself ends. complete is false past maxPartUnions unions.
func memberHolders(node func(ir.TypeID) (ir.TypeDef, bool), body ir.TypeID) (holders []ir.TypeID, complete bool) {
	root, ok := bodyNode(node, body).(*ir.Union)
	if !ok || root == nil {
		return []ir.TypeID{body}, true
	}
	seen := map[*ir.Union]bool{root: true}
	stack := [][]ir.Variant{root.Variants}
	for read := 1; len(stack) > 0; {
		top := stack[len(stack)-1]
		if len(top) == 0 {
			stack = stack[:len(stack)-1]
			continue
		}
		stack[len(stack)-1] = top[1:]
		typeID := top[0].Type.Target
		union, nested := bodyNode(node, typeID).(*ir.Union)
		switch {
		case !nested || union == nil:
			holders = append(holders, typeID)
		case seen[union]:
		case read == maxPartUnions:
			return holders, false
		default:
			seen[union] = true
			read++
			stack = append(stack, union.Variants)
		}
	}
	return holders, true
}

// partFlags returns pe, the encoding written for part, with the structural
// flags of the part decls declare, read off the types the allOf merge settled
// on (declarationFlags), multipart saying whether the body is multipart. Where
// a type is a kind partTypeFlags cannot decide, RepeatsPerArrayItem is read off
// every schema of part, as refs resolves them (declaredMulti), a reading left
// until then since it walks a union's branches. unions is the run's reading of
// the unions parts name.
func partFlags(unions *partUnions, node func(ir.TypeID) (ir.TypeDef, bool), refs *typed.Bindings, decls []*ir.Property, part bodyPart, pe ir.PartEncoding, multipart bool) ir.PartEncoding {
	multi, file, decided := declarationFlags(unions, node, decls, multipart)
	if !decided {
		multi = declaredMulti(refs, part.schemas)
	}
	pe.RepeatsPerArrayItem, pe.HasFilename = multi, file
	return pe
}

// declarationFlags reads the structural flags of a part whose value may be of
// any of the types decls declare, as partTypeFlags reads a union's members:
// multi where any is, and file where every one is, a type partTypeFlags cannot
// decide being no file, as a union's member of such a kind is. decided is
// false where partTypeFlags cannot decide one of them.
func declarationFlags(unions *partUnions, node func(ir.TypeID) (ir.TypeDef, bool), decls []*ir.Property, multipart bool) (multi, file, decided bool) {
	file, decided = multipart && len(decls) > 0, true
	for _, property := range decls {
		declMulti, declFile, ok := partTypeFlags(unions, node, property.Type, multipart)
		multi, file, decided = multi || declMulti, file && declFile, decided && ok
	}
	return multi, file, decided
}

// partTypeFlags reads the structural flags of a part from its type, as
// memberFlags does, or, for a union, from each of its members (unionMembers):
// multi where any is, since a part that may repeat travels as repeated parts,
// and, in a multipart body, file where every one is raw bytes (rawBytes) or a
// list or tuple of them. A member memberFlags cannot answer for is neither.
// decided is false where memberFlags cannot answer, or the union's members are
// not read. A union's flags are read once a run (partUnions.flagsOf).
func partTypeFlags(unions *partUnions, node func(ir.TypeID) (ir.TypeDef, bool), ref ir.TypeRef, multipart bool) (multi, file, decided bool) {
	td, found := node(ref.Target)
	union, isUnion := td.(*ir.Union)
	if !found || !isUnion {
		return memberFlags(node, ref, multipart)
	}
	return unions.flagsOf(node, union, multipart)
}

// maxPartUnions bounds the unions unionMembers and memberHolders read through
// (styleguide bounded-everything rule). A part reaching more reads as one of a
// kind memberFlags cannot answer for does (partFlags).
const maxPartUnions = 64

// unionMembers returns the variants of union through the unions nested in it,
// each read once, so a union naming itself ends. ok is false where a variant
// names no node or more than maxPartUnions unions are read. A part reads it
// through partUnions, once a run.
func unionMembers(node func(ir.TypeID) (ir.TypeDef, bool), union *ir.Union) (members []ir.TypeRef, ok bool) {
	pending := []*ir.Union{union}
	seen := map[ir.TypeID]bool{}
	for range maxPartUnions {
		if len(pending) == 0 {
			return members, true
		}
		next := pending[0]
		pending = pending[1:]
		for _, variant := range next.Variants {
			td, found := node(variant.Type.Target)
			if !found {
				return nil, false
			}
			nested, isUnion := td.(*ir.Union)
			switch {
			case !isUnion:
				members = append(members, variant.Type)
			case !seen[variant.Type.Target]:
				seen[variant.Type.Target] = true
				pending = append(pending, nested)
			}
		}
	}
	if len(pending) > 0 {
		return nil, false
	}
	return members, true
}

// memberFlags reads the structural flags of a part from its type: multi when
// it is a list or tuple, and, in a multipart body, file when it is bytes no
// scalar re-encodes (rawBytes), following the alias scalars a hoisted position
// puts over the primitive, or a list or tuple whose every element is such bytes
// (rawTuple). A `format: byte` part is base64 text, and a url-encoded body has
// no file parts. decided is false for a type of a kind it cannot answer for.
// The walk is bounded by maxBodyAliasHops.
func memberFlags(node func(ir.TypeID) (ir.TypeDef, bool), ref ir.TypeRef, multipart bool) (multi, file, decided bool) {
	id := ref.Target
	for range maxBodyAliasHops {
		td, found := node(id)
		if !found {
			return false, false, false
		}
		switch t := td.(type) {
		case *ir.List:
			return true, multipart && rawBytes(node, t.ElementType), true
		case *ir.Tuple:
			return true, multipart && rawTuple(node, t), true
		case *ir.Primitive:
			return false, multipart && rawBytes(node, ref), true
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

// buildPartEncoding assembles the explicit encoding config of the part named
// name under mt, a normalized media type: content types, headers and the
// arrangement. Its structural flags, RepeatsPerArrayItem and HasFilename, are
// partKeys' to read once every type has lowered (partFlags).
func buildPartEncoding(serviceContext service.Ctx, loweringState operationLoweringState, name string, encMap *sequencedmap.Map[string, *soa.Encoding], mediaPtr jsontext.Pointer, mt string) (ir.PartEncoding, ir.Unmodeled, []ir.Diagnostic) {
	enc, ok := encMap.Get(name)
	if !ok {
		return ir.PartEncoding{}, nil, nil
	}
	return encodingConfig(serviceContext, loweringState, enc, mediaPtr+ids.Ptr("encoding", name), ids.Scope("encoding", name), mt)
}

// declaredMulti reads whether a part repeats off every schema declaring it, as
// refs resolves them: where any declares an array (declaresArray).
func declaredMulti(refs *typed.Bindings, schemas []*oas3.JSONSchema[oas3.Referenceable]) bool {
	return slices.ContainsFunc(schemas, func(js *oas3.JSONSchema[oas3.Referenceable]) bool {
		s := schemaOf(refs, js)
		return s != nil && declaresArray(refs, s)
	})
}

// declaresArray reports whether s declares a repeated part: the array type on
// s, or, where s writes no type of its own, which would hold beside every
// branch, on a branch of a union it writes, a part that may repeat traveling as
// repeated parts.
func declaresArray(refs *typed.Bindings, s *oas3.Schema) bool {
	if len(withoutNull(s.GetType())) > 0 {
		return schemaIsArray(s)
	}
	return slices.ContainsFunc(slices.Concat(s.GetAnyOf(), s.GetOneOf()), func(branch *oas3.JSONSchema[oas3.Referenceable]) bool {
		return schemaIsArray(schemaOf(refs, branch))
	})
}

// schemaIsArray reports whether a schema declares the array type (a repeated
// multipart part).
func schemaIsArray(s *oas3.Schema) bool {
	return slices.Contains(s.GetType(), oas3.SchemaTypeArray)
}

// maxBodyAliasHops bounds the alias chains bodyNode, partTypeFlags and the
// default content type follow over the IR (styleguide bounded-everything
// rule), which the IR permits to be cyclic, and the $ref chain declaresUnread
// reads. The composition partMembers reads is ir.FlattenedProperties's to
// bound: it meets each model and alias once.
const maxBodyAliasHops = 64

// bodyNode returns the node body stands for past the alias scalars a
// $ref-with-siblings position hoists, nil where the chain reaches no node or
// runs past maxBodyAliasHops.
func bodyNode(node func(ir.TypeID) (ir.TypeDef, bool), body ir.TypeID) ir.TypeDef {
	id := body
	for range maxBodyAliasHops {
		td, found := node(id)
		if !found {
			return nil
		}
		scalar, alias := td.(*ir.Scalar)
		if !alias || scalar == nil || scalar.Base == nil {
			return td
		}
		id = scalar.Base.Target
	}
	return nil
}

// partEncodingEmpty reports whether a PartEncoding carries no information and can
// be omitted from the encoding map. It names every field, which
// TestPartEncodingEmpty_ReadsEveryField holds it to: one it skipped would drop
// a part declaring nothing else.
func partEncodingEmpty(pe ir.PartEncoding) bool {
	return len(pe.ContentTypes) == 0 && len(pe.Headers) == 0 && !pe.RepeatsPerArrayItem && !pe.HasFilename &&
		pe.Style == "" && pe.Explode == nil && !pe.AllowReserved && pe.Inferred == ""
}
