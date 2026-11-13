// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass

import (
	"context"
	"slices"
	"strings"

	"github.com/dexpace/morphic/ir"
)

var _ Pass = ConstUnions{}

// ConstUnionsName is the stable name of the [ConstUnions] pass.
const ConstUnionsName = "const-unions"

// ConstUnions rewrites a closed union of strings as the closed [ir.Enum] of
// them, at the union's own ID, so a consumer meets one closed set of strings in
// one shape however the document spelled it
// (docs/compilers/openapi.md#string-const-union). Each variant names one value,
// which its member holds with what the variant says of it, or the values of a
// branch only that variant names, which moves into the Enum whole. Where the
// Enum could not say all the union does, the union stays. It holds no state,
// so it is safe for concurrent use.
type ConstUnions struct{}

// Name returns [ConstUnionsName].
func (ConstUnions) Name() string { return ConstUnionsName }

// Run returns doc with each union read as an Enum replaced by it, and each
// branch type those unions alone referenced removed. It returns doc unchanged
// where no union is one, and with nothing once ctx is done. A document holding
// no union that may be one costs no walk.
func (ConstUnions) Run(ctx context.Context, doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	if doc == nil {
		return doc, nil
	}
	refs := refsTo(doc.Types)
	if len(refs.watched) == 0 {
		return doc, nil
	}
	truncated, err := ir.WalkValuesContext(ctx, doc, ir.DocumentPath, refs)
	if err != nil {
		return doc, nil
	}
	if truncated {
		refs = nil // the walk may have missed a reference, so it counted none
	}
	enums, dead := readingOf(doc, refs).rewrite()
	if len(enums) == 0 || ctx.Err() != nil {
		return doc, nil
	}
	e := ir.NewEditor(doc)
	for _, enum := range enums {
		e.ReplaceType(enum)
	}
	for _, id := range dead {
		e.RemoveType(id)
	}
	return e.Finish()
}

// readingOf returns the reading of each union of doc, taking a branch of
// several values only where refs shows the union naming it is its one
// reference, and none where refs is nil.
func readingOf(doc *ir.Document, refs *typeRefs) *unionReading {
	r := &unionReading{types: doc.Types, spaces: doc.IDSpaces[ir.IDKindEnumMember], refs: refs,
		values: map[ir.TypeID]*unionValues{}, opened: map[ir.TypeID]bool{}, read: map[ir.TypeID]bool{}}
	for id := range doc.Types {
		r.settle(id)
	}
	return r
}

// unionReading reads each union of a registry as valuesOf does, after the
// unions its variants name, each as the values it holds: a union naming one is
// rewritten in the run that rewrites it, so a second run finds none left
// (docs/architecture.md §2.2).
type unionReading struct {
	types  ir.TypeRegistry
	spaces []string
	refs   *typeRefs
	// values holds the values of each union read as an Enum.
	values map[ir.TypeID]*unionValues
	// opened holds each union whose variants were queued to be read first, and
	// read each union read.
	opened, read map[ir.TypeID]bool
	// stack holds the IDs settle has yet to read.
	stack []ir.TypeID
	// reads counts the unions read, each once however many name it, and work
	// each key distinct sets and each member enum builds: what a reading
	// costs.
	reads, work int
}

// settle reads the union at root, if it is one keyed at its own ID and not
// read yet, after each union its variants name. A union met again through a
// cycle is read before what it names is: no union whose variants reach it
// again is an Enum, read in either order.
func (r *unionReading) settle(root ir.TypeID) {
	r.stack = append(r.stack[:0], root)
	for len(r.stack) > 0 {
		id := r.stack[len(r.stack)-1]
		u := r.union(id)
		switch {
		case u == nil || r.read[id]:
			r.stack = r.stack[:len(r.stack)-1]
		case !r.opened[id]:
			r.opened[id] = true
			for _, v := range u.Variants {
				r.stack = append(r.stack, v.Type.Target)
			}
		default:
			r.stack = r.stack[:len(r.stack)-1]
			r.read[id] = true
			r.reads++
			if vs, ok := r.valuesOf(u); ok {
				r.values[id] = vs
			}
		}
	}
}

// union returns the union the registry keys at id under its own ID, or nil.
func (r *unionReading) union(id ir.TypeID) *ir.Union {
	u, isUnion := r.types[id].(*ir.Union)
	if !isUnion || u == nil || u.ID != id {
		return nil
	}
	return u
}

// rewrite returns, in ID order, the Enum each union read as one becomes, and
// the branch types the rewrite leaves unreferenced, which it removes instead:
// each type every reference to which is a variant of a union read as an Enum.
// Those references go with the unions, and the rewrite moves every other a
// branch or a variant holds onto a member rather than dropping it, so a type
// one of them names stays. A union read as an Enum that goes too gave its
// values to the union taking them, so its own Enum is never built.
func (r *unionReading) rewrite() ([]*ir.Enum, []ir.TypeID) {
	named := map[ir.TypeID]int{}
	for _, vs := range r.values {
		for _, v := range vs.union.Variants {
			named[v.Type.Target]++
		}
	}
	var dead []ir.TypeID
	for id, variants := range named {
		if r.refs.onlyVariants(id, variants) {
			dead = append(dead, id)
		}
	}
	slices.Sort(dead)
	enums := make([]*ir.Enum, 0, len(r.values))
	for id, vs := range r.values {
		if _, gone := slices.BinarySearch(dead, id); !gone {
			enums = append(enums, r.enum(vs))
		}
	}
	slices.SortFunc(enums, func(a, b *ir.Enum) int { return strings.Compare(string(a.ID), string(b.ID)) })
	return enums, dead
}
