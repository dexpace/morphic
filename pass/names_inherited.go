// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass

import (
	"slices"
	"strconv"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irfacts"
)

// inherited reads the members a model exposes, its own properties and those it
// inherits, for two spellings of one neutral name that first meet there: a
// derived model's property beside its base's, or two mixins' properties. Every
// model's own properties are read as one list first, and only the models where
// two spellings can first meet are flattened, so a document with no such set
// costs the list, and a hierarchy is not flattened once a model deep. A model
// below the one that met the pair is not told again.
func (r *nameReader) inherited(doc *ir.Document) {
	ids := liveTypeIDs(doc)
	r.owners, r.wires = r.owners[:0], r.wires[:0]
	r.parties = r.parties[:0]
	for k, id := range ids {
		m, ok := doc.Types[id].(*ir.Model)
		if !ok {
			continue
		}
		for i := range m.Properties {
			r.parties = append(r.parties, partyOf(m.Properties[i].Name))
			r.owners = append(r.owners, int32(k))
			r.wires = append(r.wires, m.Properties[i].WireName)
		}
	}
	if !r.read(byWords) || len(r.shared) == 0 {
		return
	}
	var facts *irfacts.DocumentFacts
	for _, id := range r.meetings(doc, ids) {
		if facts == nil {
			facts = irfacts.New(doc)
		}
		r.exposed(doc, facts, id)
	}
}

// meetings returns, sorted, the models to flatten: each owner of a property of a
// set of one neutral name spelled on the wire two ways that composes anything,
// and each model composing two or more. None where no set is spelled two ways.
func (r *nameReader) meetings(doc *ir.Document, ids []ir.TypeID) []ir.TypeID {
	var models []ir.TypeID
	candidate := false
	for _, first := range r.shared {
		if !r.spelledTwoWays(first) {
			continue
		}
		candidate = true
		for i := first; i >= 0; i = r.later[i] {
			id := ids[r.owners[i]]
			if m, ok := doc.Types[id].(*ir.Model); ok && entries(m) > 0 {
				models = append(models, id)
			}
		}
	}
	if !candidate {
		return nil
	}
	for _, id := range ids {
		if m, ok := doc.Types[id].(*ir.Model); ok && entries(m) >= 2 {
			models = append(models, id)
		}
	}
	slices.Sort(models)
	return slices.Compact(models)
}

// spelledTwoWays reports whether the set from first holds two properties of
// different wire names.
func (r *nameReader) spelledTwoWays(first int) bool {
	for i := r.later[first]; i >= 0; i = r.later[i] {
		if r.wires[i] != r.wires[first] {
			return true
		}
	}
	return false
}

// entries counts the composition entries of m: its base, the interfaces it
// implements and its mixins.
func entries(m *ir.Model) int {
	n := len(m.Implements) + len(m.Mixins)
	if m.Base != nil {
		n++
	}
	return n
}

// exposed reads the members the model id exposes as one scope and reports each
// set of one neutral name the model is the first to meet.
func (r *nameReader) exposed(doc *ir.Document, facts *irfacts.DocumentFacts, id ir.TypeID) {
	fl := ir.FlattenedProperties(doc.Types, id)
	members := fl.Properties
	r.parties = r.parties[:0]
	for i := range members {
		r.parties = append(r.parties, partyOf(members[i].Property.Name))
	}
	r.read(byWords)
	r.bare = r.bare[:0]
	r.shared = slices.DeleteFunc(r.shared, func(first int) bool {
		return !r.first(doc, facts, id, members, first)
	})
	if len(r.shared) == 0 {
		return
	}
	r.report(scope{in: "model", id: string(id), one: "exposed property", many: "exposed properties",
		ids: idsOf(len(members), func(i int) string { return memberLabel(doc, id, members[i].PropertyDecl) })})
}

// first reports whether the model id is the first to meet the set of members
// from first: not where its own scope reports it, with every member its own,
// and not where a model it composes met two of them already.
func (r *nameReader) first(doc *ir.Document, facts *irfacts.DocumentFacts, id ir.TypeID, members []ir.FlatProperty, first int) bool {
	owned, total := 0, 0
	for i := first; i >= 0; i = r.later[i] {
		total++
		if members[i].Owner == id {
			owned++
		}
	}
	if owned == total {
		return false
	}
	if owned > 0 {
		return true
	}
	m, ok := doc.Types[id].(*ir.Model)
	return !ok || !slices.ContainsFunc(entryTargets(m), func(target ir.TypeID) bool {
		exposing := 0
		for i := first; i >= 0; i = r.later[i] {
			if facts.Exposes(target, members[i].Property.ID) {
				exposing++
			}
		}
		return exposing >= 2
	})
}

// entryTargets lists the types m's composition entries name.
func entryTargets(m *ir.Model) []ir.TypeID {
	targets := make([]ir.TypeID, 0, entries(m))
	if m.Base != nil {
		targets = append(targets, m.Base.Target)
	}
	for i := range m.Implements {
		targets = append(targets, m.Implements[i].Target)
	}
	for i := range m.Mixins {
		targets = append(targets, m.Mixins[i].Target)
	}
	return targets
}

// memberLabel names a member by its index in its owner's properties, and the
// owner where that is not the model id whose members are read.
func memberLabel(doc *ir.Document, id ir.TypeID, decl ir.PropertyDecl) string {
	label := "?"
	if owner, ok := doc.Types[decl.Owner].(*ir.Model); ok {
		for k := range owner.Properties {
			if &owner.Properties[k] == decl.Property {
				label = strconv.Itoa(k)
				break
			}
		}
	}
	if decl.Owner != id {
		label += " of model " + string(decl.Owner)
	}
	return label
}
