// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"cmp"
	"encoding/json/jsontext"
	"slices"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
	"github.com/dexpace/morphic/compilers/openapi/internal/specrules"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// maxFallbackReferenceHops bounds how many $refs one use reads on through
// (styleguide bounded-everything rule), a loop of allOfs each naming the next
// ending on it.
const maxFallbackReferenceHops = 64

// Use is what speaks for one position beside the schema written there: the
// entries of its allOf read as written beside it, then what it falls back to.
type Use struct {
	FoldedAllOfEntries []annotation.FoldedAllOfEntry
	// Referents is what the position falls back to, nearest first
	// (docs/compilers/openapi.md#ref-use-site).
	Referents []*statement.Schema
	// places[i] is where Referents[i] is written, in whichever document;
	// appendFallbackReferent and appendFallbackReferents write the two
	// together.
	places []referentPlace
	// narrowed is what the position admits where a referent may say more, and nil
	// where every referent's default stands.
	narrowed *admitted
	// narrowedFrom is the first of Referents past the value set narrowing the
	// position: one before it writes that set or stands beside it, so its default
	// is the position's own, as a plain enum's is.
	narrowedFrom int
	// nullable says the position's reference admits null, whatever narrowed says.
	nullable bool
	// deprecatedBase says the node at the position composes, as its Base, a
	// schema a keyword or a key deprecates, as a decorator over a deprecated
	// target does.
	deprecatedBase bool
}

// Site is u as annotation.Read takes it, for schema, the schema written at the
// position. A position that lost its $ref still reads it, so a Site here is a
// reference site whatever schema writes.
func (u Use) Site(schema *statement.Schema) annotation.SchemaPositionAnnotationSources {
	return annotation.SchemaPositionAnnotationSources{Kind: annotation.Reference, Node: schema, FoldedAllOfEntries: u.FoldedAllOfEntries, Referents: u.Referents}
}

// own is u without what the position falls back to: what it writes itself,
// beside the entries of its allOf read as written there.
func (u Use) own() Use {
	return Use{FoldedAllOfEntries: u.FoldedAllOfEntries, nullable: u.nullable}
}

// SiteAt is Site for schema written at pointer, saying what the dialect schema
// is read in spells otherwise, which annotation.Read keeps (dialectIn).
func (u Use) SiteAt(loweringContext lowering.Ctx, schema *statement.Schema, pointer jsontext.Pointer) annotation.SchemaPositionAnnotationSources {
	site := u.Site(schema)
	site.Dialect = dialectIn(loweringContext.Rules().At(loweringContext.Position(pointer)), schema)
	return site
}

// dialectIn is what schema, read with rules, spells otherwise than 2020-12: the
// keyword naming its identifier, whether the dialect ignores it beside a $ref
// schema writes, as the binder does, whether it defines `dependencies`, and
// whether it reads an `items` list as a tuple (listedTuple).
func dialectIn(rules specrules.SchemaRules, schema *statement.Schema) annotation.Dialect {
	return annotation.Dialect{Identifier: rules.Identifier(), IdentifierIgnored: rules.IgnoresRefFields() && writesRef(schema),
		Dependencies: rules.Defines("dependencies"), ListedTuple: rules.Takes("items", specrules.List)}
}

// writesRef reports whether schema writes a $ref the binder reads as one: a
// string, empty or not, beside its other keywords too
// (statement.Schema.BesideRef), or beside those of an allOf entry's rest
// (statement.Schema.WrittenBeside). A null, a mapping or a list writes none.
func writesRef(schema *statement.Schema) bool {
	if schema != nil {
		schema = cmp.Or(schema.BesideRef, schema.WrittenBeside, schema)
	}
	node := nodeview.Deref(keyNode(schema, "$ref"))
	return node != nil && node.Kind == yaml.ScalarNode
}

