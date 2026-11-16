// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"cmp"
	"encoding/json/jsontext"
	"maps"
	"slices"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/ir"
)

// Table is what a model's composition says of the wire names more than one
// model declares: for each, the declaration its flattening takes
// (ir.FlattenedProperties), which a model settled before holds folded from
// every other its composition gives the name. A name one model alone declares
// never decides a member twice, so a settlement reads none.
type Table map[string]ir.PropertyDecl

// Settle folds each member of the model more than one declaration decides,
// its own and those its entries bring
// (docs/compilers/openapi.md#allof-redeclared-property), which the flattening
// would take one of by where it reads it, judging none. Each fold reads them in
// the order the walk met them, an entry's where it met the entry, so a $ref
// and the schema it names written inline fold alike. tables holds the Table
// of each type the model composes (ComposedOf), so the caller settles those
// first, and shared the names more than one model declares. It returns the
// model's Table.
func (idx *PropIndex) Settle(g Merger, tables map[ir.TypeID]Table, shared map[string]bool) ([]ir.Diagnostic, Table) {
	brought, inherited := idx.brought(g, tables)
	diags := make([]ir.Diagnostic, 0, len(idx.byWire))
	for _, f := range slices.SortedFunc(maps.Values(idx.byWire), func(a, b *field) int { return cmp.Compare(a.pos, b.pos) }) {
		decls := slices.Clone(f.decls)
		decls[0].prop = idx.m.Properties[f.pos]
		wire := decls[0].prop.WireName
		decls = inOrder(decls, brought[wire])
		delete(brought, wire)
		diags = append(diags, idx.settle(g, f.pos, decls)...)
	}
	for _, wire := range slices.Sorted(maps.Keys(brought)) {
		diags = append(diags, idx.settle(g, -1, brought[wire])...)
	}
	return diags, idx.members(shared, inherited)
}

