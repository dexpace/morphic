// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"encoding/json/jsontext"
	"fmt"
	"reflect"
	"slices"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// builder builds the node the members at one position meet in
// (docs/compilers/openapi.md#allof-redeclared-property): the winner's node
// holding what each member meeting it adds, its members built the same way a
// level down. A dry run mints and reports nothing, so a fold learns whether
// every position builds before it registers a node; failed says one did not.
type builder struct {
	f      *folding
	dry    bool
	failed bool
}

// mint registers node at pointer, or in a dry run names nothing.
func (b *builder) mint(pointer jsontext.Pointer, node ir.TypeDef) ir.TypeID {
	if b.dry {
		return ""
	}
	return b.f.g.Mint(pointer, node)
}

// meetNode returns the node base and partners, members that meet it, stand on:
// base's own where no partner adds anything, and otherwise one minted at
// pointer. A literal among them is the node, since it is within each; a
// literal meets only one node, so it is built beside one partner alone.
func (b *builder) meetNode(base member, partners []member, pointer jsontext.Pointer, depth int) ir.TypeID {
	if len(partners) == 0 {
		return base.ref.Target
	}
	all := append([]member{base}, partners...)
	nodes := make([]ir.TypeDef, len(all))
	for k, m := range all {
		td, ok := b.f.g.Resolve(m.ref.Target)
		if !ok {
			b.failed = true
			return base.ref.Target
		}
		nodes[k] = td
	}
	if k := slices.IndexFunc(nodes, isLiteral); k > 0 {
		all[0], all[k], nodes[0], nodes[k] = all[k], all[0], nodes[k], nodes[0]
	}
	built, ok := b.build(all, nodes, pointer, depth)
	if !ok {
		b.failed = true
		return base.ref.Target
	}
	built = b.adoptInto(built, all, nodes)
	if reflect.DeepEqual(built, nodes[0]) {
		return all[0].ref.Target
	}
	return b.mint(pointer, built)
}

// build returns nodes[0] holding what each of nodes[1:] adds: its members built
// from all the nodes' (settle), its bounds met with theirs (meetConstraints),
// and what else they say adopted (adopt), or false where the nodes are of
// kinds no node holds together.
func (b *builder) build(all []member, nodes []ir.TypeDef, pointer jsontext.Pointer, depth int) (ir.TypeDef, bool) {
	switch n := nodes[0].(type) {
	case *ir.Literal:
		out := *n
		return &out, len(nodes) == 2
	case *ir.Scalar:
		out := *n
		return &out, sameKind[*ir.Scalar](nodes)
	case *ir.Enum:
		out := *n
		out.Members = sharedBy(n.Members, nodes[1:])
		return &out, sameKind[*ir.Enum](nodes) && len(out.Members) > 0
	case *ir.List:
		out := *n
		out.Elem = b.settle(membersOf(all, nodes, func(l *ir.List) ir.TypeRef { return l.Elem }), pointer+"/items", depth)
		return &out, sameKind[*ir.List](nodes)
	case *ir.MapT:
		out := *n
		out.Value = b.settle(membersOf(all, nodes, func(m *ir.MapT) ir.TypeRef { return m.Value }), pointer+"/additionalProperties", depth)
		return &out, sameKind[*ir.MapT](nodes)
	case *ir.Tuple:
		return b.tuple(n, all, nodes, pointer, depth)
	default:
		return nil, false
	}
}

// tuple is build for a tuple: each position, and what may follow the last,
// built from all the tuples' (settle).
func (b *builder) tuple(n *ir.Tuple, all []member, nodes []ir.TypeDef, pointer jsontext.Pointer, depth int) (ir.TypeDef, bool) {
	if !sameKind[*ir.Tuple](nodes) {
		return nil, false
	}
	out := *n
	out.Elems = make([]ir.TypeRef, len(n.Elems))
	for i := range n.Elems {
		at := pointer + jsontext.Pointer(fmt.Sprintf("/prefixItems/%d", i))
		out.Elems[i] = b.settle(membersOf(all, nodes, func(t *ir.Tuple) ir.TypeRef { return t.Elems[i] }), at, depth)
	}
	if n.Rest != nil {
		rest := b.settle(membersOf(all, nodes, func(t *ir.Tuple) ir.TypeRef { return *t.Rest }), pointer+"/items", depth)
		out.Rest = &rest
	}
	return &out, true
}

// sameKind reports whether every node is a T.
func sameKind[T ir.TypeDef](nodes []ir.TypeDef) bool {
	return !slices.ContainsFunc(nodes, func(td ir.TypeDef) bool { _, ok := td.(T); return !ok })
}