// Default returns the default the nearest schema of u's chain writes for the
// carrier at pointer holding schema, reading the chain hop by hop, and a
// warning at that default's own position where no ir.Value holds it. The
// carrier then takes none: a later hop's default is not what the nearest
// writes.
func (u Use) Default(loweringContext lowering.Ctx, schema *statement.Schema, pointer jsontext.Pointer) (*ir.Value, []ir.Diagnostic) {
	chain := u.Site(schema).Chain()
	own := len(chain) - len(u.Referents) + u.narrowedFrom
	for i, statementSchema := range chain {
		node := statementSchema.Default()
		if node == nil || u.narrowed != nil && i >= own && !u.takes(node) {
			continue
		}
		irValue, err := value.FromNode(node)
		if err != nil {
			return nil, []ir.Diagnostic{diag.Newf(ir.SeverityWarning, diag.UnloweredConstruct, u.defaultAt(loweringContext, i, pointer),
				"default is no value the IR holds, so no property, header or parameter reading it takes one: %s",
				diag.OneLine(err))}
		}
		return &irValue, nil
	}
	return nil, nil
}

// defaultAt places the default the schema at index i of u.Site's chain writes,
// for the carrier at pointer: the site's own, a folded entry's, or a
// referent's where it is written, in whichever document. A referent the
// bindings place nowhere is reported at the carrier, which reads it.
func (u Use) defaultAt(loweringContext lowering.Ctx, i int, pointer jsontext.Pointer) ir.Provenance {
	switch v := i - 1; {
	case i == 0:
		return loweringContext.Place().At(pointer.AppendToken("default"))
	case v < len(u.FoldedAllOfEntries):
		return loweringContext.Place().At(u.FoldedAllOfEntries[v].At.AppendToken("default"))
	}
	r := i - 1 - len(u.FoldedAllOfEntries)
	if r >= len(u.places) || !u.places[r].known {
		return loweringContext.Place().At(pointer)
	}
	at := u.places[r]
	return at.doc.At(at.pointer.AppendToken("default"))
}

// takes reports whether the position admits node: the narrowing does, or node
// is the null its reference admits.
func (u Use) takes(node *yaml.Node) bool {
	return u.narrowed.takes(node) || (u.nullable && isNullValue(node))
}

// fallbackKeys returns what the mapped keys of what u falls back to lend its
// position. They are read along the chain its keywords are, nearest first
// (Referents): the nearest deprecated, by a keyword or a key, lends its
// members, any stating a deprecation by a key states it of the use, and any
// asking for redaction asks it of the use. No `deprecated` withdraws them, as
// none withdraws another's keyword (annotation.EffectiveDeprecated).
func (u Use) fallbackKeys(loweringContext lowering.Ctx) promotion.Referent {
	from := promotion.Referent{Homes: u.deprecatedBase}
	fallbackReferent := false
	for _, schema := range u.Referents {
		read := keysOf(loweringContext, schema)
		from.Redacted = from.Redacted || read.Redacted
		from.Deprecated = from.Deprecated || read.Deprecated
		deprecated := deprecates(schema)
		from.Homes = from.Homes || deprecated
		if !fallbackReferent && (read.Deprecated || deprecated) {
			from.Members, fallbackReferent = read.Members, true
		}
	}
	return from
}

// deprecates reports whether schema writes `deprecated: true`, which a `false`
// or none does not.
func deprecates(schema *statement.Schema) bool {
	return schema != nil && schema.Notes != nil && schema.Notes.Deprecated != nil && *schema.Notes.Deprecated
}

// keysOf reads what the mapped keys of schema, a schema a use falls back to,
// lend it, as the policy answers once for each schema
// (promotion.Promoter.Referent).
func keysOf(loweringContext lowering.Ctx, schema *statement.Schema) promotion.Referent {
	promoter := loweringContext.Promoter()
	return promoter.Referent(schema, deprecates(schema), func() ir.Unmodeled {
		if schema == nil {
			return nil
		}
		return annotation.SchemaExtensionsNamed(schema.Extensions, promoter.Keys())
	})
}

// readThroughAllOfReference is how a use of a schema reads past it, into the
// one $ref its allOf writes (throughRef).
type readThroughAllOfReference struct {
	// admits is what the schema admits, read only where narrows says it is less
	// than ref admits: a default no narrowing excludes reads as beside a $ref.
	admits admitted
	// ref is nil for a refinement writing no $ref.
	ref *statement.Statement
	// lost marks a ref the position does not lower to, so no Base reaches it.
	lost bool
	// narrows reports that the schema admits less than ref admits, so admits
	// is read.
	narrows bool
}

