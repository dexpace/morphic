// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"context"
	"encoding/json/jsontext"
	"maps"
	"slices"
	"strings"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// lowerDiscriminator lowers d, a discriminator the schema written at pointer
// declares, nil for none: its tag, located by exactly one of Property,
// PropertyName and Index (docs/ir/models.md#discriminator), and its routes,
// each mapping value and the defaultMapping read by discriminatorMapping. m is
// the model declaring it, whose property's PropID locates the tag, else
// PropertyName; a nil m, for a union, names it by PropertyName alone. x is a
// union's branches, nil for a model: a union routes to its variants
// (branchIndex.route), a model to the type each value's chain ends on
// (hierarchyRoute).
func lowerDiscriminator(c lowering.Ctx, ts *compile.Types, d *statement.Discriminator, pointer jsontext.Pointer, m *ir.Model, x *branchIndex) (*ir.Discriminator, []ir.Diagnostic) {
	if d == nil || !tagLocated(d, m) {
		return nil, nil
	}
	mapping, diags := discriminatorMapping(c, ts, d, x, pointer)
	disc := &ir.Discriminator{Mapping: mapping}
	if pid, ok := propIDByName(m, d.PropertyName); ok {
		disc.Property = pid
	} else {
		disc.PropertyName = d.PropertyName
	}
	defaultID, defaultDiags := discriminatorDefault(c, ts, d, x, pointer)
	disc.Default = defaultID
	return disc, append(diags, defaultDiags...)
}

// tagLocated reports whether the IR can locate the tag d names, on m where a
// model declares it: by any name but the empty one, which PropertyName empty
// cannot spell, and by that one only as the PropID of a property m declares
// itself (docs/compilers/openapi.md#discriminator). One it cannot locate is no
// node's, and the census keeps it verbatim.
func tagLocated(d *statement.Discriminator, m *ir.Model) bool {
	if d.PropertyName != "" {
		return true
	}
	_, ok := propIDByName(m, "")
	return ok
}

// propIDByName returns the PropID of the model property with the given source
// name, if the model declares it. m is nil-safe: lowerDiscriminator's
// oneOf/anyOf union case has no model to search, only a tag name.
func propIDByName(m *ir.Model, name string) (ir.PropID, bool) {
	if m == nil {
		return "", false
	}
	for i := range m.Properties {
		if m.Properties[i].Name.Source == name {
			return m.Properties[i].ID, true
		}
	}
	return "", false
}

// subtypeTag is the wire tag an allOf subtype carries in its hierarchy, as
// subtypeDiscriminatorValue reads it.
type subtypeTag struct {
	// value is the tag, "" being one (docs/ir/models.md).
	value string
	// routed reports that a discriminator routes to the subtype, value being
	// the tag it routes by.
	routed bool
	// byName is the discriminator routing it by its component name, OpenAPI's
	// implicit mapping, nil where a mapping key tags it or none routes to it.
	byName *statement.Discriminator
}

// subtypeDiscriminatorValue returns the wire tag this allOf subtype carries:
// the mapping key naming it, else its declared schema name where a
// discriminator routes to it by name (GitHub #505, #517).
//
// Every discriminated ancestor is asked, nearest first (GitHub #305); within a
// level, first, the schema s's Base names, and its ancestors lead, so the
// Base's hierarchy tags it whichever entry is written first. Of several keys
// the smallest in byte order is elected (GitHub #410), with an info.
func subtypeDiscriminatorValue(c lowering.Ctx, s *statement.Schema, id ir.TypeID, pointer jsontext.Pointer,
	first *statement.Schema,
) (subtypeTag, []ir.Diagnostic) {
	var lead []*statement.Schema
	if stated(first) {
		lead = append([]*statement.Schema{first}, discriminatedAncestors(c, first)...)
	}
	var ancestors []*statement.Schema
	for _, level := range ancestorLevels(c, s) {
		slices.SortStableFunc(level, func(a, b *statement.Schema) int {
			return compareTrueFirst(slices.Contains(lead, a), slices.Contains(lead, b))
		})
		ancestors = append(ancestors, level...)
	}
	for _, a := range ancestors {
		tags := mappingTagsFor(c, a.Discriminator(), id)
		if len(tags) == 0 {
			continue
		}
		tag := subtypeTag{value: tags[0], routed: true}
		if len(tags) == 1 {
			return tag, nil
		}
		return tag, []ir.Diagnostic{c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
			"the discriminator mapping names this schema under %d keys (%s); "+
				"discriminatorValue holds the smallest in byte order, and the base's mapping keeps them all",
			len(tags), strings.Join(tags, ", "))}
	}
	// Read the state, not only the name: a top-level entry of another kind, such
	// as components/x-shared/Cat, has none to read (GitHub #572).
	name, state := ids.ComponentSchema(pointer)
	if state != ids.NamedComponent {
		return subtypeTag{}, nil
	}
	for _, a := range ancestors {
		if routesByName(c, a, s) {
			return subtypeTag{value: name, routed: true, byName: a.Discriminator()}, nil
		}
	}
	return subtypeTag{}, nil
}