// membersOf returns what each node, a T, holds at one position (of), each the
// member of the declaration whose node it is.
func membersOf[T ir.TypeDef](all []member, nodes []ir.TypeDef, of func(T) ir.TypeRef) []member {
	out := make([]member, 0, len(nodes))
	for k, td := range nodes {
		if n, ok := td.(T); ok {
			out = append(out, member{ref: of(n), decl: all[k].decl})
		}
	}
	return out
}

// sharedBy returns the members of ms whose value every enum of others holds,
// in ms's order: the values of all the enums at once.
func sharedBy(ms []ir.EnumMember, others []ir.TypeDef) []ir.EnumMember {
	out := make([]ir.EnumMember, 0, len(ms))
	for _, m := range ms {
		if !slices.ContainsFunc(others, func(td ir.TypeDef) bool {
			e, ok := td.(*ir.Enum)
			if !ok {
				return true
			}
			_, held := memberOf(e.Members, m.Value)
			return !held
		}) {
			out = append(out, m)
		}
	}
	if len(out) == len(ms) {
		return ms
	}
	return out
}

// settle returns the type of one position of the nodes built at depth, its
// members ms: the model they compose, or the node they meet in built a level
// down, at pointer, admitting null as position.null reads them. A member
// outside the winner's cone fails the build, since no node holds it beside the
// rest.
func (b *builder) settle(ms []member, pointer jsontext.Pointer, depth int) ir.TypeRef {
	p := b.f.g.judgePosition(b.f.decls, ms, depth+1)
	all := func(int) bool { return true }
	var target ir.TypeID
	if len(p.conjuncts) > 0 {
		target = b.compose(p.conjuncts, pointer)
	} else {
		for k := range ms {
			if !p.holds(p.winner, k) {
				b.failed = true
				return ms[p.winner].ref
			}
		}
		base, partners := p.meeting(all)
		target = b.meetNode(base, partners, pointer, depth+1)
	}
	ref := p.null(registry{b.f.g.Resolve}, all, depth+1)
	ref.Target = target
	return ref
}

// adoptInto returns built, the node nodes[0] stands for with its members built,
// holding the bounds every node says together and what else each of nodes[1:]
// adds: all a reference to each would read. A keyword or a field two give
// differently is the first's, and the later one is reported where its
// declaration is written and kept verbatim, as foldDocs keeps a property's. A
// literal takes no bounds, since every node meeting it admits its value.
func (b *builder) adoptInto(built ir.TypeDef, all []member, nodes []ir.TypeDef) ir.TypeDef {
	sets := make([]*ir.Constraints, len(b.f.decls))
	for k, td := range nodes {
		sets[all[k].decl] = boundsOf(td)
	}
	if !isLiteral(built) {
		if met := meetConstraints(sets, b.disagree); !sameBounds(met, boundsOf(nodes[0])) {
			built = withBounds(built, met)
		}
	}
	others := make([]ir.TypeCommon, len(nodes)-1)
	for k, td := range nodes[1:] {
		others[k] = *td.Common()
	}
	c := built.Common()
	*c = adopt(*c, others, b.f.g.Alike, func(k int, field string) { b.differ(all[k+1].decl, field) })
	if l, ok := built.(*ir.List); ok && l.Encoding == nil {
		b.adoptEncoding(l, all, nodes)
	}
	return built
}

// adoptEncoding sets on l, a list giving no encoding, the first one nodes[1:]
// give, reporting each later list giving another as adopt reports a field.
func (b *builder) adoptEncoding(l *ir.List, all []member, nodes []ir.TypeDef) {
	for k, td := range nodes[1:] {
		other, ok := td.(*ir.List)
		switch {
		case !ok || other.Encoding == nil:
		case l.Encoding == nil:
			l.Encoding = other.Encoding
		case !reflect.DeepEqual(l.Encoding, other.Encoding):
			b.differ(all[k+1].decl, "encoding")
		}
	}
}

// disagree is folding.disagree, heard only where the build is not a dry run.
func (b *builder) disagree(i, kept int, detail string) {
	if !b.dry {
		b.f.disagree(i, kept, detail)
	}
}

// differ reports at declaration decl that it gives what field names of a type
// the field holds otherwise than one before it, keeping it verbatim.
func (b *builder) differ(decl int, field string) {
	if b.dry {
		return
	}
	b.f.lost[decl] = true
	b.f.note(decl, ir.SeverityInfo, diag.DegradedConstruct,
		"declarations of field %q give the %s of a type it holds differently; kept the first given over the "+
			"one at %s, which is kept verbatim under Unmodeled", b.f.merged.WireName, field, b.f.at(decl))
}
