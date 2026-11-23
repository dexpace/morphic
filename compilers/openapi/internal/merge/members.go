// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"cmp"
	"encoding/json/jsontext"
	"maps"
	"slices"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/ir"
)

// RedeclaredWireNameTable is what a model's composition says of the wire names
// more than one model declares: for each, the declaration its flattening takes
// (ir.FlattenedProperties), which a model settled before holds folded from
// every other its composition gives the name. A name one model alone declares
// never decides a member twice, so a settlement reads none.
type RedeclaredWireNameTable map[string]Declared

// Declared is the declaration a flattening takes for one name, and where the
// nodes of its type are written: at the declaration, or, for a member a fold
// settled, where the fold says (Merger.typeSites). A settled member keeps its
// first declaration's identity whichever gave its type, so its own pointer
// does not say (declaration.sites).
type Declared struct {
	ir.PropertyDecl
	TypeAt []ir.Provenance
}

// DeclaredAt is propertyDecl as a model's flattening takes it unsettled: its
// type is written where it is.
func DeclaredAt(propertyDecl ir.PropertyDecl) Declared {
	return Declared{PropertyDecl: propertyDecl, TypeAt: []ir.Provenance{propertyDecl.Property.Provenance}}
}

// Settle folds each member of the model more than one declaration decides,
// its own and those its entries bring
// (docs/compilers/openapi.md#allof-redeclared-property), which the flattening
// would take one of, judging none. Each fold reads them in the order the walk
// met them, an entry's where it met the entry, so a $ref and the schema it
// names written inline fold alike, but for a tie the entry settled and one of
// notes (ownFirst). tables holds the RedeclaredWireNameTable of each type the
// model composes (ComposedOf), settled first, and shared the names more than
// one model declares. It returns the model's RedeclaredWireNameTable.
func (idx *ModelPropertyDeclarations) Settle(merger Merger, tables map[ir.TypeID]RedeclaredWireNameTable, shared map[string]bool) ([]ir.Diagnostic, RedeclaredWireNameTable) {
	brought, inherited := idx.brought(merger, tables)
	diags := make([]ir.Diagnostic, 0, len(idx.byWire))
	for _, fieldValue := range slices.SortedFunc(maps.Values(idx.byWire), func(a, b *field) int { return cmp.Compare(a.pos, b.pos) }) {
		decls := slices.Clone(fieldValue.decls)
		decls[0].property = idx.m.Properties[fieldValue.pos]
		wire := decls[0].property.WireName
		decls = inOrder(decls, brought[wire])
		delete(brought, wire)
		diags = append(diags, idx.settle(merger, fieldValue.pos, decls)...)
	}
	for _, wire := range slices.Sorted(maps.Keys(brought)) {
		diags = append(diags, idx.settle(merger, -1, brought[wire])...)
	}
	return diags, idx.members(shared, inherited)
}

// members returns the model's RedeclaredWireNameTable once settled: each name
// its parts leave one declaration of, and each shared one it declares itself,
// which stands over theirs, its type written where its fold says
// (Merger.typeSites) or, where one declaration alone decides it, there.
func (idx *ModelPropertyDeclarations) members(shared map[string]bool, inherited RedeclaredWireNameTable) RedeclaredWireNameTable {
	out := make(RedeclaredWireNameTable, len(inherited))
	maps.Copy(out, inherited)
	for i := range idx.m.Properties {
		property := &idx.m.Properties[i]
		if !shared[property.WireName] {
			continue
		}
		declared := DeclaredAt(ir.PropertyDecl{Owner: idx.m.ID, Property: property})
		if at, settled := idx.typeAt[property.WireName]; settled {
			declared.TypeAt = at
		}
		out[property.WireName] = declared
	}
	return out
}