// listedBy keys the schemas the union a schema writes lists (routesByName).
type listedBy struct {
	schema *statement.Schema
}

// routesByName reports whether the discriminator the ancestor a declares routes
// the component schema s by its name, OpenAPI's implicit mapping. One standing
// with no oneOf or anyOf routes every schema composing a through allOf. One
// standing with either (standingUnion) routes the schemas that union lists,
// every possible one listed there (docs/compilers/openapi.md#discriminator-value):
// those a $ref branch of it ends on, as discriminatedAncestors reads a $ref,
// read once a run, as every subtype of a asks. Any other schema composing a
// narrows that union, each instance carrying its variant's tag, and composes
// it as written (docs/ir/models.md#a-union-a-composition-names).
func routesByName(c lowering.Ctx, a, s *statement.Schema) bool {
	holder := standingUnion(c, a)
	if !stated(holder) {
		return true
	}
	branches, _, _ := unionBranches(c, a, holder)
	listed := lowering.Remembered(c, listedBy{a}, func() map[*statement.Schema]bool {
		set := make(map[*statement.Schema]bool, len(branches))
		for _, b := range branches {
			if target := refBranchTarget(c, c.Statements().Sub(b)); stated(target) {
				set[target] = true
			}
		}
		return set
	})
	return listed[s]
}

// standingUnion returns the schema writing the union the discriminator a
// declares stands with: a where it writes one, else the branch of a's allOf
// whose union a lowers (findBranchUnion), which writes no discriminator of its
// own beside a's, and nil for none, a hierarchy's base.
func standingUnion(c lowering.Ctx, a *statement.Schema) *statement.Schema {
	if declaresUnion(a) {
		return a
	}
	at := branchUnionIndexOf(c, a)
	if !at.ok {
		return nil
	}
	return allOfBranches(c, a, "").branches[at.index].schema.Schema
}

// maxDiscriminatorAncestorDepth bounds how many composition levels
// discriminatedAncestors climbs (styleguide bounded-everything rule). The visited
// set below is what makes the walk terminate; this is the explicit limit, set far
// beyond any hierarchy a source document plausibly declares.
const maxDiscriminatorAncestorDepth = 256

// discriminatedAncestors returns s's composition ancestors declaring a
// discriminator, nearest first: the resolved targets of s's own $ref branches
// in source order, then theirs, level by level. Nearest means fewest hops,
// which decides between two ancestors whose mappings name the same subtype.
//
// The visited set bounds the work; the depth cap bounds only the levels. A
// schema reached by several branches would be walked once per path, so the
// frontier multiplies by the fan-out each level (2^level for a chain of
// diamonds). TestAllOf_DiscriminatorValueCyclicComposition (cyclic) and
// TestCompile_SharedCompositionAncestorsDoNotAmplify (acyclic) pin it.
func discriminatedAncestors(c lowering.Ctx, s *statement.Schema) []*statement.Schema {
	return slices.Concat(ancestorLevels(c, s)...)
}

