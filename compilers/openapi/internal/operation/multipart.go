// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"encoding/json/jsontext"
	"maps"
	"slices"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// partEncodings builds a form body's per-part wire config, keyed by the PropID
// of the body property each part names (docs/compilers/openapi.md#multipart-encoding).
// A part is included when it carries an encoding entry or a default content
// type or is a repeated or file part; keepUnkeyedEncodings keeps whole an
// entry naming none. body is the TypeID the content's schema lowered to.
//
// It returns the map with no keys yet, and the content's Unmodeled: keys,
// flags and defaults read the type behind body, which a $ref may have promised
// (compile.Types.Promise), so partKeys settles them, and each entry's records,
// once every node exists.
func partEncodings(c lowering.Ctx, w walk, media *soa.MediaType, mt string, mediaPtr jsontext.Pointer, body ir.TypeID) (map[ir.PropID]ir.PartEncoding, ir.Unmodeled, []ir.Diagnostic) {
	bodyWalk := walkBodyParts(c.Bindings(), media.GetSchema())
	parts := bodyWalk.parts
	unmodeled, keptDiags := keepUnkeyedEncodings(c, media, mediaPtr, bodyWalk, unkeyedFormEntry)
	diags := append(diagPartWalkCut(c, bodyWalk, mediaPtr+ids.Ptr("schema")), keptDiags...)
	if len(parts) == 0 {
		return nil, unmodeled, diags
	}
	encMap := media.GetEncoding()
	keys := partKeys{c: c, body: body, schema: mediaPtr + ids.Ptr("schema"), mediaType: lowering.NormalizeMediaType(mt)}
	for _, part := range parts {
		kp, partDiags := lowerPart(c, w, part, encMap, mediaPtr, keys.mediaType)
		diags = append(diags, partDiags...)
		keys.parts = append(keys.parts, kp)
		if kp.entry != nil && unmodeled == nil {
			// The content holds this map, which Settle writes each entry's records to.
			unmodeled = ir.Unmodeled{}
		}
	}
	keys.kept = unmodeled
	keys.out = make(map[ir.PropID]ir.PartEncoding, len(keys.parts))
	w.types.Defer(keys)
	return keys.out, unmodeled, diags
}

// lowerPart lowers the entry of part, a part of a form body under mt, a
// normalized media type, and reads what its declarations say of it being a file
// and of its default content type (keyedPart).
func lowerPart(c lowering.Ctx, w walk, part bodyPart, encMap *sequencedmap.Map[string, *soa.Encoding], mediaPtr jsontext.Pointer, mt string) (keyedPart, []ir.Diagnostic) {
	pe, partUnmodeled, diags := buildPartEncoding(c, w, part.name, encMap, mediaPtr)
	kp := keyedPart{
		part:      part,
		encoding:  pe,
		unmodeled: partUnmodeled,
		at:        mediaPtr + ids.Ptr("encoding", part.name),
		file:      readPartFile(c, part, pe.ContentTypes, isMultipart(mt)),
		encoded:   readContentEncoding(c, part.schemas),
		defaulted: true,
	}
	if enc, ok := encMap.Get(part.name); ok && enc != nil {
		kp.entry = enc.GetRootNode()
		kp.defaulted = takesDefault(lowering.EncodingRules(c), enc, mt)
		diags = append(diags, unknownNestedKeys(c, part, kp.entry, kp.at)...)
	}
	return kp, diags
}

// unknownNestedKeys warns of each key of the nested encoding the entry of part,
// written at at, writes, which encodingConfig keeps verbatim, naming no
// property the value it applies to declares (unknownEncodingKey): OpenAPI 3.2
// applies one as the Media Type Object's encoding is applied, to each item of
// an array part. Where the walk of a declaration leaves a key undecided
// (partWalk.decidesKeys), no key is judged.
func unknownNestedKeys(c lowering.Ctx, part bodyPart, entry *yaml.Node, at jsontext.Pointer) []ir.Diagnostic {
	nested := nodeview.Pairs(nodeview.Child(entry, "encoding"))
	if len(nested) == 0 {
		return nil
	}
	declared := map[string]bool{}
	for _, js := range part.schemas {
		if s := schemaOf(c.Bindings(), js); isArraySchema(s) {
			js = s.GetItems()
		}
		w := walkBodyParts(c.Bindings(), js)
		if !w.decidesKeys() {
			return nil
		}
		for _, p := range w.parts {
			declared[p.name] = true
		}
	}
	var diags []ir.Diagnostic
	for _, pair := range nested {
		if !declared[pair.Key] {
			diags = append(diags, unknownEncodingKey(c, pair.Key, "part", at+ids.Ptr("encoding", pair.Key)))
		}
	}
	return diags
}