// settle folds decls, one member's declarations, where at least two are, into
// the model's property at pos, or appends one where pos is -1. A member only
// the model declares keeps its identity (Merger.fold), and so does one it
// declares beside an entry's where no other model holds the own declaration
// the walk met first (heldAlone), so two at different depths follow their
// order (GitHub #1931). Any other takes an identity in the composed namespace
// (composedMember), placed by where its declarations are written
// (assembledFrom). Where an entry brings a declaration, each report names the
// member (naming).
func (idx *ModelPropertyDeclarations) settle(merger Merger, pos int, decls []declaration) []ir.Diagnostic {
	if len(decls) < 2 {
		return nil
	}
	if !slices.ContainsFunc(decls, isBrought) {
		merged, diags := merger.fold(decls)
		idx.place(pos, merged)
		idx.typeAt[merged.WireName] = merger.typeSites(merged, decls)
		return deciding(diags, merged.ID)
	}
	own := slices.DeleteFunc(slices.Clone(decls), isBrought)
	field := composedMember(merger.IDs, idx.m, decls[0].property.WireName)
	kept := len(own) > 0 && idx.heldAlone(merger, own[0])
	if kept {
		_, field = fieldSpan(own[:min(len(own), maxFoldDeclarations)])
	}
	var assembled []jsontext.Pointer
	if !kept {
		assembled = assembledFrom(decls)
	}
	merged, diags := merger.foldAt(decls, field, assembled)
	inferred := merged.Provenance.Inferred
	if kept {
		merged.ID, merged.Provenance = own[0].property.ID, own[0].property.Provenance
	} else {
		merged.ID, merged.Provenance = merger.IDs.ComposedProperty(field), merger.Doc.At(field, assembled...)
	}
	merged.Provenance.Inferred = inferred
	if i := slices.Index(idx.m.Requires, merged.WireName); i >= 0 {
		idx.m.Requires = slices.Delete(idx.m.Requires, i, i+1)
		merged.Required = true
	}
	idx.place(pos, merged)
	idx.typeAt[merged.WireName] = merger.typeSites(merged, decls)
	return deciding(naming(diags, merged.ID), merged.ID)
}

// maxTypeSites bounds the nodes a settled member reads of a type its fold
// built, to record where they are written (Merger.builtSites; styleguide
// bounded-everything rule). A node past them reads as one a $ref reached.
const maxTypeSites = 2 * maxFoldDeclarations

// typeSites returns where the nodes of the type of merged, the member decls
// fold to, are written, so a later fold tells one hoisted at a declaration
// from one a $ref reached (hoistedAt): where the one declaration naming its
// node says; nowhere for a named node or a primitive several name, which no
// declaration writes; for a hoisted node several name, where the first whose
// places hold it says; and builtSites for a node the fold built.
func (g *Merger) typeSites(merged ir.Property, decls []declaration) []ir.Provenance {
	var naming [][]ir.Provenance
	for _, declaration := range decls {
		if declaration.property.Type.Target == merged.Type.Target {
			naming = append(naming, declaration.sites())
		}
	}
	td, registered := g.resolve(merged.Type.Target)
	switch {
	case len(naming) == 1:
		return naming[0]
	case !registered || !td.Common().Anonymous:
		return nil
	case len(naming) > 1:
		at := slices.IndexFunc(naming, func(sites []ir.Provenance) bool { return hoistedAt(td, sites) })
		return naming[max(at, 0)]
	default:
		return g.builtSites(td, decls)
	}
}

// builtSites returns where the nodes of td, which a fold of decls built, are
// written: where td stands, and, for each node it holds (heldTypes), the first
// place a declaration says that holds it. A node none holds was reached by a
// $ref, and so was what it holds. A hierarchy building a node at every level
// so records what each node holds, not its depth. It reads each node once,
// maxTypeSites of them at most.
func (g *Merger) builtSites(td ir.TypeDef, decls []declaration) []ir.Provenance {
	places := make([][]ir.Provenance, len(decls))
	for i, declaration := range decls {
		places[i] = declaration.sites()
	}
	out := []ir.Provenance{td.Common().Provenance}
	seen := map[ir.TypeID]bool{}
	queue := heldTypes(td)
	for len(queue) > 0 && len(seen) < maxTypeSites {
		id := queue[0]
		queue = queue[1:]
		node, registered := g.resolve(id)
		if seen[id] || !registered {
			continue
		}
		seen[id] = true
		if !hoistedAt(node, out) {
			at, held := writtenAt(node, places)
			if !held {
				continue
			}
			out = append(out, at)
		}
		queue = append(queue, heldTypes(node)...)
	}
	return out
}

// writtenAt returns the first of places, each declaration's in order, that
// node is hoisted at or under (hoistedUnder), and false where none is.
func writtenAt(node ir.TypeDef, places [][]ir.Provenance) (ir.Provenance, bool) {
	typeCommon := node.Common()
	for _, sites := range places {
		for _, at := range sites {
			if hoistedUnder(typeCommon, at) {
				return at, true
			}
		}
	}
	return ir.Provenance{}, false
}