// ancestorLevels is discriminatedAncestors level by level, nearest first.
func ancestorLevels(c lowering.Ctx, s *statement.Schema) [][]*statement.Schema {
	var out [][]*statement.Schema
	visited := make(map[*statement.Schema]bool)
	// s is visited before the walk starts, so a composition cycle cannot bring it
	// back as one of its own ancestors. Without this a schema in a cycle that
	// declares a discriminator answers to its own mapping, which is a hierarchy of
	// one thing standing above itself.
	visited[s] = true
	level := []*statement.Schema{s}
	for depth := 0; depth < maxDiscriminatorAncestorDepth && len(level) > 0; depth++ {
		next := make([]*statement.Schema, 0, len(level))
		for _, cur := range level {
			next = append(next, unvisitedRefTargets(c, cur, visited)...)
		}
		var found []*statement.Schema
		for _, target := range next {
			if target.Discriminator() != nil {
				found = append(found, target)
			}
		}
		if len(found) > 0 {
			out = append(out, found)
		}
		level = next
	}
	return out
}

// composedByOf keys what composedByBranch read of one schema and the union
// standing with it.
type composedByOf struct {
	schema, holder *statement.Schema
}

// composedByBranch reports whether a branch of u, a union of s's, the combinator
// unionBranches elects, composes s through allOf at any depth, so that s's
// discriminator routes to a subtype of s: s is then the base of a hierarchy,
// which a union of its subtypes would no longer be
// (docs/compilers/openapi.md#union-beside-structure). Such a branch finds s's
// discriminator among its ancestors' (discriminatedAncestors). Where s writes
// one, it is the one standing with u (findBranchUnion). Every reader of the
// position asks, so it is read once a run.
func composedByBranch(c lowering.Ctx, s *statement.Schema, u unionAt) bool {
	d := s.Discriminator()
	if d == nil {
		return false
	}
	return lowering.Remembered(c, composedByOf{s, u.holder}, func() bool {
		branches, _, _ := u.branches(c)
		return slices.ContainsFunc(branches, func(b statement.Sub) bool {
			st := c.Statements().Sub(b)
			target := st.Schema
			if st.Ref != nil {
				target = refBranchTarget(c, st)
			}
			return stated(target) && slices.ContainsFunc(discriminatedAncestors(c, target),
				func(a *statement.Schema) bool { return a.Discriminator() == d })
		})
	})
}

// unvisitedRefTargets returns the schemas s's $ref composition branches resolve
// to, in source order, skipping those already seen and marking those it returns.
// Marking as it enqueues rather than when the walk reaches them is what keeps a
// schema two branches both name from being queued, and walked, twice.
func unvisitedRefTargets(c lowering.Ctx, s *statement.Schema, visited map[*statement.Schema]bool) []*statement.Schema {
	var out []*statement.Schema
	for b := range allOfRefBranches(c, s) {
		target := refBranchTarget(c, b)
		if !stated(target) || visited[target] {
			continue
		}
		visited[target] = true
		out = append(out, target)
	}
	return out
}

// discriminatorBesideUnion returns the finisher queueing a settlement for the
// discriminator s writes, which the model in hand holds beside the union holder
// writes, kept verbatim: beside the model by lowerBesideUnmodeledUnion, or with
// its allOf branch. A body that lowered to anything else holds none: the census
// kept its discriminator where it kept the rest, a multi-typed body's on its
// union (lowerTypeVariant).
func discriminatorBesideUnion(c lowering.Ctx, ts *compile.Types, s, holder *statement.Schema, pointer jsontext.Pointer) finisher {
	return func(td ir.TypeDef, _ keySet, _ *principalKept) {
		if isKind(td, ir.KindModel) {
			ts.Defer(besideUnionDiscriminator{c: c, s: s, pointer: pointer, model: td.Common().ID, named: namedBranches(c, s, holder)})
		}
	}
}

// namedBranches returns the components named by $ref among the branches of
// the union holder elects at the position s, in branch order: the types a
// discriminator routes to by name when no mapping key routes to them. An inline
// branch has no name and neither has any other position, so the discriminator
// reaches those only through its mapping, which the model's Discriminator
// already holds.
func namedBranches(c lowering.Ctx, s, holder *statement.Schema) []ir.TypeID {
	scope := c.RefScope()
	branches, _, _ := unionBranches(c, s, holder)
	var named []ir.TypeID
	for _, b := range branches {
		if _, id, ok := componentNamed(scope, referenceOf(c, c.Statements().Sub(b))); ok {
			named = append(named, id)
		}
	}
	return named
}