// takesDefault reports whether a part whose Encoding Object is enc, under mt, a
// normalized media type, takes the content type OpenAPI gives it by default
// (docs/compilers/openapi.md#multipart-encoding): enc writes no contentType,
// even an empty one, and no style, explode or allowReserved where it takes
// effect, which replaces the content type, implicit or explicit, with the
// arrangement a query parameter's would take.
func takesDefault(rules oas.EncodingRules, enc *soa.Encoding, mt string) bool {
	return enc.ContentType == nil &&
		(enc.Style == nil || !rules.TakesEffect("style", mt)) &&
		(enc.Explode == nil || !rules.TakesEffect("explode", mt)) &&
		(enc.AllowReserved == nil || !rules.TakesEffect("allowReserved", mt))
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
// names none of the parts w found, under Unmodeled at
// annotation.EntryKey("encoding", key), with one info each, which why completes
// (docs/compilers/openapi.md#multipart-encoding). Where the walk read every
// position of the body that can declare a property, and no catch-all admits
// the key, the key names no property and draws a warning besides
// (unknownEncodingKey). Nothing an entry kept declares is lowered or judged
// (GitHub #1662). An entry that is no object is skipped, since load reports it.
func keepUnkeyedEncodings(c lowering.Ctx, media *soa.MediaType, mediaPtr jsontext.Pointer, w partWalk, why string) (ir.Unmodeled, []ir.Diagnostic) {
	walked := make(map[string]struct{}, len(w.parts))
	for _, part := range w.parts {
		walked[part.name] = struct{}{}
	}
	var kept ir.Unmodeled
	var diags []ir.Diagnostic
	for name, enc := range media.GetEncoding().All() {
		if _, isPart := walked[name]; isPart {
			continue
		}
		at := mediaPtr + ids.Ptr("encoding", name)
		written, keepDiags := keepEntry(c, &kept, name, enc.GetRootNode(), at, why)
		diags = append(diags, keepDiags...)
		if written && w.decidesKeys() && why != unkeyedOtherEntry {
			diags = append(diags, unknownEncodingKey(c, name, "body", at))
		}
	}
	return kept, diags
}

// keepEntry keeps entry, the encoding entry named name written at at, whole
// under u at annotation.EntryKey("encoding", name), with one info saying, as
// why completes it, why Content.Encoding has no key for it. The model reads an
// entry that is no mapping as one with no node, so nothing is kept for it and
// load's error is its one finding. written reports whether the entry was kept.
func keepEntry(c lowering.Ctx, u *ir.Unmodeled, name string, entry *yaml.Node, at jsontext.Pointer, why string) (written bool, diags []ir.Diagnostic) {
	written, diags = annotation.PreserveNodeInto(u, annotation.EntryKey("encoding", name), entry,
		ir.ReasonNoIRHome, c.Place().At(at))
	if !written {
		return false, diags
	}
	return true, append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
		"encoding entry %q %s, so Content.Encoding has no key for it; kept verbatim under Unmodeled", name, why))
}

// unknownEncodingKey reports the encoding entry named name, written at at, whose
// key names no property the schema of owner, its body or its part, declares,
// which OpenAPI requires of every key and gives no effect where it does not
// hold.
func unknownEncodingKey(c lowering.Ctx, name, owner string, at jsontext.Pointer) ir.Diagnostic {
	return c.DiagAt(ir.SeverityWarning, diag.UnknownEncodingKey, at,
		"encoding entry %q names no property the %s's schema declares, which OpenAPI requires of an encoding key, "+
			"so the entry has no effect", name, owner)
}