// heldTypes returns the types td holds values of: a list's element, a map's
// value, a tuple's positions and what follows them, and what a model or an
// alias composes (ComposedOf).
func heldTypes(td ir.TypeDef) []ir.TypeID {
	switch t := td.(type) {
	case *ir.List:
		return []ir.TypeID{t.ElementType.Target}
	case *ir.MapT:
		return []ir.TypeID{t.Value.Target}
	case *ir.Tuple:
		held := make([]ir.TypeID, 0, len(t.ElementTypes)+1)
		for _, typeRef := range t.ElementTypes {
			held = append(held, typeRef.Target)
		}
		if t.Rest != nil {
			held = append(held, t.Rest.Target)
		}
		return held
	default:
		return ComposedOf(td)
	}
}

// composedMember is where model declares a member named wire that takes an
// identity in the composed namespace, which no schema occupies:
// <model>/allOf/properties/<name>, and one allOf deeper for a model the merge
// composed, a field's composition or a distributed variant, since the schema
// written at its pointer may settle a member of that name too. No schema is
// written at an allOf, so neither path is a declaration's.
func composedMember(documentIDNamespace ids.DocumentIDNamespace, model *ir.Model, wire string) jsontext.Pointer {
	return settledAt(model, wire, documentIDNamespace.ComposedType(model.Provenance.Pointer) == model.ID)
}

// settledAt is composedMember's path for model, one allOf deeper where deeper.
func settledAt(model *ir.Model, wire string, deeper bool) jsontext.Pointer {
	at := model.Provenance.Pointer + ids.Ptr("allOf")
	if deeper {
		at += ids.Ptr("allOf")
	}
	return at + ids.Ptr("properties", wire)
}

// assembledFrom returns where the model writes each of decls, a member's
// declarations: an entry bringing one, or its own. A member declared where no
// schema is written is assembled from them, so it is credited to an overlay
// that wrote each (provenance.Attribution.WriterOf).
func assembledFrom(decls []declaration) []jsontext.Pointer {
	out := make([]jsontext.Pointer, len(decls))
	for i, declaration := range decls {
		out[i] = declaration.reportAt()
	}
	return out
}

// isBrought reports whether declarationValue is a declaration an entry brings,
// which another model holds.
func isBrought(declarationValue declaration) bool { return declarationValue.via != nil }

// heldAlone reports whether no model but this one holds declarationValue, its
// own declaration, so the member it decides may keep declarationValue's
// identity: declarationValue is written inside the model's schema, as the body
// a distributed variant shares with its siblings is not, and no schema between
// the two stands as a node of its own (Merger.carriedApart).
func (idx *ModelPropertyDeclarations) heldAlone(merger Merger, declarationValue declaration) bool {
	model, at := idx.m.Provenance.Pointer, declarationValue.property.Provenance.Pointer
	return model.Contains(at) && !merger.carriedApart(at, model)
}

// place writes property as the model's property at pos, or appends it where pos
// is -1. A discriminator naming the property it replaces names property, and so
// does one naming by its name a member the model did not declare when the walk
// read it, which the model now declares
// (docs/compilers/openapi.md#discriminator).
func (idx *ModelPropertyDeclarations) place(pos int, property ir.Property) {
	discriminator := idx.m.Discriminator
	if pos < 0 {
		idx.m.Properties = append(idx.m.Properties, property)
		if discriminator != nil && discriminator.Property == "" && discriminator.PropertyName != "" && discriminator.PropertyName == property.Name.Source {
			discriminator.Property, discriminator.PropertyName = property.ID, ""
		}
		return
	}
	if discriminator != nil && discriminator.Property == idx.m.Properties[pos].ID {
		discriminator.Property = property.ID
	}
	idx.m.Properties[pos] = property
}

// naming returns diags, each naming id, the property the fold decided, as the
// node it locates (ir.Provenance.Node): a report about an entry's declaration
// sits where the model writes the entry, which is not where the property is.
func naming(diags []ir.Diagnostic, id ir.PropertyID) []ir.Diagnostic {
	for i := range diags {
		diags[i].Provenance.Node = string(id)
	}
	return diags
}

