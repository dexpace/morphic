// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"cmp"
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

// mint registers node at pointer, its parts brought where brought says and
// placed as the field is (folding.assembled; FieldDeclarationContext.Mint), or in a dry run names
// nothing.
func (b *builder) mint(pointer jsontext.Pointer, node ir.TypeDef, brought []jsontext.Pointer) ir.TypeID {
	if b.dry {
		return ""
	}
	return b.f.g.Walk.Mint(pointer, node, brought, b.f.assembled)
}

// meetNode returns the node base and partners, members that meet it, stand on:
// base's own where no partner adds anything, and otherwise one minted at
// pointer, built from the node each stands for (registry.view). A literal among
// them is the node, since it is within each; a literal meets only one node, so
// it is built beside one partner alone.
func (b *builder) meetNode(base member, partners []member, pointer jsontext.Pointer, depth int) ir.TypeID {
	if len(partners) == 0 {
		return base.ref.Target
	}
	all := append([]member{base}, partners...)
	nodes := make([]ir.TypeDef, len(all))
	reg := registry{b.f.g.resolve}
	for k, member := range all {
		td, ok := reg.resolve(member.ref.Target)
		if !ok {
			b.failed = true
			return base.ref.Target
		}
		nodes[k] = td
	}
	stop := reg.sharedLink(nodes...)
	for k, td := range nodes {
		nodes[k] = reg.view(td, stop)
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
	return b.mint(pointer, built, nil)
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
		valueType, narrowest := narrowestValueType(nodes)
		out.ValueType = valueType
		return &out, narrowest && len(out.Members) > 0
	case *ir.List:
		out := *n
		out.ElementType = b.settle(membersOf(all, nodes, func(list *ir.List) ir.TypeRef { return list.ElementType }), pointer+"/items", depth)
		return &out, sameKind[*ir.List](nodes)
	case *ir.MapT:
		out := *n
		out.Value = b.settle(membersOf(all, nodes, func(mapT *ir.MapT) ir.TypeRef { return mapT.Value }), pointer+"/additionalProperties", depth)
		return &out, sameKind[*ir.MapT](nodes)
	case *ir.Tuple:
		return b.tuple(n, all, nodes, pointer, depth)
	default:
		return nil, false
	}
}

// tuple is build for a tuple: each position, and what may follow the last,
// built from all the tuples' (settle), which meet only at one length and one
// presence of what follows (typeJudge.tuples).
func (b *builder) tuple(tuple *ir.Tuple, all []member, nodes []ir.TypeDef, pointer jsontext.Pointer, depth int) (ir.TypeDef, bool) {
	if !shapedAs(tuple, nodes) {
		return nil, false
	}
	out := *tuple
	out.ElementTypes = make([]ir.TypeRef, len(tuple.ElementTypes))
	for i := range tuple.ElementTypes {
		at := pointer + jsontext.Pointer(fmt.Sprintf("/prefixItems/%d", i))
		out.ElementTypes[i] = b.settle(membersOf(all, nodes, func(irTuple *ir.Tuple) ir.TypeRef { return irTuple.ElementTypes[i] }), at, depth)
	}
	if tuple.Rest != nil {
		rest := b.settle(membersOf(all, nodes, func(irTuple *ir.Tuple) ir.TypeRef { return *irTuple.Rest }), pointer+"/items", depth)
		out.Rest = &rest
	}
	return &out, true
}

// shapedAs reports whether every node is a tuple of tuple's length, and admits what
// follows its last position where tuple does.
func shapedAs(tuple *ir.Tuple, nodes []ir.TypeDef) bool {
	for _, td := range nodes {
		irTuple, ok := td.(*ir.Tuple)
		if !ok || len(irTuple.ElementTypes) != len(tuple.ElementTypes) || (irTuple.Rest == nil) != (tuple.Rest == nil) {
			return false
		}
	}
	return true
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
// in ms's order: the values of all the enums at once. A node of others that is
// no enum holds none of them.
func sharedBy(ms []ir.EnumMember, others []ir.TypeDef) []ir.EnumMember {
	indexes := make([]memberIndex, len(others))
	for k, td := range others {
		enum, ok := td.(*ir.Enum)
		if !ok {
			return nil
		}
		indexes[k] = indexMembers(enum.Members)
	}
	out := make([]ir.EnumMember, 0, len(ms))
	for _, enumMember := range ms {
		if heldByAll(indexes, enumMember.Value) {
			out = append(out, enumMember)
		}
	}
	if len(out) == len(ms) {
		return ms
	}
	return out
}

// heldByAll reports whether each enum indexes indexed holds value.
func heldByAll(indexes []memberIndex, value ir.Value) bool {
	for _, memberIndex := range indexes {
		if _, held := memberIndex.find(value); !held {
			return false
		}
	}
	return true
}

// settle returns the type of one position of the nodes built at depth, its
// members ms: the model they compose, or the node they meet in built a level
// down, at pointer, admitting null as position.null reads them. A member
// outside the winner's cone fails the build, since no node holds it beside the
// rest. ms are judged in declaration order, as a field's own types are, so a
// tie goes to the later and what they compose is listed as they are written.
func (b *builder) settle(ms []member, pointer jsontext.Pointer, depth int) ir.TypeRef {
	ms = slices.SortedStableFunc(slices.Values(ms), func(memberValue, otherMember member) int { return cmp.Compare(memberValue.decl, otherMember.decl) })
	position := b.f.g.judgePosition(b.f.decls, ms, depth+1)
	all := func(int) bool { return true }
	var target ir.TypeID
	if len(position.conjuncts) > 0 {
		target = b.compose(position.conjuncts, pointer)
	} else {
		for k := range ms {
			if !position.holds(position.winner, k) {
				b.failed = true
				return ms[position.winner].ref
			}
		}
		base, partners := position.meeting(all)
		target = b.meetNode(base, partners, pointer, depth+1)
	}
	ref := position.null(registry{b.f.g.resolve}, all, depth+1)
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
	typeCommon := built.Common()
	*typeCommon = adopt(*typeCommon, others, b.f.g.Walk, func(k int, field string) { b.differ(all[k+1].decl, field) })
	if list, ok := built.(*ir.List); ok && list.Encoding == nil {
		b.adoptEncoding(list, all, nodes)
	}
	return built
}

// adoptEncoding sets on list, a list giving no encoding, the first one nodes[1:]
// give, reporting each later list giving another as adopt reports a field.
func (b *builder) adoptEncoding(list *ir.List, all []member, nodes []ir.TypeDef) {
	for k, td := range nodes[1:] {
		other, ok := td.(*ir.List)
		switch {
		case !ok || other.Encoding == nil:
		case list.Encoding == nil:
			list.Encoding = other.Encoding
		case !reflect.DeepEqual(list.Encoding, other.Encoding):
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
		"declarations of field %q give the %s of a type it holds differently; kept the one a later "+
			"declaration gives over the one at %s, which %s", b.f.merged.WireName, field, b.f.at(decl), b.f.decls[decl].keptAs())
}