// readThroughAllOfReferenceOf returns the read-through of schema, written at
// pointer, and false for a schema a use does not read past.
func readThroughAllOfReferenceOf(loweringContext lowering.Ctx, schema *statement.Schema, pointer jsontext.Pointer) (readThroughAllOfReference, bool) {
	if refinement, refined := refinementOf(loweringContext, schema, pointer); refined {
		readThroughAllOfReferenceValue := readThroughAllOfReference{admits: refinement.joint}
		if refinement.ref != nil {
			readThroughAllOfReferenceValue.ref, readThroughAllOfReferenceValue.lost = refinement.ref.schema, !refinement.principalIs(*refinement.ref)
			readThroughAllOfReferenceValue.narrows = readThroughAllOfReferenceValue.lost || refinement.held.narrowedTo(refinement.joint)
		}
		return readThroughAllOfReferenceValue, true
	}
	ref := throughRef(loweringContext, schema)
	if ref == nil {
		return readThroughAllOfReference{}, false
	}
	if slices.Contains(dispatchOf(schema).skipped, "allOf") {
		admissionReader := newAdmissionReader(loweringContext)
		return readThroughAllOfReference{ref: ref, lost: true, admits: admissionReader.written(schema, admissionReader.nullable), narrows: true}, true
	}
	return readThroughAllOfReference{ref: ref}, true
}

// throughRef returns the $ref a use of schema reads on through, or nil: that of
// an allOf an enum or a const beside it passed over, the one a refinement reads
// through (readThrough), or that of a decorator, an allOf composing one $ref
// beside nothing an object holds (decoratesOnly). A union beside a composing
// allOf is distributed across it or kept beside it, so that allOf lends
// nothing. It reads the source and its bindings alone, since a chain asks it of
// every link.
func throughRef(loweringContext lowering.Ctx, schema *statement.Schema) *statement.Statement {
	refs := slices.Collect(allOfRefEntries(loweringContext, schema))
	if slices.Contains(dispatchOf(schema).skipped, "allOf") {
		if len(refs) != 1 {
			return nil
		}
		return refs[0]
	}
	if decision := decisionOf(loweringContext, schema); decision.refines {
		return entryStatement(loweringContext, schema, decision.refAt)
	}
	if len(refs) != 1 || !allOfMayRefine(schema) || declaresUnion(schema) || !decoratesOnly(loweringContext, schema) {
		return nil
	}
	return refs[0]
}

// decoratesOnly reports whether schema and each entry of its allOf write
// nothing an object holds of its own: no shape keyword claiming an object
// (shapeKinds), and no entry the composition reads apart, a `false` or one
// nested past the flattening depth.
func decoratesOnly(loweringContext lowering.Ctx, schema *statement.Schema) bool {
	comp := allOfEntries(loweringContext, schema, "")
	if shapeKinds(schema)&kindObject != 0 || !refinableEntries(comp) {
		return false
	}
	return !slices.ContainsFunc(comp.entries, func(allOfEntryValue allOfEntry) bool {
		return allOfEntryValue.schema.Ref == nil && shapeKinds(allOfEntryValue.schema.Schema)&kindObject != 0
	})
}

// referenceReferentsNearestFirst returns what p's $ref says of a use, nearest
// first, as a Use's referents: what is written beside it, then what it lands on
// lends.
func (p readThroughAllOfReference) referenceReferentsNearestFirst(loweringContext lowering.Ctx) Use {
	var use Use
	use.appendFallbackReferent(p.ref.Schema, placeOf(loweringContext, p.ref))
	use.appendFallbackReferents(fallbackAnnotationsOf(loweringContext, declared(loweringContext, p.ref)))
	return use
}

// UseOf reads what speaks for a carrier holding js at pointer, which lowered to
// t (readUse), its voices marked where the outcome keeps their entry whole
// (markKeptWhole).
func UseOf(loweringContext lowering.Ctx, js *statement.Statement, typeRef ir.TypeRef, pointer jsontext.Pointer) Use {
	use := readUse(loweringContext, js, typeRef, pointer)
	use.FoldedAllOfEntries = markKeptWhole(loweringContext, js.Schema, pointer, typeRef, use.FoldedAllOfEntries)
	return use
}

