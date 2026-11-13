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
	for _, s := range chain {
		if s.Encoding != nil || keepsReference(s) {
			return td
		}
	}
	bounds, ok := chainBounds(chain, end)
	if !ok {
		return td
	}
	if v, ok := viewOf(end, chain, bounds); ok {
		return v
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
		s, alias := td.(*ir.Scalar)
		if !alias || s.Base == nil || s.Base.Target == stop {
			return chain, td
		}
		next, ok := r.resolve(s.Base.Target)
		if !ok {
			return nil, td
		}
		if _, kind := next.(*ir.Primitive); kind {
			return chain, td
		}
		chain, td = append(chain, s), next
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
	for k, s := range chain {
		ids[k] = s.Base.Target
	}
	return ids
}

// chainBounds returns the bounds end and every alias of chain say together, and
// false where two say a keyword no one value states. An enum or a const holds
// none: bounds every value it lists meets say nothing of it, and false is
// returned where they cut a value or have no judge (admitsConstraints), since
// the node they leave lists values none of its declarations do.
func chainBounds(chain []*ir.Scalar, end ir.TypeDef) (*ir.Constraints, bool) {
	bounds := boundsOf(end)
	for _, s := range chain {
		if sameBounds(s.Constraints, nil) {
			continue
		}
		if !Meets(bounds, s.Constraints) {
			return nil, false
		}
		var met ir.Constraints
		bounds = Constraints(Constraints(&met, bounds), s.Constraints)
	}
	switch n := end.(type) {
	case *ir.Enum:
		return nil, !slices.ContainsFunc(n.Members, func(m ir.EnumMember) bool {
			return !admitsConstraints(bounds, m.Value, 0)
		})
	case *ir.Literal:
		return nil, admitsConstraints(bounds, n.Value, 0)
	default:
		return bounds, true
	}
}

// chainCommon returns the TypeCommon a reference to chain's first alias reads:
// each field the nearest of chain and end sets, a redaction where any asks for
// it, and every Unmodeled key, the nearest keeping it giving it. It holds the
// first alias's site but for the ID, end's, beneath which an enum's members are
// named, and writes to no node.
func chainCommon(chain []*ir.Scalar, end ir.TypeDef) ir.TypeCommon {
	c := *end.Common()
	id := c.ID
	c.Unmodeled = maps.Clone(c.Unmodeled)
	out := reflect.ValueOf(&c).Elem()
	for _, s := range slices.Backward(chain) {
		near := s.TypeCommon
		kept := c.Unmodeled
		said := reflect.ValueOf(near)
		for i := range out.NumField() {
			if f := said.Field(i); !f.IsZero() {
				out.Field(i).Set(f)
			}
		}
		c.Unmodeled = kept
		for key, entry := range near.Unmodeled {
			if c.Unmodeled == nil {
				c.Unmodeled = ir.Unmodeled{}
			}
			c.Unmodeled[key] = entry
		}
	}
	site := chain[0].TypeCommon
	c.ID, c.Name, c.Anonymous, c.Provenance = id, site.Name, site.Anonymous, site.Provenance
	return c
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

// standsFor reports whether td is an alias whose chain reaches id: every value
// of td is one of id's, and a reference to td reads all id says where td says
// nothing else, so td is within the type id names.
func (r registry) standsFor(td ir.TypeDef, id ir.TypeID) bool {
	for range maxTypeResolveDepth {
		s, alias := td.(*ir.Scalar)
		if !alias || s.Base == nil {
			return false
		}
		if s.Base.Target == id {
			return true
		}
		next, ok := r.resolve(s.Base.Target)
		if !ok {
			return false
		}
		td = next
	}
	return false
}
