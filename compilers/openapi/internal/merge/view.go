// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"maps"
	"reflect"
	"slices"

	"github.com/dexpace/morphic/ir"
)

// view returns the node the judgment compares, and the builder builds from, for
// td (docs/compilers/openapi.md#allof-redeclared-property): td itself, or, where
// td is a chain of aliases ending in an enum, a const, a Scalar, a list, a map or
// a tuple, a copy of that node holding what the chain adds (chainBounds,
// chainCommon), read down to stop, where the alias naming stop is the node. A
// chain giving an encoding or bounds no one value states, or keeping a reference
// under Unmodeled, which reads against where it is written, is td.
func (r registry) view(td ir.TypeDef, stop ir.TypeID) ir.TypeDef {
	chain, end := r.aliasedNode(td, stop)
	if len(chain) == 0 || keepsReference(end) {
		return td
	}
	for _, scalar := range chain {
		if scalar.Encoding != nil || keepsReference(scalar) {
			return td
		}
	}
	bounds, ok := chainBounds(chain, end)
	if !ok {
		return td
	}
	if typeDef, ok := viewOf(end, chain, bounds); ok {
		return typeDef
	}
	return td
}

// aliasedNode returns the aliases a chain from td leads through, td first, and
// the node it ends in: the alias naming stop, the first node that is no alias,
// or a Scalar over a kind, whose Base says what its values are rather than naming
// another node. It returns no chain where a link names nothing the registry
// holds or the chain runs past maxTypeResolveDepth aliases.
func (r registry) aliasedNode(td ir.TypeDef, stop ir.TypeID) ([]*ir.Scalar, ir.TypeDef) {
	var chain []*ir.Scalar
	for range maxTypeResolveDepth + 1 {
		scalar, alias := td.(*ir.Scalar)
		if !alias || scalar.Base == nil || scalar.Base.Target == stop {
			return chain, td
		}
		next, ok := r.resolve(scalar.Base.Target)
		if !ok {
			return nil, td
		}
		if _, kind := next.(*ir.Primitive); kind {
			return chain, td
		}
		chain, td = append(chain, scalar), next
	}
	return nil, td
}

// sharedLink returns the first node every alias chain from tds names, and ""
// where no node is named by all of them. Each chain is a path one Base at a time,
// so where two meet they share the rest, and the first node they share is where
// aliases of one node stop being read through: what each adds to it is judged
// against what the others add, and a node built of them stays an alias of it.
func (r registry) sharedLink(tds ...ir.TypeDef) ir.TypeID {
	first := r.links(tds[0])
	if len(first) == 0 {
		return ""
	}
	rest := make([][]ir.TypeID, len(tds)-1)
	for k, td := range tds[1:] {
		if rest[k] = r.links(td); len(rest[k]) == 0 {
			return ""
		}
	}
	for _, id := range first {
		if !slices.ContainsFunc(rest, func(ids []ir.TypeID) bool { return !slices.Contains(ids, id) }) {
			return id
		}
	}
	return ""
}

// links returns the nodes an alias chain from td names, nearest first, down to
// the node it ends in (aliasedNode).
func (r registry) links(td ir.TypeDef) []ir.TypeID {
	chain, _ := r.aliasedNode(td, "")
	ids := make([]ir.TypeID, len(chain))
	for k, scalar := range chain {
		ids[k] = scalar.Base.Target
	}
	return ids
}

// chainBounds returns the bounds end and every alias of chain say together, and
// false where two say a keyword no one value states. A closed enum or a const
// holds none: bounds every value it lists meets say nothing of it, and false is
// returned where they cut a value or have no judge (admitsConstraints), since
// the node they leave lists values none of its declarations do. An open enum's
// bounds also constrain the values it does not list, which no node of its kind
// holds, so false is returned for any.
func chainBounds(chain []*ir.Scalar, end ir.TypeDef) (*ir.Constraints, bool) {
	bounds := boundsOf(end)
	for _, scalar := range chain {
		if sameBounds(scalar.Constraints, nil) {
			continue
		}
		if !Meets(bounds, scalar.Constraints) {
			return nil, false
		}
		var met ir.Constraints
		bounds = Constraints(Constraints(&met, bounds), scalar.Constraints)
	}
	switch n := end.(type) {
	case *ir.Enum:
		if !n.Closed && !sameBounds(bounds, nil) {
			return nil, false
		}
		return nil, !slices.ContainsFunc(n.Members, func(enumMember ir.EnumMember) bool {
			return !admitsConstraints(bounds, enumMember.Value, 0)
		})
	case *ir.Literal:
		return nil, admitsConstraints(bounds, n.Value, 0)
	default:
		return bounds, true
	}
}