// readUse reads what speaks for a carrier holding js at pointer, which lowered
// to typeRef: what a $ref names, or what readThroughAllOfReferenceOf reads on
// through, an inline principal speaking as a folded entry, as its node stands
// at the position. A composition throughRef reads no $ref through lends
// nothing: its Model holds what it composes as Base and Mixins.
func readUse(loweringContext lowering.Ctx, js *statement.Statement, typeRef ir.TypeRef, pointer jsontext.Pointer) Use {
	if js.Ref != nil {
		use := fallbackAnnotationsOf(loweringContext, declared(loweringContext, js))
		use.nullable = typeRef.Nullable
		return use
	}
	useValue := Use{FoldedAllOfEntries: positionFoldedEntries(loweringContext, js.Schema, pointer, typeRef), nullable: typeRef.Nullable}
	readThroughAllOfReference, ok := readThroughAllOfReferenceOf(loweringContext, js.Schema, pointer)
	if !ok {
		return useValue
	}
	if readThroughAllOfReference.ref == nil {
		return useValue
	}
	useValue.narrowedFrom = len(useValue.Referents)
	useValue.appendFallbackReferents(readThroughAllOfReference.referenceReferentsNearestFirst(loweringContext))
	if readThroughAllOfReference.narrows {
		useValue.narrowed = &readThroughAllOfReference.admits
	}
	return useValue
}

// nodeUse reads what speaks for the node a position owns at pointer, which
// lowered to target. A Base reaches what a $ref says, so the node falls back
// only to a $ref its lowering lost; of one its Base holds, it reads only
// whether a keyword or a key along it deprecates the node, which the node's own
// member keys then annotate (promotion.Referent.Homes).
func nodeUse(loweringContext lowering.Ctx, schema *statement.Schema, pointer jsontext.Pointer, target ir.TypeRef) Use {
	use := Use{FoldedAllOfEntries: markKeptWhole(loweringContext, schema, pointer, target, positionFoldedEntries(loweringContext, schema, pointer, target))}
	readThroughAllOfReference, ok := readThroughAllOfReferenceOf(loweringContext, schema, pointer)
	switch {
	case !ok:
	case readThroughAllOfReference.lost:
		use.appendFallbackReferents(readThroughAllOfReference.referenceReferentsNearestFirst(loweringContext))
	case readThroughAllOfReference.ref != nil:
		use.deprecatedBase = slices.ContainsFunc(readThroughAllOfReference.referenceReferentsNearestFirst(loweringContext).Referents, func(statementSchema *statement.Schema) bool {
			return deprecates(statementSchema) || keysOf(loweringContext, statementSchema).Deprecated
		})
	}
	return use
}

// fallbackReadKey keys what fallbackAnnotationsOf read of one schema.
type fallbackReadKey struct {
	schema *statement.Schema
}

// fallbackAnnotationsOf reads what js, the schema a $ref lands on, lends a use
// of it, nearest first: each hop of the chain of $refs from js that writes more
// than its $ref, the schema the chain ends on, the entries of its allOf that
// fold (lendingEntries), and past a refinement, a decorator or a narrowing enum
// whatever its own $ref lends in turn. Only that end is read for what it
// admits, so the narrowing starts past its entries. Every $ref landing on js
// asks, so it is read once a run.
func fallbackAnnotationsOf(loweringContext lowering.Ctx, js *statement.Statement) Use {
	return lowering.ReadOncePerRun(loweringContext, fallbackReadKey{js.Schema}, func() Use { return readLent(loweringContext, js) })
}

// readLent reads what fallbackAnnotationsOf remembers. A run of aliases is read
// within one turn, so only the read-throughs throughRef reads spend
// maxFallbackReferenceHops.
func readLent(loweringContext lowering.Ctx, js *statement.Statement) Use {
	var use Use
	for turn := range maxFallbackReferenceHops {
		schema, ok := use.appendFallbackChainRun(loweringContext, js)
		if !ok {
			break
		}
		if turn == 0 {
			use.narrowTo(loweringContext, schema)
		}
		through := throughRef(loweringContext, schema)
		if through == nil {
			break
		}
		use.appendFallbackReferent(through.Schema, placeOf(loweringContext, through))
		js = declared(loweringContext, through)
	}
	return use
}

