// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir

// MemberReader reads single members of the flattenings of one registry's
// models, for a reader asking a model for a name or two rather than for its
// whole shape. It runs the walk [FlattenedProperties] runs, so the two take
// the same declaration, but reads each model's own properties through an
// index of them by wire name, made at the model's first read and kept. A read
// costs the root's composition, and each model's properties once per reader.
// Like a [Flattening], it describes the registry as it stood when it read it.
type MemberReader struct {
	types TypeRegistry
	// own places, for each model read, each wire name among its own
	// properties: the index of each declaring it, in source order.
	own map[TypeID]map[string][]int
	// steps counts what every read has read: the steps of each walk, as a
	// flattening counts them, and each property an index was made of.
	steps int
}

// NewMemberReader returns a MemberReader over types that has read nothing.
func NewMemberReader(types TypeRegistry) *MemberReader {
	return &MemberReader{types: types, own: map[TypeID]map[string][]int{}}
}

// Member returns the declaration FlattenedProperties(types, root) takes for
// wire, and false where that flattening holds no such member: where root
// names no model, read through alias scalars, or none it composes declares
// the name.
func (r *MemberReader) Member(root TypeID, wire string) (PropertyDecl, bool) {
	q := memberQuery{reader: r, wire: wire}
	f := flattener{types: r.types, query: &q}
	f.read(root)
	r.steps += f.steps
	if len(q.reads) == 0 {
		return PropertyDecl{}, false
	}
	return taken(q.reads), true
}

// memberQuery is the one wire name a MemberReader's walk reads, and the
// declarations of it the walk has read, in its order.
type memberQuery struct {
	reader *MemberReader
	wire   string
	reads  []PropertyDecl
}

// emit reads the model id names for the query's wire name, through the
// model's own index: each of its own declarations of the name, in source
// order, as a flattening's walk reads them.
func (q *memberQuery) emit(id TypeID, m *Model) {
	for _, i := range q.reader.ownIndex(id, m)[q.wire] {
		q.reads = append(q.reads, PropertyDecl{Owner: id, Property: &m.Properties[i]})
	}
}

// ownIndex returns the index of the model m, which id names, by the wire
// names of its own properties, made at its first read.
func (r *MemberReader) ownIndex(id TypeID, m *Model) map[string][]int {
	if own, read := r.own[id]; read {
		return own
	}
	own := make(map[string][]int, len(m.Properties))
	for i := range m.Properties {
		r.steps++
		wire := m.Properties[i].EffectiveWireName()
		own[wire] = append(own[wire], i)
	}
	r.own[id] = own
	return own
}