// partKeys keys a form body's part encodings once the registry is whole
// (compile.Settlement): Settle fills out, the map the body's Content holds,
// with each part left carrying something, keyed by the first declaration the
// body's type holds of its name (partMembers). kept is the content's
// Unmodeled, which takes each entry's records where its part is keyed, and
// the entry whole where it is not. c is the context the content lowered in,
// schema where its schema is written, and mediaType its normalized media type.
type partKeys struct {
	c         lowering.Ctx
	out       map[ir.PropID]ir.PartEncoding
	kept      ir.Unmodeled
	body      ir.TypeID
	schema    jsontext.Pointer
	mediaType string
	parts     []keyedPart
}

// keyedPart is one part: the encoding its entry declares and what that entry
// keeps of its own, the entry as written and where, what its declarations and
// that entry say of it being a file (readPartFile), whether its declarations
// write contentEncoding (readContentEncoding), and whether it takes the
// default content type (takesDefault).
type keyedPart struct {
	file      partFile
	unmodeled ir.Unmodeled
	entry     *yaml.Node
	at        jsontext.Pointer
	part      bodyPart
	encoding  ir.PartEncoding
	encoded   contentEncoding
	defaulted bool
}

// unkeyedPartEntry says, in its info, why the entry of a part the walk found
// keys nothing: the body's type holds no property of that name, as one
// declaring properties beside a keyword lowering it to no model does.
const unkeyedPartEntry = "names a part the form body's schema declares, but the type its schema lowered to holds no property of that name"

// Settle keys each part's encoding, writing no node of reg. It reads each
// property a part names as the merge settled it, which Finish has done by then:
// a fold is a completion (compile.Types.Complete).
func (k partKeys) Settle(reg ir.TypeRegistry) []ir.Diagnostic {
	node := func(id ir.TypeID) (ir.TypeDef, bool) {
		td, ok := reg[id]
		return td, ok
	}
	members, complete := partMembers(reg, k.body)
	var diags []ir.Diagnostic
	if !complete {
		diags = append(diags, k.c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, k.schema,
			"form body's type nests past %d unions, so a part only a union below them declares has no Content.Encoding key", maxPartUnions))
	}
	multipart := isMultipart(k.mediaType)
	rules := lowering.EncodingRules(k.c)
	for _, p := range k.parts {
		decls := members[p.part.name]
		if len(decls) == 0 {
			diags = append(diags, k.keepWhole(p)...)
			continue
		}
		maps.Copy(k.kept, p.unmodeled)
		pe := withDefaultContentType(node, rules, decls, p, p.file.apply(partFlags(node, k.c.Bindings(), decls, p, multipart)))
		if partEncodingEmpty(pe) {
			continue
		}
		k.out[decls[0].ID] = pe
	}
	return diags
}

// keepWhole keeps the entry of p, a part the body's type holds no property for,
// whole, as keepUnkeyedEncodings keeps one naming no part. Where the entry
// cannot be kept, what its lowering kept of it stands instead, so what the
// lowering announced holds. A part with no entry keeps nothing: the schema
// lowering kept the properties declaring it, which say what its flags would.
func (k partKeys) keepWhole(p keyedPart) []ir.Diagnostic {
	if p.entry == nil {
		return nil
	}
	kept := k.kept // the map partEncodings made, which the content holds
	written, diags := keepEntry(k.c, &kept, p.part.name, p.entry, p.at, unkeyedPartEntry)
	if !written {
		maps.Copy(k.kept, p.unmodeled)
	}
	return diags
}

// bodyPart is one form part: the name keying its encoding entry, and every
// schema declaring it, which decide the structural flags when the body's type
// cannot.
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
	// capped marks an inline branch nested past schema.MaxAllOfFlattenDepth
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
	// refs says where each $ref ends, as the lowering read it (schemaOf).
	refs    *typed.Bindings
	entered map[*oas3.Schema]int
	cut     map[*oas3.Schema]bool
	// capped holds the allOf entries of an inline branch past the flattening
	// depth, which the walk did not follow.
	capped map[*oas3.Schema]bool
	open   *bool
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
	open := false
	v := partVisits{
		refs:    refs,
		entered: map[*oas3.Schema]int{},
		cut:     map[*oas3.Schema]bool{},
		capped:  map[*oas3.Schema]bool{},
		open:    &open,
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
	w.open = open
	return w
}