// appendFallbackChainRun appends to u.Referents each alias of the run of $refs
// js starts that writes more than its $ref, then the schema the run ends on and
// its lending entries, and returns that schema. It is false, the run lending
// nothing past what it appended, where the run ends on no schema, as a chain
// that loops or fails does. The table's count of the chain's hops bounds the
// run.
func (u *Use) appendFallbackChainRun(loweringContext lowering.Ctx, js *statement.Statement) (*statement.Schema, bool) {
	var referenceSite reftable.ReferenceSite
	if js.Ref != nil && js.Ref.Bound {
		referenceSite = js.Ref.Entry
		if referenceSite.Chain.Status != reftable.Resolved {
			return nil, false
		}
	}
	for range referenceSite.Chain.Hops + 1 {
		if js.Truth != statement.Stated {
			return nil, false
		}
		at := placeOf(loweringContext, js)
		if js.Ref == nil {
			u.appendFallbackReferent(js.Schema, at)
			u.lendEntries(loweringContext, js.Schema, at)
			return js.Schema, true
		}
		if writesMoreThanRef(js.Schema) {
			u.appendFallbackReferent(js.Schema, at)
			u.lendEntries(loweringContext, js.Schema, at)
		}
		js = declared(loweringContext, js)
	}
	return nil, false
}

// narrowTo has u admit what schema admits where schema, the end of the first
// run of a chain, narrows what its own $ref admits: past it, a referent's
// default stands only where schema admits it.
func (u *Use) narrowTo(loweringContext lowering.Ctx, schema *statement.Schema) {
	if readThroughAllOfReference, ok := readThroughAllOfReferenceOf(loweringContext, schema, ""); ok && readThroughAllOfReference.narrows {
		u.narrowed, u.narrowedFrom = &readThroughAllOfReference.admits, len(u.Referents)
	}
}

// appendFallbackReferent appends schema, written at, to what u falls back to.
// It and appendFallbackReferents are the only writers of Referents and places,
// which keep each referent's place at its index.
func (u *Use) appendFallbackReferent(schema *statement.Schema, at referentPlace) {
	u.Referents = append(u.Referents, schema)
	u.places = append(u.places, at)
}

// appendFallbackReferents appends what from falls back to, each referent with
// its place, to what u falls back to.
func (u *Use) appendFallbackReferents(from Use) {
	u.Referents = append(u.Referents, from.Referents...)
	u.places = append(u.places, from.places...)
}

// lendEntries appends the lending entries of s, written at, each placed
// beneath it.
func (u *Use) lendEntries(loweringContext lowering.Ctx, schema *statement.Schema, at referentPlace) {
	for _, b := range lendingEntries(loweringContext, schema) {
		u.appendFallbackReferent(b.schema.Schema, at.beneath(b.pointer))
	}
}

// writesMoreThanRef reports whether schema, a schema writing a $ref, writes
// anything else, which a use reads; one writing its $ref alone lends nothing.
func writesMoreThanRef(schema *statement.Schema) bool {
	node := schema.Node
	return schema.Truth != statement.Stated || node == nil || node.Kind != yaml.MappingNode || len(node.Content) > 2
}

// referentPlace is where a referent is written: the document placing it and the
// pointer there, unknown where the bindings place none.
type referentPlace struct {
	doc     provenance.Doc
	pointer jsontext.Pointer
	known   bool
}

// placeOf returns where the bindings read js, unknown for a schema they do not
// know or a document the compile did not read.
func placeOf(loweringContext lowering.Ctx, js *statement.Statement) referentPlace {
	documentPointer, known := siteOf(js.Schema)
	in, read := lowering.InDocument(loweringContext, documentPointer.Doc)
	return referentPlace{doc: in.Place(), pointer: documentPointer.Pointer, known: known && read}
}

// beneath returns the place of what is written at below, relative to p, and
// unknown where p is.
func (p referentPlace) beneath(below jsontext.Pointer) referentPlace {
	p.pointer += below
	return p
}

// DecidingSchema returns the schema deciding what s lowers to, s itself or, past
// a refinement, its principal's, for a reader of the shape alone. A principal
// owning a node, a $ref or a narrower inline entry, is what the position is.
func DecidingSchema(loweringContext lowering.Ctx, schema *statement.Schema) *statement.Schema {
	for range maxFallbackReferenceHops {
		decision := decisionOf(loweringContext, schema)
		if !decision.refines || decision.principalAt < 0 {
			break
		}
		principal := entryStatement(loweringContext, schema, decision.principalAt)
		schema = principal.Schema
		if principal.Ref != nil {
			schema = refBranchTarget(loweringContext, principal)
		}
	}
	return schema
}
