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
	"github.com/dexpace/morphic/compilers/openapi/internal/componentlookup"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// lowerDiscriminator lowers discriminator, a discriminator the schema written
// at pointer declares, nil for none: its tag, located by exactly one of
// Property, PropertyName and Index (docs/ir/models.md#discriminator), and its
// routes, each mapping value and the defaultMapping read by
// discriminatorMapping. model is the model declaring it, whose property's
// PropertyID locates the tag, else PropertyName; a nil model, for a union,
// names it by PropertyName alone. branchIndexValue is a union's branches, nil
// for a model: a union routes to its variants (branchIndex.route), a model to
// the type each value's chain ends on (hierarchyRoute).
func lowerDiscriminator(loweringContext lowering.Ctx, ts *compile.Types, discriminator *statement.Discriminator, pointer jsontext.Pointer, model *ir.Model, branchIndexValue *branchIndex) (*ir.Discriminator, []ir.Diagnostic) {
	if discriminator == nil || !tagLocated(discriminator, model) {
		return nil, nil
	}
	mapping, diags := discriminatorMapping(loweringContext, ts, discriminator, branchIndexValue, pointer)
	disc := &ir.Discriminator{Mapping: mapping}
	if pid, ok := propertyIDByName(model, discriminator.PropertyName); ok {
		disc.Property = pid
	} else {
		disc.PropertyName = discriminator.PropertyName
	}
	defaultID, defaultDiags := discriminatorDefault(loweringContext, ts, discriminator, branchIndexValue, pointer)
	disc.Default = defaultID
	return disc, append(diags, defaultDiags...)
}

// tagLocated reports whether the IR can locate the tag discriminator names, on
// model where a model declares it: by any name but the empty one, which
// PropertyName empty cannot spell, and by that one only as the PropertyID of a
// property model declares itself (docs/compilers/openapi.md#discriminator). One
// it cannot locate is no node's, and the census keeps it verbatim.
func tagLocated(discriminator *statement.Discriminator, model *ir.Model) bool {
	if discriminator.PropertyName != "" {
		return true
	}
	_, ok := propertyIDByName(model, "")
	return ok
}