// deciding returns diags, each conflict between two declarations listing id,
// the property the fold decided, among what it concerns (ir.RelatedDecided):
// every model composing the two draws the report, and the run says it once
// for all of them (ReportOnce), where one node named beside its place could
// not say which.
func deciding(diags []ir.Diagnostic, id ir.PropertyID) []ir.Diagnostic {
	for i := range diags {
		if diags[i].Code != diag.ConflictingRedecl {
			continue
		}
		diags[i].Related = append(diags[i].Related, ir.Related{
			Role: ir.RelatedDecided, Provenance: ir.Provenance{Source: ir.NoSource, Node: string(id)},
		})
	}
	return diags
}

// candidate is a declaration one of the model's parts brings, and that part.
type candidate struct {
	decl Declared
	part int
}

// brought returns, by wire name, the declarations the model's parts bring, as
// tables holds them, each read where the walk met the entry naming its part
// (parts), and the one declaration each name its parts leave one of. A
// declaration two parts bring is read once, at the first, and one within
// another's composition is left out: the model composing it narrows it there
// already. A part on a cycle may hold the model's own declaration, which the
// model reads as its own, not brought. Names are read in order, as their walks
// spend the run's reads (ownership).
func (idx *ModelPropertyDeclarations) brought(merger Merger, tables map[ir.TypeID]RedeclaredWireNameTable) (map[string][]declaration, RedeclaredWireNameTable) {
	parts := idx.parts()
	byWire := map[string][]candidate{}
	for i, part := range parts {
		for wire, declared := range tables[part.target] {
			if declared.Owner != idx.m.ID {
				byWire[wire] = append(byWire[wire], candidate{decl: declared, part: i})
			}
		}
	}
	owners := newOwnership(registry{merger.resolve}, merger.Reads)
	out, inherited := map[string][]declaration{}, RedeclaredWireNameTable{}
	for _, wire := range slices.Sorted(maps.Keys(byWire)) {
		left := owners.left(byWire[wire])
		for _, cd := range left {
			out[wire] = append(out[wire], broughtDeclaration(cd.decl, &parts[cd.part]))
		}
		if len(left) == 1 {
			inherited[wire] = left[0].decl
		}
	}
	return out, inherited
}

// ownership is what one settlement reads of the models its parts' declarations
// come from: how far each one's composition reaches (reaching), through alias
// scalars, Base and Mixins, read once per model however many names and
// candidates ask of it, and only as far as a question needs. Each type it
// reads is charged to reads, the run's, so no fixed depth decides an answer: a
// walk the reads cut short leaves a candidate in, and the settlement reports
// the cut.
type ownership struct {
	r     registry
	reads *budget.Budget
	walks map[ir.TypeID]*ownerWalk
}

// newOwnership returns the ownership one settlement reads through
// registryValue, charging reads, which nil leaves without bound, as a Merger
// naming none does.
func newOwnership(registryValue registry, reads *budget.Budget) ownership {
	if reads == nil {
		reads = new(budget.Budget)
	}
	return ownership{r: registryValue, reads: reads, walks: map[ir.TypeID]*ownerWalk{}}
}

// ownerWalk is how far one model's composition has been read breadth first:
// the types it reaches, those queued once, and those whose parts are yet to
// read.
type ownerWalk struct {
	reached, queued map[ir.TypeID]bool
	queue           []ir.TypeID
}

// reaching returns the types owner's composition reaches, read until it
// reaches each of owners but owner, ends, or the reads run out. A hierarchy
// composing a shared base at every level answers at its first level; one
// composing at every level a model redeclaring a name it holds reads its whole
// depth at each, its depth squared, until the reads run out (GitHub #1993).
func (o ownership) reaching(owner ir.TypeID, owners map[ir.TypeID]bool) map[ir.TypeID]bool {
	ownerWalkValue, read := o.walks[owner]
	if !read {
		ownerWalkValue = &ownerWalk{reached: map[ir.TypeID]bool{}, queued: map[ir.TypeID]bool{owner: true}, queue: []ir.TypeID{owner}}
		o.walks[owner] = ownerWalkValue
	}
	missing := len(owners) - 1 - len(reachedOthers(ownerWalkValue.reached, owners, owner))
	for len(ownerWalkValue.queue) > 0 && missing > 0 {
		td, ok := o.r.resolve(ownerWalkValue.queue[0])
		ownerWalkValue.queue = ownerWalkValue.queue[1:]
		if !ok {
			continue
		}
		parts := ComposedOf(td)
		if !o.reads.Spend(1 + len(parts)) {
			ownerWalkValue.queue = nil
			break
		}
		for _, part := range parts {
			if !ownerWalkValue.reached[part] && part != owner && owners[part] {
				missing--
			}
			ownerWalkValue.reached[part] = true
			if !ownerWalkValue.queued[part] {
				ownerWalkValue.queued[part] = true
				ownerWalkValue.queue = append(ownerWalkValue.queue, part)
			}
		}
	}
	return ownerWalkValue.reached
}