// chainCommon returns the TypeCommon a reference to chain's first alias reads:
// each field the nearest of chain and end sets, the documentation field by field
// (nearerDocs), a redaction where any asks for it, and every Unmodeled key, the
// nearest keeping it giving it. It holds the first alias's site but for the ID,
// end's, beneath which an enum's members are named, and writes to no node.
func chainCommon(chain []*ir.Scalar, end ir.TypeDef) ir.TypeCommon {
	typeCommon := *end.Common()
	id := typeCommon.ID
	typeCommon.Unmodeled = maps.Clone(typeCommon.Unmodeled)
	out := reflect.ValueOf(&typeCommon).Elem()
	for _, scalar := range slices.Backward(chain) {
		near := scalar.TypeCommon
		kept, docs := typeCommon.Unmodeled, nearerDocs(typeCommon.Docs, near.Docs)
		said := reflect.ValueOf(near)
		for i := range out.NumField() {
			if f := said.Field(i); !f.IsZero() {
				out.Field(i).Set(f)
			}
		}
		typeCommon.Unmodeled, typeCommon.Docs = kept, docs
		for key, entry := range near.Unmodeled {
			if typeCommon.Unmodeled == nil {
				typeCommon.Unmodeled = ir.Unmodeled{}
			}
			typeCommon.Unmodeled[key] = entry
		}
	}
	site := chain[0].TypeCommon
	typeCommon.ID, typeCommon.Name, typeCommon.Anonymous, typeCommon.Provenance = id, site.Name, site.Anonymous, site.Provenance
	return typeCommon
}

// nearerDocs returns far with each field near sets in its place, a description
// and its Refs as one, as a reference carries documentation down to its use
// (docs/ir/annotations.md#what-a-reference-carries-down).
func nearerDocs(far, near ir.Docs) ir.Docs {
	if near.Summary != "" {
		far.Summary = near.Summary
	}
	if near.Description != "" {
		far.Description, far.Refs = near.Description, near.Refs
	}
	if len(near.ExternalDocs) > 0 {
		far.ExternalDocs = near.ExternalDocs
	}
	return far
}

// viewOf returns a copy of end holding what a reference to chain's first alias
// reads (chainCommon) and, where its kind holds them, bounds, and false where end
// is of a kind the merge builds no node of: a model, a union or what the
// judgment reads otherwise.
func viewOf(end ir.TypeDef, chain []*ir.Scalar, bounds *ir.Constraints) (ir.TypeDef, bool) {
	switch n := end.(type) {
	case *ir.Enum:
		out := *n
		out.TypeCommon = chainCommon(chain, n)
		return &out, true
	case *ir.Literal:
		out := *n
		out.TypeCommon = chainCommon(chain, n)
		return &out, true
	case *ir.Scalar:
		out := *n
		out.TypeCommon, out.Constraints = chainCommon(chain, n), bounds
		return &out, true
	case *ir.List:
		out := *n
		out.TypeCommon, out.Constraints = chainCommon(chain, n), bounds
		return &out, true
	case *ir.MapT:
		out := *n
		out.TypeCommon, out.Constraints = chainCommon(chain, n), bounds
		return &out, true
	case *ir.Tuple:
		out := *n
		out.TypeCommon, out.Constraints = chainCommon(chain, n), bounds
		return &out, true
	default:
		return nil, false
	}
}

// standsFor relates td to the type id names where td is an alias whose chain
// reaches id: a reference to td reads all id says, and td may say more, so td is
// within it (firstTypeIsNarrower), unless a link admits null, which id lacks. Such a
// chain whose every alias says nothing else (aliasAddsNothingBeyondBase), docs included,
// adds only null, so id is within td (secondTypeIsNarrower). Any other is relUnrelated,
// for the view to judge, as is a td whose chain does not reach id. Docs count,
// as a use of td reads them (docs/compilers/openapi.md#ref-use-site).
func (r registry) standsFor(td ir.TypeDef, id ir.TypeID) relation {
	null, says := false, false
	for range maxTypeResolveDepth {
		scalar, alias := td.(*ir.Scalar)
		if !alias || scalar.Base == nil {
			return relUnrelated
		}
		null, says = null || scalar.Base.Nullable, says || !aliasAddsNothingBeyondBase(scalar, false)
		if scalar.Base.Target == id {
			return addsNull(null, says)
		}
		next, ok := r.resolve(scalar.Base.Target)
		if !ok {
			return relUnrelated
		}
		td = next
	}
	return relUnrelated
}

// addsNull relates an alias chain to the type it reaches by what it adds: one
// admitting no null is within the type, one adding null alone holds it, and one
// adding null and more is related to it by neither.
func addsNull(null, says bool) relation {
	switch {
	case !null:
		return firstTypeIsNarrower
	case !says:
		return secondTypeIsNarrower
	default:
		return relUnrelated
	}
}

// nullThroughChain reports whether a link of td's alias chain admits null, so
// td's values hold null whatever a reference to it says.
func (r registry) nullThroughChain(td ir.TypeDef) bool {
	chain, _ := r.aliasChain(td)
	return slices.ContainsFunc(chain, func(scalar *ir.Scalar) bool { return scalar.Base.Nullable })
}