// propertyIDByName returns the PropertyID of the model property with the given
// source name, if the model declares it. model is nil-safe:
// lowerDiscriminator's oneOf/anyOf union case has no model to search, only a
// tag name.
func propertyIDByName(model *ir.Model, name string) (ir.PropertyID, bool) {
	if model == nil {
		return "", false
	}
	for i := range model.Properties {
		if model.Properties[i].Name.Source == name {
			return model.Properties[i].ID, true
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
// level, first, the schema schema's Base names, and its ancestors lead, so the
// Base's hierarchy tags it whichever entry is written first. Of several keys
// the smallest in byte order is elected (GitHub #410), with an info.
func subtypeDiscriminatorValue(loweringContext lowering.Ctx, schema *statement.Schema, id ir.TypeID, pointer jsontext.Pointer,
	first *statement.Schema,
) (subtypeTag, []ir.Diagnostic) {
	var lead []*statement.Schema
	if stated(first) {
		lead = append([]*statement.Schema{first}, discriminatedAncestors(loweringContext, first)...)
	}
	var ancestors []*statement.Schema
	for _, level := range ancestorLevels(loweringContext, schema) {
		slices.SortStableFunc(level, func(a, b *statement.Schema) int {
			return compareTrueFirst(slices.Contains(lead, a), slices.Contains(lead, b))
		})
		ancestors = append(ancestors, level...)
	}
	for _, a := range ancestors {
		tags := mappingTagsFor(loweringContext, a.Discriminator(), id)
		if len(tags) == 0 {
			continue
		}
		tag := subtypeTag{value: tags[0], routed: true}
		if len(tags) == 1 {
			return tag, nil
		}
		return tag, []ir.Diagnostic{loweringContext.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
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
		if routesByName(loweringContext, a, schema) {
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
// the component schema schema by its name, OpenAPI's implicit mapping. One
// standing with no oneOf or anyOf routes every schema composing a through
// allOf. One standing with either (standingUnion) routes the schemas that union
// lists, every possible one listed there
// (docs/compilers/openapi.md#discriminator-value): those a $ref branch of it
// ends on, as discriminatedAncestors reads a $ref, read once a run, as every
// subtype of a asks. Any other schema composing a narrows that union, each
// instance carrying its variant's tag, and composes it as written
// (docs/ir/models.md#a-union-a-composition-names).
func routesByName(loweringContext lowering.Ctx, a, schema *statement.Schema) bool {
	holder := standingUnion(loweringContext, a)
	if !stated(holder) {
		return true
	}
	branches, _, _ := unionBranches(loweringContext, a, holder)
	listed := lowering.ReadOncePerRun(loweringContext, listedBy{a}, func() map[*statement.Schema]bool {
		set := make(map[*statement.Schema]bool, len(branches))
		for _, b := range branches {
			if target := refBranchTarget(loweringContext, loweringContext.Statements().Subschema(b)); stated(target) {
				set[target] = true
			}
		}
		return set
	})
	return listed[schema]
}

// standingUnion returns the schema writing the union the discriminator a
// declares stands with: a where it writes one, else the branch of a's allOf
// whose union a lowers (findBranchUnion), which writes no discriminator of its
// own beside a's, and nil for none, a hierarchy's base.
func standingUnion(loweringContext lowering.Ctx, a *statement.Schema) *statement.Schema {
	if declaresUnion(a) {
		return a
	}
	at := branchUnionIndexOf(loweringContext, a)
	if !at.ok {
		return nil
	}
	return allOfBranches(loweringContext, a, "").branches[at.index].schema.Schema
}

// maxDiscriminatorAncestorDepth bounds how many composition levels
// discriminatedAncestors climbs (styleguide bounded-everything rule). The visited
// set below is what makes the walk terminate; this is the explicit limit, set far
// beyond any hierarchy a source document plausibly declares.
const maxDiscriminatorAncestorDepth = 256

// discriminatedAncestors returns schema's composition ancestors declaring a
// discriminator, nearest first: the resolved targets of schema's own $ref
// branches in source order, then theirs, level by level. Nearest means fewest
// hops, which decides between two ancestors whose mappings name the same
// subtype.
//
// The visited set bounds the work; the depth cap bounds only the levels. A
// schema reached by several branches would be walked once per path, so the
// frontier multiplies by the fan-out each level (2^level for a chain of
// diamonds). TestAllOf_DiscriminatorValueCyclicComposition (cyclic) and
// TestCompile_SharedCompositionAncestorsDoNotAmplify (acyclic) pin it.
func discriminatedAncestors(loweringContext lowering.Ctx, schema *statement.Schema) []*statement.Schema {
	return slices.Concat(ancestorLevels(loweringContext, schema)...)
}

// ancestorLevels is discriminatedAncestors level by level, nearest first.
func ancestorLevels(loweringContext lowering.Ctx, schema *statement.Schema) [][]*statement.Schema {
	var out [][]*statement.Schema
	visited := make(map[*statement.Schema]bool)
	// schema is visited before the walk starts, so a composition cycle cannot
	// bring it back as one of its own ancestors. Without this a schema in a
	// cycle that declares a discriminator answers to its own mapping, which is
	// a hierarchy of one thing standing above itself.
	visited[schema] = true
	level := []*statement.Schema{schema}
	for depth := 0; depth < maxDiscriminatorAncestorDepth && len(level) > 0; depth++ {
		next := make([]*statement.Schema, 0, len(level))
		for _, cur := range level {
			next = append(next, unvisitedRefTargets(loweringContext, cur, visited)...)
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

// composedByBranch reports whether a branch of unionAtValue, a union of
// schema's, the combinator unionBranches elects, composes schema through allOf
// at any depth, so that schema's discriminator routes to a subtype of schema:
// schema is then the base of a hierarchy, which a union of its subtypes would
// no longer be (docs/compilers/openapi.md#union-beside-structure). Such a
// branch finds schema's discriminator among its ancestors'
// (discriminatedAncestors). Where schema writes one, it is the one standing
// with unionAtValue (findBranchUnion). Every reader of the position asks, so it
// is read once a run.
func composedByBranch(loweringContext lowering.Ctx, schema *statement.Schema, unionAtValue unionAt) bool {
	discriminator := schema.Discriminator()
	if discriminator == nil {
		return false
	}
	return lowering.ReadOncePerRun(loweringContext, composedByOf{schema, unionAtValue.holder}, func() bool {
		branches, _, _ := unionAtValue.branches(loweringContext)
		return slices.ContainsFunc(branches, func(b statement.Subschema) bool {
			st := loweringContext.Statements().Subschema(b)
			target := st.Schema
			if st.Ref != nil {
				target = refBranchTarget(loweringContext, st)
			}
			return stated(target) && slices.ContainsFunc(discriminatedAncestors(loweringContext, target),
				func(a *statement.Schema) bool { return a.Discriminator() == discriminator })
		})
	})
}

// unvisitedRefTargets returns the schemas schema's $ref composition branches
// resolve to, in source order, skipping those already seen and marking those it
// returns. Marking as it enqueues rather than when the walk reaches them is
// what keeps a schema two branches both name from being queued, and walked,
// twice.
func unvisitedRefTargets(loweringContext lowering.Ctx, schema *statement.Schema, visited map[*statement.Schema]bool) []*statement.Schema {
	var out []*statement.Schema
	for b := range allOfRefBranches(loweringContext, schema) {
		target := refBranchTarget(loweringContext, b)
		if !stated(target) || visited[target] {
			continue
		}
		visited[target] = true
		out = append(out, target)
	}
	return out
}

// discriminatorBesideUnion returns the finisher queueing a settlement for the
// discriminator schema writes, which the model in hand holds beside the union
// holder writes, kept verbatim: beside the model by lowerBesideUnmodeledUnion,
// or with its allOf branch. A body that lowered to anything else holds none:
// the census kept its discriminator where it kept the rest, a multi-typed
// body's on its union (lowerTypeVariant).
func discriminatorBesideUnion(loweringContext lowering.Ctx, ts *compile.Types, schema, holder *statement.Schema, pointer jsontext.Pointer) finisher {
	return func(td ir.TypeDef, _ keySet, _ *principalKept) {
		if isKind(td, ir.KindModel) {
			ts.Defer(besideUnionDiscriminator{c: loweringContext, s: schema, pointer: pointer, model: td.Common().ID, named: namedBranches(loweringContext, schema, holder)})
		}
	}
}

// namedBranches returns the components named by $ref among the branches of
// the union holder elects at the position schema, in branch order: the types a
// discriminator routes to by name when no mapping key routes to them. An inline
// branch has no name and neither has any other position, so the discriminator
// reaches those only through its mapping, which the model's Discriminator
// already holds.
func namedBranches(loweringContext lowering.Ctx, schema, holder *statement.Schema) []ir.TypeID {
	scope := loweringContext.RefScope()
	branches, _, _ := unionBranches(loweringContext, schema, holder)
	var named []ir.TypeID
	for _, b := range branches {
		if _, id, ok := componentNamed(scope, referenceOf(loweringContext, loweringContext.Statements().Subschema(b))); ok {
			named = append(named, id)
		}
	}
	return named
}

// componentNamed returns the source component the union branch b names by
// $ref, by name and by ID: the route OpenAPI's implicit mapping takes to it. It
// is false for a branch naming none, one written inline or a $ref to any other
// position. b is the branch's reference (referenceOf), nil for none.
func componentNamed(scope componentlookup.Scope, b *statement.Statement) (string, ir.TypeID, bool) {
	if b == nil {
		return "", "", false
	}
	at, ok := landsAt(b)
	if !ok || at.Doc != reftable.Source {
		return "", "", false
	}
	id, state := scope.ComponentRef(at.Pointer)
	if state != ids.NamedComponent {
		return "", "", false
	}
	name, _ := ids.ComponentSchema(at.Pointer)
	return name, id, true
}

// spellImplicitRoutes writes into the discriminator of union, the Union at
// lowers, the route OpenAPI implies to each variant no mapping entry routes:
// the name of the component its $ref branch names, unless another branch names
// it too or an entry spells it. The IR routes by type name only where the
// Mapping is nil (docs/ir/models.md#discriminator), so these are written where
// it holds an entry, or a variant's type is not that component, as a composed
// variant's or an alias's is not. A non-null branch lacking its variant is a
// compiler bug, reported at pointer.
func spellImplicitRoutes(loweringContext lowering.Ctx, at unionAt, union *ir.Union, pointer jsontext.Pointer) []ir.Diagnostic {
	discriminator := union.Discriminator
	if discriminator == nil {
		return nil
	}
	routes := implicitRoutes(loweringContext, at)
	if len(routes) != len(union.Variants) {
		return []ir.Diagnostic{loweringContext.DiagAt(ir.SeverityError, diag.InternalInvariant, pointer,
			"internal: the union has %d variants for %d branches, so no route is written for them",
			len(union.Variants), len(routes))}
	}
	if len(discriminator.Mapping) == 0 && typesAreNamed(routes, union.Variants) {
		return nil
	}
	count := map[string]int{}
	for _, implicitRoute := range routes {
		if implicitRoute.named {
			count[implicitRoute.name]++
		}
	}
	routed := make(map[ir.TypeID]bool, len(discriminator.Mapping))
	for _, target := range discriminator.Mapping {
		routed[target] = true
	}
	for i, implicitRoute := range routes {
		variant := union.Variants[i].Type.Target
		// An unnamed route writes nothing. count skips one too, but only as it
		// counts named routes alone, so the guard says it where it is meant. A
		// routed variant's name stays out though OpenAPI routes by it: a writer
		// takes the variant's tag from the Mapping, which cannot mark a second
		// route as only read (docs/compilers/openapi.md#discriminator).
		if !implicitRoute.named || count[implicitRoute.name] != 1 || routed[variant] {
			continue
		}
		if _, spelled := discriminator.Mapping[implicitRoute.name]; spelled {
			continue
		}
		if discriminator.Mapping == nil {
			discriminator.Mapping = map[string]ir.TypeID{}
		}
		discriminator.Mapping[implicitRoute.name] = variant
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
	for i, implicitRoute := range routes {
		if implicitRoute.named && implicitRoute.component != variants[i].Type.Target {
			return false
		}
	}
	return true
}

// implicitRoutes returns the implicit route to each branch of the union
// unionAtValue that is a variant (unionAt.variantBranch), in branch order: one
// per variant. A branch written inline, or a $ref to any other position, names
// none.
func implicitRoutes(loweringContext lowering.Ctx, unionAtValue unionAt) []implicitRoute {
	scope := loweringContext.RefScope()
	branches, _, _ := unionAtValue.branches(loweringContext)
	routes := make([]implicitRoute, 0, len(branches))
	for i, b := range branches {
		st := loweringContext.Statements().Subschema(b)
		if !unionAtValue.variantBranch(loweringContext, i, st) {
			continue
		}
		name, id, ok := componentNamed(scope, referenceOf(loweringContext, st))
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
	model, isModel := reg[d.model].(*ir.Model)
	if !isModel || model == nil {
		return []ir.Diagnostic{d.c.DiagAt(ir.SeverityError, diag.InternalInvariant, d.pointer,
			"internal: %q is no model, so the discriminator settlement deferred for it is not taken", d.model)}
	}
	if model.Discriminator == nil || routesToSubtypes(reg, model, d.named) {
		return nil
	}
	at := d.pointer + ids.Ptr("discriminator")
	kept, diags := annotation.PreserveSchemaKeyword(&model.Unmodeled, d.s, "discriminator",
		ir.ReasonDegradedLowering, d.pointer, d.c.Place())
	if !kept {
		return diags
	}
	promotion.DropContained(d.c.Promoter(), &model.Unmodeled, "openapi:discriminator")
	model.Discriminator = nil
	if mt, isMap := ir.AsMap(model); isMap {
		reg[d.model] = mt
	}
	return append(diags, d.c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
		"the discriminator routes to a type that is not a subtype of the model holding it, so it cannot "+
			"stay there; kept verbatim under Unmodeled beside the union it was written for"))
}

// routesToSubtypes reports whether every type model's discriminator routes to
// is a subtype of model: each mapping target, the default, and each of named.
// Each is one ir.IsSubtype walk rather than a question of an
// irfacts.DocumentFacts, which indexes the whole registry: a settlement asks a
// few questions, of a registry the settlements before it may have rewritten.
// Walks are not shared between settlements (GitHub #1008).
func routesToSubtypes(reg ir.TypeRegistry, model *ir.Model, named []ir.TypeID) bool {
	doc := &ir.Document{Types: reg}
	routes := slices.AppendSeq(slices.Clone(named), maps.Values(model.Discriminator.Mapping))
	if model.Discriminator.Default != "" {
		routes = append(routes, model.Discriminator.Default)
	}
	for _, target := range routes {
		if !ir.IsSubtype(doc, target, model.ID) {
			return false
		}
	}
	return true
}
