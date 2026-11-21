// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapi

import (
	"context"
	"reflect"
	"slices"

	"github.com/dexpace/morphic/ir"
)

var (
	typeIDType       = reflect.TypeFor[ir.TypeID]()
	typeRegistryType = reflect.TypeFor[ir.TypeRegistry]()
)

// walkContextFunc is how pruneUnreferencedTypes walks a value:
// ir.WalkValuesContext in production, or a stub in tests that exercises
// truncation and cancellation.
type walkContextFunc func(ctx context.Context, root any, path string, visitors ...ir.Visitor) (bool, error)

// pruneUnreferencedTypes returns doc with every anonymous type in doc.Types
// that nothing reaches removed through an ir.Editor. A node is live when any
// non-anonymous type, any TypeID outside doc.Types, or any ID in extraRoots
// (reserved $ref landings and an empty-named root component schema) reaches it
// through typed TypeID references. A cancelled or truncated walk removes
// nothing so no live type is dropped.
func pruneUnreferencedTypes(ctx context.Context, doc *ir.Document, extraRoots []ir.TypeID) (*ir.Document, []ir.Diagnostic) {
	return pruneUnreferencedTypesWith(ctx, doc, extraRoots, ir.WalkValuesContext)
}

// pruneUnreferencedTypesWith is pruneUnreferencedTypes walking with walk.
func pruneUnreferencedTypesWith(ctx context.Context, doc *ir.Document, extraRoots []ir.TypeID, walk walkContextFunc) (*ir.Document, []ir.Diagnostic) {
	anon := anonymousTypes(doc.Types)
	if len(anon) == 0 {
		return doc, nil
	}
	collector := newTypeEdgeCollector(anon, extraRoots)
	if !collector.walk(ctx, doc, walk) {
		return doc, nil
	}
	dead := unreachableAnonymous(anon, collector.roots, collector.edges)
	if len(dead) == 0 {
		return doc, nil
	}
	editor := ir.NewEditor(doc)
	for _, id := range dead {
		editor.RemoveType(id)
	}
	return editor.Finish()
}

// anonymousTypes returns the set of anonymous type IDs in types, skipping any
// nil definition.
func anonymousTypes(types ir.TypeRegistry) map[ir.TypeID]bool {
	anon := make(map[ir.TypeID]bool)
	for id, td := range types {
		if !ir.IsNilTypeDef(td) && td.Common().Anonymous {
			anon[id] = true
		}
	}
	return anon
}

var _ ir.Visitor = (*typeEdgeCollector)(nil)

// typeEdgeCollector gathers the anonymous types referenced from outside
// Document.Types and from non-anonymous types into roots, and edges between
// anonymous types into edges. It prunes subtrees whose Go types cannot carry
// an ir.TypeID.
type typeEdgeCollector struct {
	anon      map[ir.TypeID]bool
	roots     []ir.TypeID
	edges     map[ir.TypeID][]ir.TypeID
	carries   map[reflect.Type]bool
	inTypes   bool
	owner     ir.TypeID
	ownerAnon bool
}

// newTypeEdgeCollector returns a collector seeded with the anonymous IDs in
// extraRoots.
func newTypeEdgeCollector(anon map[ir.TypeID]bool, extraRoots []ir.TypeID) *typeEdgeCollector {
	roots := make([]ir.TypeID, 0, len(extraRoots))
	for _, id := range extraRoots {
		if anon[id] {
			roots = append(roots, id)
		}
	}
	return &typeEdgeCollector{
		anon:    anon,
		roots:   roots,
		edges:   make(map[ir.TypeID][]ir.TypeID),
		carries: make(map[reflect.Type]bool),
	}
}