// bodyParts returns the parts a form body declares: the properties written on
// the schema, then each allOf branch's, then each oneOf and anyOf branch's, of
// the schema or an inline allOf branch. A name declared more than once is one
// part, at its first declaration's place, with every declaration kept
// (dedupeParts). The allOf branches are the lowering's own
// (schema.FlattenedAllOf); a $ref branch and a union branch are followed here,
// up to maxPartRefHops. v records the fewest hops each target was entered at,
// so one met again by no shorter path yields no parts, and the bounds that cut
// the walk.
func bodyParts(js *oas3.JSONSchema[oas3.Referenceable], hops int, v partVisits) partWalk {
	s := schemaOf(v.refs, js)
	if s == nil || hops > maxPartRefHops {
		if s != nil {
			v.cut[s] = true
		}
		return partWalk{}
	}
	if prev, ok := v.entered[s]; ok && prev <= hops {
		return partWalk{}
	}
	v.entered[s] = hops
	v.note(js, s)
	w := partWalk{parts: propertyParts(s)}
	branches := []*oas3.Schema{s}
	for _, br := range schema.FlattenedAllOf(s) {
		if br.Ref {
			w.parts = append(w.parts, bodyParts(br.Schema, hops+1, v).parts...)
			continue
		}
		bs := schemaOf(v.refs, br.Schema)
		v.note(br.Schema, bs)
		w.parts = append(w.parts, propertyParts(bs)...)
		branches = append(branches, bs)
		if br.Capped {
			v.markCapped(bs)
		}
	}
	for _, b := range branches {
		for _, union := range slices.Concat(b.GetOneOf(), b.GetAnyOf()) {
			w.parts = append(w.parts, bodyParts(union, hops+1, v).parts...)
		}
	}
	w.parts = dedupeParts(w.parts)
	return w
}

// note records in v.open that s, the schema js is entered as, leaves a key
// undecided: it admits members it names no property for (admitsMembers), or js
// or s declares properties where the walk reads none (declaresUnread).
func (v partVisits) note(js *oas3.JSONSchema[oas3.Referenceable], s *oas3.Schema) {
	if s != nil && (admitsMembers(s) || declaresUnread(v.refs, js, s)) {
		*v.open = true
	}
}

// admitsMembers reports whether s holds a catch-all for members it names no
// property for: an additionalProperties or unevaluatedProperties other than
// false, or a patternProperties. A key such a member could have is not shown
// to name nothing, though OpenAPI asks a key to name a declared property.
func admitsMembers(s *oas3.Schema) bool {
	catchAll := func(js *oas3.JSONSchema[oas3.Referenceable]) bool {
		return annotation.Written(js) && !annotation.IsFalseSchema(js)
	}
	return catchAll(s.GetAdditionalProperties()) || catchAll(s.GetUnevaluatedProperties()) ||
		s.GetPatternProperties().Len() > 0
}

// declaresUnread reports whether a property can be declared, or a member
// admitted, where the part walk reads none: under what s, the end of the chain
// of $refs js starts, writes that it does not read (writesUnread), past a $ref
// resolving to nothing, and beside a $ref on the chain, which the walk reads
// to its end (schemaOf).
func declaresUnread(refs *typed.Bindings, js *oas3.JSONSchema[oas3.Referenceable], s *oas3.Schema) bool {
	if writesUnread(s) {
		return true
	}
	for range maxBodyAliasHops {
		if js == nil || !js.IsSchema() || js.GetSchema().Ref == nil {
			return false
		}
		site := js.GetSchema()
		if refs.End(js) == nil || writesUnread(site) || admitsMembers(site) || len(site.GetAllOf()) > 0 ||
			len(site.GetOneOf()) > 0 || len(site.GetAnyOf()) > 0 || site.GetProperties().Len() > 0 {
			return true
		}
		js = refs.Declared(js)
	}
	return true
}

