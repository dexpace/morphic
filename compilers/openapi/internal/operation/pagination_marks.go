// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"slices"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// position is one place a marking source's key says a pagination value sits:
// the members walked to it from a body, none for the body whole, or a header,
// the node whose key marks it, and whether its model is reached by more than
// one walk as short.
type position struct {
	segments []ir.PropID
	keeper   *ir.Provenance
	shared   bool
	header   bool
}

// marker is one marking source of a pagination target, asked of the nodes of
// one operation. entry is the Unmodeled key its source's key is kept under.
type marker struct {
	pg     *pager
	target lowering.ExtensionTarget
	src    lowering.ExtensionSource
	entry  string
	diags  []ir.Diagnostic
}

// newMarker is the marker of src, a source of target, asked against pg.
func newMarker(pg *pager, target lowering.ExtensionTarget, src lowering.ExtensionSource) *marker {
	return &marker{pg: pg, target: target, src: src, entry: unmodeledKey(src.Key)}
}

// unmodeledKey is the Unmodeled key an extension named key is kept under.
func unmodeledKey(key string) string {
	return "openapi:" + key
}

// marks returns the provenance of the node keeping unmodeled when src marks
// it. A node keeping no entry under the key, nearly every one a search meets,
// is answered before the source's reading, which would say nothing of it.
func (m *marker) marks(unmodeled ir.Unmodeled, keeper *ir.Provenance) (*ir.Provenance, bool) {
	if _, kept := unmodeled[m.entry]; !kept {
		return keeper, false
	}
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
	m := newMarker(pg, target, src)
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
// of the success responses, or one of their headers, which must all mark the
// same one. A member is held by every response declaring a body, and a header
// by every response, as an expression's value is.
func (pg *pager) markedOutput(op *ir.Operation, target lowering.ExtensionTarget, src lowering.ExtensionSource) (resolved[*ir.PropPath], string, []ir.Diagnostic) {
	m := newMarker(pg, target, src)
	r := resolved[*ir.PropPath]{at: op.Provenance}
	var lacking, bodiedLacking bool // a response marks nothing, one declaring a body among them
	for i := range op.Responses {
		found, failure := m.inResponse(&op.Responses[i])
		switch {
		case failure != "":
			r.stated = true
			return r, failure, m.diags
		case found == nil:
			lacking = true
			bodiedLacking = bodiedLacking || op.Responses[i].Payload != nil
		case r.value == nil:
			r.value, r.read, r.stated = found.path(), []*ir.Provenance{found.keeper}, true
		case !sameOutput(r.value, found.path()):
			return r, "marks different members of the success responses", m.diags
		default:
			r.read = append(r.read, found.keeper)
		}
	}
	if r.value != nil && (bodiedLacking || lacking && r.value.In == "header") {
		return r, "marks a value one of the success responses does not hold", m.diags
	}
	return r, "", m.diags
}

// inResponse returns where m's source marks a value of r: a member of its body
// or one of its headers, and why it marks none where it marks more than one.
func (m *marker) inResponse(r *ir.Response) (*position, string) {
	found, failure := m.within(r.Payload)
	if failure != "" {
		return nil, failure
	}
	for i := range r.Headers {
		h := &r.Headers[i].Property
		keeper, ok := m.marksUse(h.Unmodeled, &h.Provenance, h.Type.Target)
		switch {
		case !ok:
		case found != nil:
			return nil, "marks more than one value of a response"
		default:
			found = &position{segments: []ir.PropID{h.ID}, keeper: keeper, header: true}
		}
	}
	return found, ""
}

// path is the page value p names.
func (p *position) path() *ir.PropPath {
	if p.header {
		return &ir.PropPath{In: "header", Segments: p.segments}
	}
	return &ir.PropPath{Segments: p.segments}
}

// within returns where m's source marks a value of body, the one position
// every content holds, and why it marks none where the contents mark different
// members, one marks more than one, or one marks none beside another marking
// one, since a pager reads whichever content arrives.
func (m *marker) within(body *ir.Payload) (*position, string) {
	if body == nil {
		return nil, ""
	}
	var out *position
	lacking := false // a content marks nothing
	for i := range body.Contents {
		found := m.search(body.Contents[i].Type.Target)
		switch {
		case len(found) == 0:
			lacking = true
		case len(found) > 1 || found[0].shared:
			return nil, "marks more than one member of a body, as near its root"
		case out == nil:
			out = &found[0]
		case !slices.Equal(out.segments, found[0].segments):
			return nil, "marks different members of a body's media types"
		}
	}
	if out != nil && lacking {
		return nil, "marks a member one of a body's media types does not hold"
	}
	return out, ""
}

// search returns the positions nearest root that m's source marks, root itself
// first: those at the least depth, each model walked once, at its least depth,
// so it ends within the registry's size. A model reached by two walks at one
// depth marks each member it holds twice, which shared records. It descends
// only into a model whose members reach the key (pager.reaching), so an
// operation pays for the walk only where the document writes the key below
// its body.
func (m *marker) search(root ir.TypeID) []position {
	if td := m.pg.lk.reg[root]; !ir.IsNilTypeDef(td) {
		if keeper, ok := m.marks(td.Common().Unmodeled, &td.Common().Provenance); ok {
			return []position{{keeper: keeper}}
		}
	}
	reach := m.pg.reaching(m.src.Key)
	if !reach[root] {
		return nil
	}
	seen := map[ir.TypeID]bool{root: true}
	level := []step{{model: root}}
	for len(level) > 0 {
		found, next := m.descend(level, seen, reach)
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
	trail  *trail
	shared bool
}

// trail is the members a search walked from its root to a model, the last
// first, so each model's walk extends its parent's rather than copying it, and
// a chain of models costs its length, not its square.
type trail struct {
	member ir.PropID
	up     *trail
	length int
}

// then is t extended by member.
func (t *trail) then(member ir.PropID) *trail {
	return &trail{member: member, up: t, length: t.len() + 1}
}

// len is how many members t holds, none for nil, the root's.
func (t *trail) len() int {
	if t == nil {
		return 0
	}
	return t.length
}

// segments is t's members from the root, then last.
func (t *trail) segments(last ir.PropID) []ir.PropID {
	out := make([]ir.PropID, t.len()+1)
	out[len(out)-1] = last
	for i, at := len(out)-2, t; at != nil; i, at = i-1, at.up {
		out[i] = at.member
	}
	return out
}

// descend reads the members of each model of level, one depth of a search:
// the positions m's source marks among them, and the models of reach they
// reach that no shallower depth did, each once, which it adds to seen.
func (m *marker) descend(level []step, seen, reach map[ir.TypeID]bool) ([]position, []step) {
	var found []position
	var next []step
	at := map[ir.TypeID]int{}
	for _, s := range level {
		for _, p := range m.pg.members(s.model) {
			if keeper, ok := m.marksUse(p.Unmodeled, &p.Provenance, p.Type.Target); ok {
				found = append(found, position{segments: s.trail.segments(p.ID), keeper: keeper, shared: s.shared})
			}
			t := p.Type.Target
			if i, ok := at[t]; ok {
				next[i].shared = true
			} else if !seen[t] && reach[t] {
				at[t] = len(next)
				next = append(next, step{model: t, trail: s.trail.then(p.ID), shared: s.shared})
			}
		}
	}
	for t := range at {
		seen[t] = true
	}
	return found, next
}

// reaching returns the models whose members reach a node keeping key, a
// member or the type a member names, through members alone: the models a
// search under key can find a mark below, read once per key. A document whose
// models' members keep the key nowhere, as most write none, flattens no model.
func (pg *pager) reaching(key string) map[ir.TypeID]bool {
	if set, read := pg.reach[key]; read {
		return set
	}
	entry := unmodeledKey(key)
	keeps := func(p *ir.Property) bool {
		if _, ok := p.Unmodeled[entry]; ok {
			return true
		}
		td := pg.lk.reg[p.Type.Target]
		if ir.IsNilTypeDef(td) {
			return false
		}
		_, ok := td.Common().Unmodeled[entry]
		return ok
	}
	set := map[ir.TypeID]bool{}
	if pg.reach == nil {
		pg.reach = map[string]map[ir.TypeID]bool{}
	}
	pg.reach[key] = set
	if !pg.anyMember(keeps) {
		return set
	}
	// Every model's members are read once to find who keeps the key and which
	// model names which, and each keeper's namers are added after it. The set
	// is the answer, so the registry's order is not.
	namers := map[ir.TypeID][]ir.TypeID{}
	var queue []ir.TypeID
	for id := range pg.lk.reg {
		for _, p := range pg.members(id) {
			if keeps(p) && !set[id] {
				set[id] = true
				queue = append(queue, id)
			}
			if pg.isModel(p.Type.Target) {
				namers[p.Type.Target] = append(namers[p.Type.Target], id)
			}
		}
	}
	for len(queue) > 0 {
		t := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		for _, namer := range namers[t] {
			if !set[namer] {
				set[namer] = true
				queue = append(queue, namer)
			}
		}
	}
	return set
}

// anyMember reports whether a property some model of the registry declares
// satisfies keeps. Every member a flattening reads is one, so this answers for
// every model's members without flattening any.
func (pg *pager) anyMember(keeps func(*ir.Property) bool) bool {
	for _, td := range pg.lk.reg {
		if model, ok := td.(*ir.Model); ok {
			for i := range model.Properties {
				if keeps(&model.Properties[i]) {
					return true
				}
			}
		}
	}
	return false
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