// walk visits doc outside doc.Types, then each definition in doc.Types in
// sorted ID order, reporting whether every walk completed without truncation
// or cancellation.
func (c *typeEdgeCollector) walk(ctx context.Context, doc *ir.Document, walk walkContextFunc) bool {
	truncated, err := walk(ctx, doc, ir.DocumentPath, c)
	if err != nil || truncated {
		return false
	}
	c.inTypes = true
	for _, td := range sortedTypeDefs(doc.Types) {
		if ir.IsNilTypeDef(td) {
			continue
		}
		c.owner = td.Common().ID
		c.ownerAnon = c.anon[c.owner]
		truncated, err = walk(ctx, td, ir.DocumentPath+".Types", c)
		if err != nil || truncated {
			return false
		}
	}
	return true
}

// sortedTypeDefs returns the definitions of types in ascending ID order so a
// walk sees every type deterministically without ranging map keys as references.
func sortedTypeDefs(types ir.TypeRegistry) []ir.TypeDef {
	ids := make([]ir.TypeID, 0, len(types))
	for id := range types {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	defs := make([]ir.TypeDef, 0, len(ids))
	for _, id := range ids {
		defs = append(defs, types[id])
	}
	return defs
}

// Visit records each anonymous TypeID reference v carries, skipping doc.Types
// during the outer walk.
func (c *typeEdgeCollector) Visit(v reflect.Value, _ ir.WalkPath) bool {
	t := v.Type()
	if !c.inTypes && t == typeRegistryType {
		return false
	}
	if t == typeIDType {
		id, _ := reflect.TypeAssert[ir.TypeID](v)
		c.record(id)
		return false
	}
	return c.canCarry(t)
}

// record adds id as a root when reached from outside doc.Types or from a
// non-anonymous type, or as an edge from owner when both are anonymous.
func (c *typeEdgeCollector) record(id ir.TypeID) {
	if !c.anon[id] {
		return
	}
	if !c.inTypes || !c.ownerAnon {
		c.roots = append(c.roots, id)
		return
	}
	if id != c.owner {
		c.edges[c.owner] = append(c.edges[c.owner], id)
	}
}

// canCarry reports whether a value of type t can hold an ir.TypeID at any
// depth, cached per type.
func (c *typeEdgeCollector) canCarry(t reflect.Type) bool {
	if known, read := c.carries[t]; read {
		return known
	}
	seen := map[reflect.Type]bool{t: true}
	queue := []reflect.Type{t}
	var carries bool
	for len(queue) > 0 && !carries {
		next := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		var held []reflect.Type
		carries, held = holdsTypeID(next)
		for _, h := range held {
			if !seen[h] {
				seen[h] = true
				queue = append(queue, h)
			}
		}
	}
	c.carries[t] = carries
	return carries
}

// holdsTypeID reports whether t is ir.TypeID and otherwise returns the types a
// value of t directly contains. No IR struct holds an array or an interface
// outside Document.Types (which walk unboxes per TypeDef).
func holdsTypeID(t reflect.Type) (bool, []reflect.Type) {
	switch t.Kind() {
	case reflect.String:
		return t == typeIDType, nil
	case reflect.Pointer, reflect.Slice:
		return false, []reflect.Type{t.Elem()}
	case reflect.Map:
		return false, []reflect.Type{t.Key(), t.Elem()}
	case reflect.Struct:
		fields := make([]reflect.Type, t.NumField())
		for i := range fields {
			fields[i] = t.Field(i).Type
		}
		return false, fields
	default:
		return false, nil
	}
}

// unreachableAnonymous returns, in sorted order, every ID in anon that no path
// in edges from roots reaches.
func unreachableAnonymous(anon map[ir.TypeID]bool, roots []ir.TypeID, edges map[ir.TypeID][]ir.TypeID) []ir.TypeID {
	reached := make(map[ir.TypeID]bool, len(anon))
	stack := slices.Clone(roots)
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if reached[id] {
			continue
		}
		reached[id] = true
		for _, next := range edges[id] {
			if !reached[next] {
				stack = append(stack, next)
			}
		}
	}
	if len(reached) == len(anon) {
		return nil
	}
	dead := make([]ir.TypeID, 0, len(anon)-len(reached))
	for id := range anon {
		if !reached[id] {
			dead = append(dead, id)
		}
	}
	slices.Sort(dead)
	return dead
}
