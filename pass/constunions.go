// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass

import (
	"context"
	"maps"
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
// a string Literal, a one-member Enum or a union that is one in turn, and gives
// its member what it says of the value. Where the Enum could not say all the
// union does, or a variant names several values, the union stays. It holds no
// state, so it is safe for concurrent use.
type ConstUnions struct{}

// Name returns [ConstUnionsName].
func (ConstUnions) Name() string { return ConstUnionsName }

// Run returns doc with each union constEnum reads as an Enum replaced by it,
// and each type only those unions named, which nothing references once they
// are gone, removed. It returns doc unchanged where no union is one, and with
// nothing once ctx is done.
func (ConstUnions) Run(ctx context.Context, doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	if doc == nil {
		return doc, nil
	}
	enums, branches := constEnums(doc)
	if len(enums) == 0 || ctx.Err() != nil {
		return doc, nil
	}
	e := ir.NewEditor(doc)
	for _, enum := range enums {
		e.ReplaceType(enum)
	}
	out, diags := e.Finish()

	dead, err := unreferenced(ctx, out, branches)
	if err != nil {
		return doc, nil
	}
	if len(dead) == 0 {
		return out, diags
	}
	trim := ir.NewEditor(out)
	for _, id := range dead {
		trim.RemoveType(id)
	}
	trimmed, more := trim.Finish()
	return trimmed, append(diags, more...)
}

// constEnums returns, in ID order, the Enum of each union of doc that
// constEnum reads as one, and the anonymous types their variants name, which
// the Enums hold the values of. A union off its own key is left to validate.
func constEnums(doc *ir.Document) ([]*ir.Enum, []ir.TypeID) {
	r := readingOf(doc)
	enums := slices.SortedFunc(maps.Values(r.enums), func(a, b *ir.Enum) int {
		return strings.Compare(string(a.ID), string(b.ID))
	})
	return enums, slices.Sorted(maps.Keys(r.named))
}

// readingOf returns the reading of each union of doc.
func readingOf(doc *ir.Document) *unionReading {
	r := &unionReading{types: doc.Types, spaces: doc.IDSpaces[ir.IDKindEnumMember],
		enums: map[ir.TypeID]*ir.Enum{}, named: map[ir.TypeID]bool{},
		opened: map[ir.TypeID]bool{}, read: map[ir.TypeID]bool{}}
	for id := range doc.Types {
		r.settle(id)
	}
	return r
}

// unionReading reads each union of a registry as constEnum does, after the
// unions its variants name, each as the Enum it becomes: a union naming one is
// rewritten in the run that rewrites it, where constEnum reads it as an Enum,
// so a second run finds none left (docs/architecture.md §2.2).
type unionReading struct {
	types  ir.TypeRegistry
	spaces []string
	// enums holds the Enum each union read as one becomes, and named the types
	// those unions' variants name.
	enums map[ir.TypeID]*ir.Enum
	named map[ir.TypeID]bool
	// opened holds each union whose variants were queued to be read first, and
	// read each union read.
	opened, read map[ir.TypeID]bool
	// stack holds the IDs settle has yet to read.
	stack []ir.TypeID
	// reads counts the unions read, each once however many name it, which is
	// what a reading costs.
	reads int
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
			r.readUnion(u)
		}
	}
}

// readUnion records the Enum u is, where constEnum reads it as one.
func (r *unionReading) readUnion(u *ir.Union) {
	r.reads++
	enum, ok := constEnum(r.typeAt, r.spaces, u)
	if !ok {
		return
	}
	r.enums[u.ID] = enum
	for _, v := range u.Variants {
		r.named[v.Type.Target] = true
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

// typeAt returns the type at id as read so far: the Enum a union read as one
// becomes, and otherwise the registry's entry.
func (r *unionReading) typeAt(id ir.TypeID) ir.TypeDef {
	if enum, read := r.enums[id]; read {
		return enum
	}
	return r.types[id]
}