// writesUnread reports whether s writes a keyword that can declare a property
// the part walk does not read: if, then, else, dependentSchemas or a
// $dynamicRef.
func writesUnread(s *oas3.Schema) bool {
	return s.GetIf() != nil || s.GetThen() != nil || s.GetElse() != nil || s.GetDependentSchemas().Len() > 0 ||
		nodeview.Child(s.GetRootNode(), "$dynamicRef") != nil
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
func diagPartWalkCut(c lowering.Ctx, w partWalk, at jsontext.Pointer) []ir.Diagnostic {
	var out []ir.Diagnostic
	if w.capped {
		out = append(out, c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, at,
			"form body composition is nested past the flattening depth of %d, so a part reached only below it has no Content.Encoding key",
			schema.MaxAllOfFlattenDepth))
	}
	if w.hopsSpent {
		out = append(out, c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, at,
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
// type body denotes takes for each of its members
// (docs/ir/models.md#the-flattening), the one an encoding key names first:
// what a reader of the body's flattened shape looks up. A body standing for a
// union, as one a composition is distributed across does, has its variants'
// members, through the unions nested in them, the first variant declaring a
// name naming it (docs/ir/payloads.md). A body standing for no model has none.
// complete is false where more than maxPartUnions unions were read, past
// which a member no union before them declares is missing.
func partMembers(reg ir.TypeRegistry, body ir.TypeID) (members map[string][]*ir.Property, complete bool) {
	node := func(id ir.TypeID) (ir.TypeDef, bool) {
		td, ok := reg[id]
		return td, ok
	}
	holders, complete := memberHolders(node, body)
	members = map[string][]*ir.Property{}
	for _, holder := range holders {
		for _, fp := range ir.FlattenedProperties(reg, holder).Properties {
			name := fp.Property.EffectiveWireName()
			if !slices.ContainsFunc(members[name], func(p *ir.Property) bool { return p.ID == fp.Property.ID }) {
				members[name] = append(members[name], fp.Property)
			}
		}
	}
	return members, complete
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
		v := top[0].Type.Target
		u, nested := bodyNode(node, v).(*ir.Union)
		switch {
		case !nested || u == nil:
			holders = append(holders, v)
		case seen[u]:
		case read == maxPartUnions:
			return holders, false
		default:
			seen[u] = true
			read++
			stack = append(stack, u.Variants)
		}
	}
	return holders, true
}

// partFlags returns p's encoding with the structural flags of the part decls
// declare, read off the types the allOf merge settled on (declarationFlags),
// multipart saying whether the body is multipart. Where a type is a kind
// partTypeFlags cannot decide, they are read off every schema declaring the
// part, as refs resolves them (declaredFlags), a reading left until then since
// it walks a union's branches.
func partFlags(node func(ir.TypeID) (ir.TypeDef, bool), refs *typed.Bindings, decls []*ir.Property, p keyedPart, multipart bool) ir.PartEncoding {
	pe := p.encoding
	if multi, file, decided := declarationFlags(node, decls, multipart); decided {
		pe.Multi, pe.Filename = multi, file
		return pe
	}
	pe.Multi, pe.Filename = declaredFlags(refs, p.part.schemas, multipart)
	return pe
}

// declarationFlags reads the structural flags of a part whose value may be of
// any of the types decls declare, as partTypeFlags reads a union's members:
// multi where any is, and file where every one is. decided is false where
// partTypeFlags cannot decide one of them.
func declarationFlags(node func(ir.TypeID) (ir.TypeDef, bool), decls []*ir.Property, multipart bool) (multi, file, decided bool) {
	file = multipart && len(decls) > 0
	for _, d := range decls {
		declMulti, declFile, ok := partTypeFlags(node, d.Type, multipart)
		if !ok {
			return false, false, false
		}
		multi, file = multi || declMulti, file && declFile
	}
	return multi, file, true
}

// partTypeFlags reads the structural flags of a part from its type, as
// memberFlags does, or, for a union, from each of its members (unionMembers):
// multi where any is, since a part that may repeat travels as repeated parts,
// and, in a multipart body, file where every one is raw bytes (rawBytes) or a
// list or tuple of them. A member memberFlags cannot answer for is neither.
// decided is false where memberFlags cannot answer, or the union's members are
// not read.
func partTypeFlags(node func(ir.TypeID) (ir.TypeDef, bool), ref ir.TypeRef, multipart bool) (multi, file, decided bool) {
	td, found := node(ref.Target)
	union, isUnion := td.(*ir.Union)
	if !found || !isUnion {
		return memberFlags(node, ref, multipart)
	}
	members, ok := unionMembers(node, union)
	if !ok {
		return false, false, false
	}
	file = multipart && len(members) > 0
	for _, member := range members {
		memberMulti, memberFile, _ := memberFlags(node, member, multipart)
		multi, file = multi || memberMulti, file && memberFile
	}
	return multi, file, true
}

// maxPartUnions bounds the unions unionMembers and memberHolders read through
// (styleguide bounded-everything rule). A part reaching more is read off its
// declarations, as a part of a kind memberFlags cannot answer for is.
const maxPartUnions = 64

// unionMembers returns the variants of union through the unions nested in it,
// each read once, so a union naming itself ends. ok is false where a variant
// names no node or more than maxPartUnions unions are read. Each part naming a
// union reads it again (GitHub #1460).
func unionMembers(node func(ir.TypeID) (ir.TypeDef, bool), union *ir.Union) (members []ir.TypeRef, ok bool) {
	pending := []*ir.Union{union}
	seen := map[ir.TypeID]bool{}
	for range maxPartUnions {
		if len(pending) == 0 {
			return members, true
		}
		next := pending[0]
		pending = pending[1:]
		for _, v := range next.Variants {
			td, found := node(v.Type.Target)
			if !found {
				return nil, false
			}
			nested, isUnion := td.(*ir.Union)
			switch {
			case !isUnion:
				members = append(members, v.Type)
			case !seen[v.Type.Target]:
				seen[v.Type.Target] = true
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
			return true, multipart && rawBytes(node, t.Elem), true
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
// name: content types, headers, style and explode. Its structural flags, Multi
// and Filename, are partKeys' to read once every type has lowered (partFlags).
func buildPartEncoding(c lowering.Ctx, w walk, name string, encMap *sequencedmap.Map[string, *soa.Encoding], mediaPtr jsontext.Pointer) (ir.PartEncoding, ir.Unmodeled, []ir.Diagnostic) {
	enc, ok := encMap.Get(name)
	if !ok {
		return ir.PartEncoding{}, nil, nil
	}
	return encodingConfig(c, w, enc, mediaPtr+ids.Ptr("encoding", name), ids.Scope("encoding", name))
}

// declaredFlags reads a part's structural flags off every schema declaring it,
// as refs resolves them: multi where any declares an array (declaresArray),
// and, in a multipart body, file where any is a string of format binary
// (schemaIsFilePart).
func declaredFlags(refs *typed.Bindings, schemas []*oas3.JSONSchema[oas3.Referenceable], multipart bool) (multi, file bool) {
	for _, js := range schemas {
		if s := schemaOf(refs, js); s != nil {
			multi = multi || declaresArray(refs, s)
			file = file || multipart && schemaIsFilePart(s)
		}
	}
	return multi, file
}

// schemaIsFilePart reports whether a multipart part schema is a file: a string
// of format binary.
func schemaIsFilePart(s *oas3.Schema) bool {
	return s.GetFormat() == "binary" && slices.Contains(s.GetType(), oas3.SchemaTypeString)
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
		s, alias := td.(*ir.Scalar)
		if !alias || s == nil || s.Base == nil {
			return td
		}
		id = s.Base.Target
	}
	return nil
}

// partEncodingEmpty reports whether a PartEncoding carries no information and can
// be omitted from the encoding map. It names every field, which
// TestPartEncodingEmpty_ReadsEveryField holds it to: one it skipped would drop
// a part declaring nothing else.
func partEncodingEmpty(pe ir.PartEncoding) bool {
	return len(pe.ContentTypes) == 0 && len(pe.Headers) == 0 && !pe.Multi && !pe.Filename &&
		pe.Style == "" && pe.Explode == nil && !pe.AllowReserved && pe.Inferred == ""
}