// reachedOthers returns the members of owners but owner that reached holds,
// reading the smaller of the two.
func reachedOthers(reached, owners map[ir.TypeID]bool, owner ir.TypeID) []ir.TypeID {
	small, large := reached, owners
	if len(owners) < len(reached) {
		small, large = owners, reached
	}
	var out []ir.TypeID
	for id := range small {
		if id != owner && large[id] {
			out = append(out, id)
		}
	}
	return out
}

// left returns cs, one name's candidates in part order, but those it reads
// elsewhere: one an earlier part brings, or one whose model another
// candidate's composes without being composed by it in turn, as two models on
// a cycle each compose the other. Each model's composition is read once, in
// the order of their IDs, so the candidates cost their number, not its square,
// and the reads run out at the same model in every run.
func (o ownership) left(cs []candidate) []candidate {
	type key struct {
		owner ir.TypeID
		id    ir.PropertyID
	}
	owners, brought := map[ir.TypeID]bool{}, map[key]bool{}
	for _, cd := range cs {
		owners[cd.decl.Owner] = true
	}
	reach, composers := map[ir.TypeID]map[ir.TypeID]bool{}, map[ir.TypeID][]ir.TypeID{}
	if len(owners) > 1 {
		for _, owner := range slices.Sorted(maps.Keys(owners)) {
			reach[owner] = o.reaching(owner, owners)
			for _, part := range reachedOthers(reach[owner], owners, owner) {
				composers[part] = append(composers[part], owner)
			}
		}
	}
	var out []candidate
	for _, cd := range cs {
		keyValue := key{owner: cd.decl.Owner, id: cd.decl.Property.ID}
		if brought[keyValue] {
			continue
		}
		brought[keyValue] = true
		within := func(by ir.TypeID) bool { return !reach[cd.decl.Owner][by] }
		if !slices.ContainsFunc(composers[cd.decl.Owner], within) {
			out = append(out, cd)
		}
	}
	return out
}

// parts returns the types the model composes (ComposedOf), each with the
// entry naming it: the first the walk met, or one at the model, read first,
// where none does. A type written twice brings its declarations once
// (ownership.left).
func (idx *ModelPropertyDeclarations) parts() []composedEntry {
	var out []composedEntry
	for _, target := range ComposedOf(idx.m) {
		at := slices.IndexFunc(idx.entries, func(composedEntryValue composedEntry) bool { return composedEntryValue.target == target })
		if at < 0 {
			out = append(out, composedEntry{target: target, at: idx.m.Provenance.Pointer})
			continue
		}
		out = append(out, idx.entries[at])
	}
	return out
}

// TakenOutside returns the declaration of member a flattening takes but those of
// from: a composition on a cycle reaches from in turn, whose declarations from
// holds already, so the one taken before them stands in. It is false where
// every declaration is from's.
func TakenOutside(member ir.FlatProperty, from ir.TypeID) (ir.PropertyDecl, bool) {
	if member.Owner != from {
		return member.PropertyDecl, true
	}
	for _, propertyDecl := range slices.Backward(member.Overridden) {
		if propertyDecl.Owner != from {
			return propertyDecl, true
		}
	}
	return ir.PropertyDecl{}, false
}

// broughtDeclaration is from, which another model declares, as an entry, via,
// brings it, its type judged where it may be written (Declared.TypeAt). The
// registry does not say which heuristic set its Secret, so where it is secret
// its marker is read as covering that too (declaration.secretBy), as
// foldAnnotations reads one covering a deprecation.
func broughtDeclaration(from Declared, via *composedEntry) declaration {
	declarationValue := declaration{property: *from.Property, order: via.order, via: via, typeAt: from.TypeAt, owner: from.Owner}
	if declarationValue.property.Secret {
		declarationValue.secretBy = declarationValue.property.Provenance.Inferred
	}
	return declarationValue
}

// inOrder returns own and brought, the declarations of one member, in the
// order the walk met them.
func inOrder(own, brought []declaration) []declaration {
	out := slices.Concat(own, brought)
	slices.SortStableFunc(out, func(a, b declaration) int { return cmp.Compare(a.order, b.order) })
	return out
}