// members returns the model's Table once settled: each name its parts leave
// one declaration of, and each shared one it declares itself, which stands
// over theirs.
func (idx *PropIndex) members(shared map[string]bool, inherited Table) Table {
	out := make(Table, len(inherited))
	maps.Copy(out, inherited)
	for i := range idx.m.Properties {
		if p := &idx.m.Properties[i]; shared[p.WireName] {
			out[p.WireName] = ir.PropertyDecl{Owner: idx.m.ID, Property: p}
		}
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
func (idx *PropIndex) settle(g Merger, pos int, decls []declaration) []ir.Diagnostic {
	if len(decls) < 2 {
		return nil
	}
	if !slices.ContainsFunc(decls, isBrought) {
		merged, diags := g.fold(decls)
		idx.place(pos, merged)
		return diags
	}
	own := slices.DeleteFunc(slices.Clone(decls), isBrought)
	field := composedMember(g.IDs, idx.m, decls[0].prop.WireName)
	kept := len(own) > 0 && idx.heldAlone(g, own[0])
	if kept {
		_, field = fieldSpan(own[:min(len(own), maxFoldDeclarations)])
	}
	var assembled []jsontext.Pointer
	if !kept {
		assembled = assembledFrom(decls)
	}
	merged, diags := g.foldAt(decls, field, assembled)
	inferred := merged.Provenance.Inferred
	if kept {
		merged.ID, merged.Provenance = own[0].prop.ID, own[0].prop.Provenance
	} else {
		merged.ID, merged.Provenance = g.IDs.ComposedProp(field), g.Doc.At(field, assembled...)
	}
	merged.Provenance.Inferred = inferred
	if i := slices.Index(idx.m.Requires, merged.WireName); i >= 0 {
		idx.m.Requires = slices.Delete(idx.m.Requires, i, i+1)
		merged.Required = true
	}
	idx.place(pos, merged)
	return naming(diags, merged.ID)
}

// composedMember is where m declares a member named wire that takes an
// identity in the composed namespace, which no schema occupies:
// <model>/allOf/properties/<name>, and one allOf deeper for a model the merge
// composed, a field's composition or a distributed variant, since the schema
// written at its pointer may settle a member of that name too. No schema is
// written at an allOf, so neither path is a declaration's.
func composedMember(d ids.Document, m *ir.Model, wire string) jsontext.Pointer {
	at := m.Provenance.Pointer + ids.Ptr("allOf")
	if d.ComposedType(m.Provenance.Pointer) == m.ID {
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
	for i, d := range decls {
		out[i] = d.reportAt()
	}
	return out
}

// isBrought reports whether d is a declaration an entry brings, which another
// model holds.
func isBrought(d declaration) bool { return d.via != nil }

// heldAlone reports whether no model but this one holds d, its own
// declaration, so the member it decides may keep d's identity: d is written
// inside the model's schema, as the body a distributed variant shares with its
// siblings is not, and no schema between the two stands as a node of its own
// (Merger.carriedApart).
func (idx *PropIndex) heldAlone(g Merger, d declaration) bool {
	model, at := idx.m.Provenance.Pointer, d.prop.Provenance.Pointer
	return model.Contains(at) && !g.carriedApart(at, model)
}

// place writes p as the model's property at pos, or appends it where pos is
// -1. A discriminator naming the property it replaces names p, and so does one
// naming by its name a member the model did not declare when the walk read
// it, which the model now declares (docs/compilers/openapi.md#discriminator).
func (idx *PropIndex) place(pos int, p ir.Property) {
	d := idx.m.Discriminator
	if pos < 0 {
		idx.m.Properties = append(idx.m.Properties, p)
		if d != nil && d.Property == "" && d.PropertyName != "" && d.PropertyName == p.Name.Source {
			d.Property, d.PropertyName = p.ID, ""
		}
		return
	}
	if d != nil && d.Property == idx.m.Properties[pos].ID {
		d.Property = p.ID
	}
	idx.m.Properties[pos] = p
}

// naming returns diags, each naming id, the property the fold decided, as the
// node it locates (ir.Provenance.Node): a report about an entry's declaration
// sits where the model writes the entry, which is not where the property is.
func naming(diags []ir.Diagnostic, id ir.PropID) []ir.Diagnostic {
	for i := range diags {
		diags[i].Provenance.Node = string(id)
	}
	return diags
}

// candidate is a declaration one of the model's parts brings, and that part.
type candidate struct {
	decl ir.PropertyDecl
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
func (idx *PropIndex) brought(g Merger, tables map[ir.TypeID]Table) (map[string][]declaration, Table) {
	parts := idx.parts()
	byWire := map[string][]candidate{}
	for i, part := range parts {
		for wire, d := range tables[part.target] {
			if d.Owner != idx.m.ID {
				byWire[wire] = append(byWire[wire], candidate{decl: d, part: i})
			}
		}
	}
	owners := newOwnership(registry{g.resolve}, g.Reads)
	out, inherited := map[string][]declaration{}, Table{}
	for _, wire := range slices.Sorted(maps.Keys(byWire)) {
		left := owners.left(byWire[wire])
		for _, cd := range left {
			out[wire] = append(out[wire], broughtDeclaration(*cd.decl.Property, &parts[cd.part]))
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

// newOwnership returns the ownership one settlement reads through r, charging
// reads, which nil leaves without bound, as a Merger naming none does.
func newOwnership(r registry, reads *budget.Budget) ownership {
	if reads == nil {
		reads = new(budget.Budget)
	}
	return ownership{r: r, reads: reads, walks: map[ir.TypeID]*ownerWalk{}}
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
	w, read := o.walks[owner]
	if !read {
		w = &ownerWalk{reached: map[ir.TypeID]bool{}, queued: map[ir.TypeID]bool{owner: true}, queue: []ir.TypeID{owner}}
		o.walks[owner] = w
	}
	missing := len(owners) - 1 - len(reachedOthers(w.reached, owners, owner))
	for len(w.queue) > 0 && missing > 0 {
		td, ok := o.r.resolve(w.queue[0])
		w.queue = w.queue[1:]
		if !ok {
			continue
		}
		parts := ComposedOf(td)
		if !o.reads.Spend(1 + len(parts)) {
			w.queue = nil
			break
		}
		for _, part := range parts {
			if !w.reached[part] && part != owner && owners[part] {
				missing--
			}
			w.reached[part] = true
			if !w.queued[part] {
				w.queued[part] = true
				w.queue = append(w.queue, part)
			}
		}
	}
	return w.reached
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
		id    ir.PropID
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
		k := key{owner: cd.decl.Owner, id: cd.decl.Property.ID}
		if brought[k] {
			continue
		}
		brought[k] = true
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
func (idx *PropIndex) parts() []composedEntry {
	var out []composedEntry
	for _, target := range ComposedOf(idx.m) {
		at := slices.IndexFunc(idx.entries, func(e composedEntry) bool { return e.target == target })
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
	for _, d := range slices.Backward(member.Overridden) {
		if d.Owner != from {
			return d, true
		}
	}
	return ir.PropertyDecl{}, false
}

// broughtDeclaration is p, which another model declares, as an entry, via,
// brings it. The registry does not say which heuristic set its Secret, so
// where it is secret its marker is read as covering that too
// (declaration.secretBy), as foldAnnotations reads one covering a deprecation.
func broughtDeclaration(p ir.Property, via *composedEntry) declaration {
	d := declaration{prop: p, order: via.order, via: via}
	if p.Secret {
		d.secretBy = p.Provenance.Inferred
	}
	return d
}

// inOrder returns own and brought, the declarations of one member, in the
// order the walk met them.
func inOrder(own, brought []declaration) []declaration {
	out := slices.Concat(own, brought)
	slices.SortStableFunc(out, func(a, b declaration) int { return cmp.Compare(a.order, b.order) })
	return out
}