// componentNamed returns the source component the union branch b names by
// $ref, by name and by ID: the route OpenAPI's implicit mapping takes to it. It
// is false for a branch naming none, one written inline or a $ref to any other
// position. b is the branch's reference (referenceOf), nil for none.
func componentNamed(scope resolve.Scope, b *statement.Statement) (string, ir.TypeID, bool) {
	if b == nil {
		return "", "", false
	}
	at, ok := landsAt(b)
	if !ok || at.Doc != bind.Source {
		return "", "", false
	}
	id, state := scope.ComponentRef(at.Pointer)
	if state != ids.NamedComponent {
		return "", "", false
	}
	name, _ := ids.ComponentSchema(at.Pointer)
	return name, id, true
}

// spellImplicitRoutes writes into the discriminator of u, the Union at lowers,
// the route OpenAPI implies to each variant no mapping entry routes:
// the name of the component its $ref branch names, unless another branch names
// it too or an entry spells it. The IR routes by type name only where the
// Mapping is nil (docs/ir/models.md#discriminator), so these are written where
// it holds an entry, or a variant's type is not that component, as a composed
// variant's or an alias's is not. A non-null branch lacking its variant is a
// compiler bug, reported at pointer.
func spellImplicitRoutes(c lowering.Ctx, at unionAt, u *ir.Union, pointer jsontext.Pointer) []ir.Diagnostic {
	d := u.Discriminator
	if d == nil {
		return nil
	}
	routes := implicitRoutes(c, at)
	if len(routes) != len(u.Variants) {
		return []ir.Diagnostic{c.DiagAt(ir.SeverityError, diag.InternalInvariant, pointer,
			"internal: the union has %d variants for %d branches, so no route is written for them",
			len(u.Variants), len(routes))}
	}
	if len(d.Mapping) == 0 && typesAreNamed(routes, u.Variants) {
		return nil
	}
	count := map[string]int{}
	for _, r := range routes {
		if r.named {
			count[r.name]++
		}
	}
	routed := make(map[ir.TypeID]bool, len(d.Mapping))
	for _, target := range d.Mapping {
		routed[target] = true
	}
	for i, r := range routes {
		variant := u.Variants[i].Type.Target
		// An unnamed route writes nothing. count skips one too, but only as it
		// counts named routes alone, so the guard says it where it is meant. A
		// routed variant's name stays out though OpenAPI routes by it: a writer
		// takes the variant's tag from the Mapping, which cannot mark a second
		// route as only read (docs/compilers/openapi.md#discriminator).
		if !r.named || count[r.name] != 1 || routed[variant] {
			continue
		}
		if _, spelled := d.Mapping[r.name]; spelled {
			continue
		}
		if d.Mapping == nil {
			d.Mapping = map[string]ir.TypeID{}
		}
		d.Mapping[r.name] = variant
	}
	return nil
}

// implicitRoute is the route OpenAPI's implicit mapping takes to a union
// branch: the name of the component its $ref names, and that component's ID,
// where it names one.
type implicitRoute struct {
	name      string
	component ir.TypeID
	named     bool
}

// routesExactly reports whether r routes by the one string set holds, and so
// by all a variant admitting set admits.
func (r implicitRoute) routesExactly(set []string) bool {
	return r.named && len(set) == 1 && set[0] == r.name
}

// typesAreNamed reports whether each variant whose branch names a component,
// routes holding the route to each in order, is of that component's type, so
// that the name of its type is its route.
func typesAreNamed(routes []implicitRoute, variants []ir.Variant) bool {
	for i, r := range routes {
		if r.named && r.component != variants[i].Type.Target {
			return false
		}
	}
	return true
}

