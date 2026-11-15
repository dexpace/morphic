// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"cmp"
	"maps"
	"slices"

	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/ir"
)

// Settle folds each member of the model that more than one declaration
// decides: one it declares twice, and one an entry brings beside its own
// declaration or another entry's (docs/compilers/openapi.md#allof-redeclared-property).
// The flattening would take one of them by where it reads it and judge none.
// Each fold reads the declarations in the order the walk met them, an entry's
// where the walk met the entry, so a $ref and the schema it names written
// inline fold alike. It reads each model the model composes as it stands, so
// the caller settles those first.
func (idx *PropIndex) Settle(g Merger) []ir.Diagnostic {
	brought := idx.brought(registry{g.resolve}, g.Reg)
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
	return diags
}

// settle folds decls, the declarations of one member, where at least two are,
// into the property the model holds at pos, or appends one where pos is -1. A
// member only the model declares keeps its identity (Merger.fold); one an entry
// brings a declaration of is the model's own at <model>/allOf/properties/<name>
// in the composed namespace, which no schema occupies, as a property may not
// follow allOf, and each report names it (naming).
func (idx *PropIndex) settle(g Merger, pos int, decls []declaration) []ir.Diagnostic {
	if len(decls) < 2 {
		return nil
	}
	if !slices.ContainsFunc(decls, func(d declaration) bool { return d.via != nil }) {
		merged, diags := g.fold(decls)
		idx.place(pos, merged)
		return diags
	}
	field := idx.m.Provenance.Pointer + ids.Ptr("allOf", "properties", decls[0].prop.WireName)
	merged, diags := g.foldAt(decls, field)
	inferred := merged.Provenance.Inferred
	merged.ID = g.IDs.ComposedProp(field)
	merged.Provenance = g.Doc.At(field)
	merged.Provenance.Inferred = inferred
	if i := slices.Index(idx.m.Requires, merged.WireName); i >= 0 {
		idx.m.Requires = slices.Delete(idx.m.Requires, i, i+1)
		merged.Required = true
	}
	idx.place(pos, merged)
	return naming(diags, merged.ID)
}

// place writes p as the model's property at pos, or appends it where pos is
// -1. A discriminator naming the property it replaces names p.
func (idx *PropIndex) place(pos int, p ir.Property) {
	if pos < 0 {
		idx.m.Properties = append(idx.m.Properties, p)
		return
	}
	if d := idx.m.Discriminator; d != nil && d.Property == idx.m.Properties[pos].ID {
		d.Property = p.ID
	}
	idx.m.Properties[pos] = p
}

// naming returns diags, each naming id, the property the fold decided, as the
// node it locates (ir.Provenance.Node): an entry's report sits where the model
// writes the entry, not beneath the property, whose identity no entry order
// decides.
func naming(diags []ir.Diagnostic, id ir.PropID) []ir.Diagnostic {
	for i := range diags {
		diags[i].Provenance.Node = string(id)
	}
	return diags
}

// brought returns, by wire name, the declarations the model's entries bring
// that no other's owner composes: one a composing model narrows already stands
// within it. Each is read where the walk met the first entry naming its owner
// or a type composing it; one no entry places is read first, at the model.
func (idx *PropIndex) brought(r registry, reg ir.TypeRegistry) map[string][]declaration {
	out := map[string][]declaration{}
	for _, member := range ir.FlattenedProperties(reg, idx.m.ID).Properties {
		var decls []ir.PropertyDecl
		for _, d := range append(slices.Clone(member.Overridden), member.PropertyDecl) {
			if d.Owner != idx.m.ID {
				decls = append(decls, d)
			}
		}
		for _, d := range r.maximal(decls) {
			via := idx.entryOf(r, d.Owner)
			out[d.Property.WireName] = append(out[d.Property.WireName], broughtDeclaration(*d.Property, via))
		}
	}
	return out
}

// entryOf is the first entry naming owner or a type composing it, or one at the
// model, read first, where none does.
func (idx *PropIndex) entryOf(r registry, owner ir.TypeID) *composedEntry {
	for i := range idx.entries {
		if e := &idx.entries[i]; e.target == owner || r.composes(e.target, owner) {
			return e
		}
	}
	return &composedEntry{target: owner, at: idx.m.Provenance.Pointer}
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

// maximal returns the declarations of decls whose owner no other's owner
// composes, in order: one a composing model narrows already stands within it,
// so it decides nothing the other does not.
func (r registry) maximal(decls []ir.PropertyDecl) []ir.PropertyDecl {
	return slices.DeleteFunc(slices.Clone(decls), func(d ir.PropertyDecl) bool {
		return slices.ContainsFunc(decls, func(e ir.PropertyDecl) bool {
			return e.Owner != d.Owner && r.composes(e.Owner, d.Owner)
		})
	})
}
