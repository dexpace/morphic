// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"slices"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// position is one place a marking source's key says a pagination value sits:
// the members walked to it from a body, none for the body whole, the node
// whose key marks it, and whether its model is reached by more than one walk
// as short.
type position struct {
	segments []ir.PropID
	keeper   *ir.Provenance
	shared   bool
}

// marker is one marking source of a pagination target, asked of the nodes of
// one operation.
type marker struct {
	pg     *pager
	target lowering.ExtensionTarget
	src    lowering.ExtensionSource
	diags  []ir.Diagnostic
}

// marks returns the provenance of the node keeping unmodeled when src marks
// it.
func (m *marker) marks(unmodeled ir.Unmodeled, keeper *ir.Provenance) (*ir.Provenance, bool) {
	st, diags := m.pg.p.PageStatement(m.target, m.src, unmodeled)
	m.diags = append(m.diags, diags...)
	return keeper, st.Stated
}

// marksUse returns the node marking a use of a type, the property or
// parameter holding the use or, where its own keys do not, the type it names.
func (m *marker) marksUse(own ir.Unmodeled, keeper *ir.Provenance, of ir.TypeID) (*ir.Provenance, bool) {
	if at, ok := m.marks(own, keeper); ok {
		return at, true
	}
	td := m.pg.lk.reg[of]
	if ir.IsNilTypeDef(td) {
		return nil, false
	}
	common := td.Common()
	return m.marks(common.Unmodeled, &common.Provenance)
}

// markedInput resolves where src marks a value of op's request: a parameter, or
// a member of its body.
func (pg *pager) markedInput(op *ir.Operation, target lowering.ExtensionTarget, src lowering.ExtensionSource) (resolved[*ir.InputPath], string, []ir.Diagnostic) {
	m := &marker{pg: pg, target: target, src: src}
	var found []*ir.InputPath
	var read []*ir.Provenance
	for i := range op.Params {
		p := &op.Params[i]
		if keeper, ok := m.marksUse(p.Unmodeled, &p.Provenance, p.Type.Target); ok {
			found, read = append(found, &ir.InputPath{Param: &p.ID}), append(read, keeper)
		}
	}
	body, failure := m.within(op.Request)
	if body != nil {
		found, read = append(found, &ir.InputPath{Segments: body.segments}), append(read, body.keeper)
	}
	r := resolved[*ir.InputPath]{read: read, at: op.Provenance, stated: len(found) > 0 || failure != ""}
	switch {
	case failure != "":
		return r, failure, m.diags
	case len(found) > 1:
		return r, "marks more than one value of the request", m.diags
	case len(found) == 1:
		r.value = found[0]
	}
	return r, "", m.diags
}

// markedOutput resolves where src marks a value of a page: a member of the body
// of the success responses, which must all mark the same one.
func (pg *pager) markedOutput(op *ir.Operation, target lowering.ExtensionTarget, src lowering.ExtensionSource) (resolved[*ir.PropPath], string, []ir.Diagnostic) {
	m := &marker{pg: pg, target: target, src: src}
	r := resolved[*ir.PropPath]{at: op.Provenance}
	for i := range op.Responses {
		found, failure := m.within(op.Responses[i].Payload)
		switch {
		case failure != "":
			r.stated = true
			return r, failure, m.diags
		case found == nil:
		case r.value == nil:
			r.value, r.read, r.stated = &ir.PropPath{Segments: found.segments}, []*ir.Provenance{found.keeper}, true
		case !slices.Equal(r.value.Segments, found.segments):
			return r, "marks different members of the success responses' bodies", m.diags
		default:
			r.read = append(r.read, found.keeper)
		}
	}
	return r, "", m.diags
}

// within returns where m's source marks a value of body, the one position each
// content marking one holds, and why it marks none where the contents mark
// different members or one marks more than one.
func (m *marker) within(body *ir.Payload) (*position, string) {
	if body == nil {
		return nil, ""
	}
	var out *position
	for i := range body.Contents {
		found := m.search(body.Contents[i].Type.Target)
		switch {
		case len(found) == 0:
		case len(found) > 1 || found[0].shared:
			return nil, "marks more than one member of a body, as near its root"
		case out == nil:
			out = &found[0]
		case !slices.Equal(out.segments, found[0].segments):
			return nil, "marks different members of a body's media types"
		}
	}
	return out, ""
}

// search returns the positions nearest root that m's source marks, root itself
// first: those at the least depth, each model walked once, at its least depth,
// so it ends within the registry's size. A model reached by two walks at one
// depth marks each member it holds twice, which shared records.
func (m *marker) search(root ir.TypeID) []position {
	if td := m.pg.lk.reg[root]; !ir.IsNilTypeDef(td) {
		if keeper, ok := m.marks(td.Common().Unmodeled, &td.Common().Provenance); ok {
			return []position{{keeper: keeper}}
		}
	}
	seen := map[ir.TypeID]bool{root: true}
	level := []step{{model: root}}
	for len(level) > 0 {
		found, next := m.descend(level, seen)
		if len(found) > 0 {
			return found
		}
		level = next
	}
	return nil
}

// step is one model a search reaches, the members walked to it, and whether
// another walk as short reaches it too.
type step struct {
	model  ir.TypeID
	path   []ir.PropID
	shared bool
}

// descend reads the members of each model of level, one depth of a search:
// the positions m's source marks among them, and the models they reach that no
// shallower depth did, each once, which it adds to seen.
func (m *marker) descend(level []step, seen map[ir.TypeID]bool) ([]position, []step) {
	var found []position
	var next []step
	at := map[ir.TypeID]int{}
	for _, s := range level {
		for _, p := range m.pg.members(s.model) {
			path := append(slices.Clip(s.path), p.ID)
			if keeper, ok := m.marksUse(p.Unmodeled, &p.Provenance, p.Type.Target); ok {
				found = append(found, position{segments: path, keeper: keeper, shared: s.shared})
			}
			t := p.Type.Target
			if i, ok := at[t]; ok {
				next[i].shared = true
			} else if !seen[t] && m.pg.isModel(t) {
				at[t] = len(next)
				next = append(next, step{model: t, path: path, shared: s.shared})
			}
		}
	}
	for t := range at {
		seen[t] = true
	}
	return found, next
}

// members returns the members of the model at id in layout order, read once.
func (pg *pager) members(id ir.TypeID) []*ir.Property {
	if props, read := pg.flat[id]; read {
		return props
	}
	var props []*ir.Property
	if pg.isModel(id) {
		flat := ir.FlattenedProperties(pg.lk.reg, id)
		props = make([]*ir.Property, len(flat.Properties))
		for i := range flat.Properties {
			props[i] = flat.Properties[i].Property
		}
	}
	pg.flat[id] = props
	return props
}

// isModel reports whether the registry holds a model at id.
func (pg *pager) isModel(id ir.TypeID) bool {
	_, ok := pg.lk.reg[id].(*ir.Model)
	return ok
}