// implicitRoutes returns the implicit route to each branch of the union u
// that is a variant (unionAt.variantBranch), in branch order: one per variant.
// A branch written inline, or a $ref to any other position, names none.
func implicitRoutes(c lowering.Ctx, u unionAt) []implicitRoute {
	scope := c.RefScope()
	branches, _, _ := u.branches(c)
	routes := make([]implicitRoute, 0, len(branches))
	for i, b := range branches {
		st := c.Statements().Sub(b)
		if !u.variantBranch(c, i, st) {
			continue
		}
		name, id, ok := componentNamed(scope, referenceOf(c, st))
		routes = append(routes, implicitRoute{name: name, component: id, named: ok})
	}
	return routes
}

// besideUnionDiscriminator settles a discriminator a model holds beside a
// union kept verbatim. A model discriminator says the model is a hierarchy's
// base, so it stays only where every type it routes to is a subtype
// (ir.IsSubtype). Otherwise it moves, whole, to the model's Unmodeled, beside
// the union it was written for, a route to the model itself among them, which
// pass/validate takes from a concrete model (GitHub #1806).
//
// It is a settlement because only a lowered node says whether its type is a
// subtype, and a subtype lowered after the model has no Base yet when the
// model lowers.
type besideUnionDiscriminator struct {
	c       lowering.Ctx
	s       *statement.Schema
	pointer jsontext.Pointer
	model   ir.TypeID   // holds the discriminator and keeps the union verbatim
	named   []ir.TypeID // namedBranches
}

// Settle keeps the model's discriminator or moves it. A model holding none is
// left alone: it declared none, or it was settled already, by the settlement a
// reference deferred when it built the model before its declaration did.
//
// It moves only what it could copy (GitHub #144). One converting to no JSON,
// such as one writing an extension `.nan`, is reported as unpreservable and
// stays on the model rather than vanish, where pass/validate reports its
// routes.
//
// A move leaving a map swaps in its MapT (ir.AsMap), which homes all the model
// did; a later settlement leaves the map alone.
func (d besideUnionDiscriminator) Settle(_ context.Context, reg ir.TypeRegistry) []ir.Diagnostic {
	if mt, settled := reg[d.model].(*ir.MapT); settled && mt != nil {
		return nil
	}
	m, isModel := reg[d.model].(*ir.Model)
	if !isModel || m == nil {
		return []ir.Diagnostic{d.c.DiagAt(ir.SeverityError, diag.InternalInvariant, d.pointer,
			"internal: %q is no model, so the discriminator settlement deferred for it is not taken", d.model)}
	}
	if m.Discriminator == nil || routesToSubtypes(reg, m, d.named) {
		return nil
	}
	at := d.pointer + ids.Ptr("discriminator")
	kept, diags := annotation.PreserveSchemaKeyword(&m.Unmodeled, d.s, "discriminator",
		ir.ReasonDegradedLowering, d.pointer, d.c.Place())
	if !kept {
		return diags
	}
	promotion.DropContained(d.c.Promoter(), &m.Unmodeled, "openapi:discriminator")
	m.Discriminator = nil
	if mt, isMap := ir.AsMap(m); isMap {
		reg[d.model] = mt
	}
	return append(diags, d.c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
		"the discriminator routes to a type that is not a subtype of the model holding it, so it cannot "+
			"stay there; kept verbatim under Unmodeled beside the union it was written for"))
}

// routesToSubtypes reports whether every type m's discriminator routes to is a
// subtype of m: each mapping target, the default, and each of named. Each is
// one ir.IsSubtype walk rather than a question of an irfacts.Index, which
// indexes the whole registry: a settlement asks a few questions, of a registry
// the settlements before it may have rewritten. Walks are not shared between
// settlements (GitHub #1008).
func routesToSubtypes(reg ir.TypeRegistry, m *ir.Model, named []ir.TypeID) bool {
	doc := &ir.Document{Types: reg}
	routes := slices.AppendSeq(slices.Clone(named), maps.Values(m.Discriminator.Mapping))
	if m.Discriminator.Default != "" {
		routes = append(routes, m.Discriminator.Default)
	}
	for _, target := range routes {
		if !ir.IsSubtype(doc, target, m.ID) {
			return false
		}
	}
	return true
}
