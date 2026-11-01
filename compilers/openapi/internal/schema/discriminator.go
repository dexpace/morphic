// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"maps"
	"slices"
	"strings"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/ir"
)

// lowerDiscriminator lowers a schema's discriminator. Each mapping entry
// resolves to a target TypeID (a bare name implies
// #/components/schemas/<name>; nil stays nil, preserving infer-by-name
// semantics), and the tag is located via exactly one of Property /
// PropertyName / Index (docs/ir/models.md#discriminator). m is the model
// declaring it, a plain object or an allOf composition, whose declaring
// property's PropID locates the tag, else PropertyName if undeclared; a nil m,
// for a oneOf/anyOf union with no single model, names it by PropertyName alone.
// variants are a union's, and nil for a model: what each names decides what the
// mapping names (readsThrough).
func lowerDiscriminator(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, m *ir.Model, variants []ir.Variant, pointer jsontext.Pointer) (*ir.Discriminator, []ir.Diagnostic) {
	d := s.GetDiscriminator()
	if d == nil {
		return nil, nil
	}
	route := mappingRoute{variants: variantTargets(variants), referents: referencedVariants(c, ts, s, variants)}
	mapping, diags := discriminatorMapping(c, ts, d, route, pointer)
	disc := &ir.Discriminator{Mapping: mapping}
	if pid, ok := propIDByName(m, d.GetPropertyName()); ok {
		disc.Property = pid
	} else {
		disc.PropertyName = d.GetPropertyName()
	}
	defaultID, defaultDiags := discriminatorDefault(c, ts, d, route, pointer)
	disc.Default = defaultID
	return disc, append(diags, defaultDiags...)
}

// variantTargets returns the set of types the variants name: the types a
// union's discriminator may route to, as pass.Validate holds its mapping. It is
// nil for none, which is a model's.
func variantTargets(variants []ir.Variant) map[ir.TypeID]bool {
	if len(variants) == 0 {
		return nil
	}
	targets := make(map[ir.TypeID]bool, len(variants))
	for _, v := range variants {
		targets[v.Type.Target] = true
	}
	return targets
}

// mappingRoute is what a union's variants tell a mapping target: the types they
// name, which readsThrough reads, and the one variant standing for each type a
// branch's $ref names (referencedVariants). Both are nil for a model.
type mappingRoute struct {
	variants  map[ir.TypeID]bool
	referents map[ir.TypeID]ir.TypeID
}

// routed is the type a mapping target resolving to id routes to: the variant
// whose branch names id, which is an alias over it where a keyword is written
// beside the branch's $ref, so the mapping names a variant, as pass.Validate
// holds it and an emitter routes by it (GitHub #847).
func (r mappingRoute) routed(id ir.TypeID) ir.TypeID {
	if variant, ok := r.referents[id]; ok {
		return variant
	}
	return id
}

// referencedVariants maps each type a branch's $ref names to its variant, for a
// union's variants, built in branch order past the null branches. A type two
// branches name maps to neither: a mapping naming it says nothing of which, so
// it stays on the type, which is itself a variant unless both alias it, and
// that pass.Validate reports. It reads the branches rather than the registry,
// so the answer turns on no lowering order.
func referencedVariants(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, variants []ir.Variant) map[ir.TypeID]ir.TypeID {
	branches, _, _ := unionBranches(s)
	branches = slices.DeleteFunc(slices.Clone(branches), isNullSchema)
	if len(branches) != len(variants) {
		return nil // a model's, which has no variants
	}
	referents := map[ir.TypeID]ir.TypeID{}
	shared := map[ir.TypeID]bool{}
	for i, b := range branches {
		named, ok := resolveSchemaRef(c, ts, b) // false for one written inline
		if !ok {
			continue
		}
		_, twice := referents[named]
		shared[named] = shared[named] || twice
		referents[named] = variants[i].Type.Target
	}
	maps.DeleteFunc(referents, func(named, _ ir.TypeID) bool { return shared[named] })
	return referents
}

// discriminatorMapping resolves a discriminator's wire-value-to-schema mapping
// into TypeIDs, in source order. An entry that does not resolve yields one
// error diagnostic and is dropped — never a synthesized ID that nothing backs
// (issue #14). An all-dropped mapping collapses to nil, preserving infer-by-name
// semantics and a clean round-trip.
func discriminatorMapping(c lowering.Ctx, ts *compile.Types, d *oas3.Discriminator, route mappingRoute, pointer jsontext.Pointer) (map[string]ir.TypeID, []ir.Diagnostic) {
	m := d.GetMapping()
	if m == nil || m.Len() == 0 {
		return nil, nil
	}
	var diags []ir.Diagnostic
	out := make(map[string]ir.TypeID, m.Len())
	for tag, target := range m.All() {
		id, ok := resolveMappingTarget(c, ts, d, target, route)
		if !ok {
			diags = append(diags, c.DiagAt(ir.SeverityError, diag.UnresolvedRef,
				pointer+ids.Ptr("discriminator", "mapping", tag),
				"discriminator mapping %q references unresolved schema %q%s", tag, target,
				mappingWhy(c, d, target)))
			continue
		}
		out[tag] = id
	}
	if len(out) == 0 {
		return nil, diags
	}
	return out, diags
}

// discriminatorDefault resolves an OpenAPI 3.2 defaultMapping to its target ID,
// dropping it with one diagnostic when it does not resolve, as
// discriminatorMapping drops an entry.
func discriminatorDefault(c lowering.Ctx, ts *compile.Types, d *oas3.Discriminator, route mappingRoute, pointer jsontext.Pointer) (ir.TypeID, []ir.Diagnostic) {
	dm := d.GetDefaultMapping()
	if dm == "" {
		return "", nil
	}
	id, ok := resolveMappingTarget(c, ts, d, dm, route)
	if !ok {
		return "", []ir.Diagnostic{c.DiagAt(ir.SeverityError, diag.UnresolvedRef,
			pointer+ids.Ptr("discriminator", "defaultMapping"),
			"discriminator defaultMapping references unresolved schema %q%s", dm,
			mappingWhy(c, d, dm))}
	}
	return id, nil
}

// mappingWhy is the reason target, a value of d's mapping, reports when it
// resolves to no type: why it lands on no schema, or why the table never read
// it, as unresolvedWhy says for a $ref.
// A value with no '#' names a component, which the table never reads
// (bind.Table.Mapping), so neither its budget nor the merge bound is why.
func mappingWhy(c lowering.Ctx, d *oas3.Discriminator, target string) string {
	m, ok := c.Bindings().Mapping(d, target)
	switch {
	case ok:
		return hopWhy(m.Hop)
	case !strings.Contains(target, "#"):
		return ""
	}
	return unreadWhy(c.Bindings(), d)
}

// resolveMappingTarget resolves a mapping target, a mapping entry's or a
// defaultMapping's, to the ID of the schema it names: a declared component's
// bare name directly, and a reference as resolvePointer resolves a $ref at the
// position whose type it names (typePosition), promising the schema there.
//
// The parser never reads a mapping value
// (docs/parser-library.md#mapping-unresolved); the compiler reads one as a
// $ref written at the discriminator (resolve.Scope.MappingTarget). Hoisting
// what it lands on gives the target one pointer-derived ID in either
// declaration order (GitHub #530). The answer is routed to the variant standing
// for it (mappingRoute).
func resolveMappingTarget(c lowering.Ctx, ts *compile.Types, d *oas3.Discriminator, target string, route mappingRoute) (ir.TypeID, bool) {
	if c.DeclaresSchema(target) {
		return route.routed(componentIDByName(target)), true
	}
	at, decl, ok := c.RefScope().MappingTarget(d, target)
	if !ok {
		return "", false
	}
	at, decl = typePosition(c, at, decl, route.variants)
	id, ok := resolveAt(c, ts, at, decl)
	return route.routed(id), ok
}

// maxTypePositionHops bounds how many $ref positions typePosition reads
// through (styleguide bounded-everything rule). A loop of them, and any longer
// chain, stops at the position the bound reached, so the mapping names that
// position's alias.
const maxTypePositionHops = 64

// typePosition returns the position, in whichever document, whose type a
// mapping target naming at names, and the schema held there, starting from
// decl, unread at a source component. It is at unless the position reads
// through (readsThrough): a $ref only uses its target's type, so it names the
// position the $ref does, as a union variant there does (GitHub #758). A $ref
// landing on nothing a type is read at stops it, and its own position, lowered
// as an alias that reports why, is named.
func typePosition(c lowering.Ctx, at bind.Position, decl *oas3.JSONSchema[oas3.Referenceable],
	variantTypes map[ir.TypeID]bool,
) (bind.Position, *oas3.JSONSchema[oas3.Referenceable]) {
	if sourceComponent(at) {
		// Most targets name a component, and resolve.Scope.ComponentRef answers
		// for one by name, so nothing is fetched for it (see mappingTagsFor).
		return at, nil
	}
	scope := c.RefScope()
	for range maxTypePositionHops {
		if !readsThrough(c, at, decl, variantTypes) {
			break
		}
		next, ok := scope.Target(decl)
		target := scope.Bindings.Declared(decl)
		if !ok || target == nil && !sourceComponent(next) {
			break
		}
		at, decl = next, target
	}
	return at, decl
}

// sourceComponent reports whether at is one of the source's components, which
// is named by name rather than fetched where it is declared.
func sourceComponent(at bind.Position) bool {
	_, named := ids.ComponentSchemaName(at.Pointer)
	return named && at.Doc == bind.Source
}

// readsThrough reports whether a mapping target naming at, where decl is
// declared, names the position decl's $ref names instead (typePosition): not at
// a component, whose node is a type of its own.
//
// Nor where the node at pointer is in variantTypes, the types a union's
// variants name, nil for a model: that variant is a $ref to the position, and
// the mapping must name what the variant names (GitHub #758). Any other
// position is no variant or subtype, whatever is written beside its $ref, so
// the mapping means what the $ref names (GitHub #764).
func readsThrough(c lowering.Ctx, at bind.Position, decl *oas3.JSONSchema[oas3.Referenceable],
	variantTypes map[ir.TypeID]bool,
) bool {
	_, named := ids.ComponentSchemaName(at.Pointer)
	if named || ids.ComponentSchemaNamedEmpty(at.Pointer) || decl == nil {
		return false
	}
	if len(variantTypes) == 0 {
		return true
	}
	in, ok := lowering.InDocument(c, at.Doc)
	return !ok || !variantTypes[in.IDs().ForPointer(at.Pointer)]
}

// componentIDByName is the ID a mapping target naming a declared component
// resolves to: its pointer's in the source, whose components a bare name names,
// so a component keyed "" gets the anonymous ID it is interned under rather
// than a named one nothing backs.
func componentIDByName(name string) ir.TypeID {
	return ids.Document{}.ForPointer(ids.Ptr("components", "schemas", name))
}

// mappingTargetID resolves a discriminator mapping target, a bare schema name
// or a $ref string, to the TypeID of the schema it names. A bare name (even one
// containing '/') naming a declared component resolves in any source order,
// since every declared name is recorded before lowering begins. Otherwise the
// target must be a $ref, read at the position whose type it names
// (typePosition), in whichever document: a source component, or any other
// position by the ID its pointer derives there, whose node resolveMappingTarget
// promises. It reads no registry, so no answer turns on which schema lowered
// first. scope is c's.
func mappingTargetID(c lowering.Ctx, scope resolve.Scope, d *oas3.Discriminator, target string) (ir.TypeID, bool) {
	if scope.Declares(target) {
		return componentIDByName(target), true
	}
	named, decl, ok := scope.MappingTarget(d, target)
	if !ok {
		return "", false
	}
	// No variant types: a tag asks whether the mapping reaches the subtype, so
	// it reads through whatever a union's variants name (readsThrough). Each
	// subtype asks it of every entry, so it is read once (Ctx.TypePosition).
	at := c.TypePosition(named, func() bind.Position {
		position, _ := typePosition(c, named, decl, nil)
		return position
	})
	in, ok := lowering.InDocument(c, at.Doc)
	if !ok {
		return "", false
	}
	if in.IDs() == (ids.Document{}) {
		if id, resolved, handled := scope.ComponentRef(at.Pointer); handled {
			return id, resolved
		}
	}
	return in.IDs().ForPointer(at.Pointer), true
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

// subtypeDiscriminatorValue returns the wire tag this allOf subtype carries in
// its discriminator hierarchy, or "" when no ancestor anchors one
// (docs/ir/models.md): the mapping key naming the subtype, else its schema name
// as declared, not as its escaped pointer token spells it (GitHub #505). An
// inline subtype has no name, so only a mapping entry can tag it (GitHub #517).
//
// Every discriminated ancestor is asked, since an intermediate may declare none
// (GitHub #305). Of several mapping keys for the subtype the smallest in byte
// order is elected, as a mapping is unordered (GitHub #410); the base's
// Discriminator keeps them all, and an info diagnostic reports it.
func subtypeDiscriminatorValue(c lowering.Ctx, s *oas3.Schema, id ir.TypeID, pointer jsontext.Pointer) (string, []ir.Diagnostic) {
	ds := ancestorDiscriminators(c.Bindings(), s)
	if len(ds) == 0 {
		return "", nil
	}
	for _, d := range ds {
		tags := mappingTagsFor(c, d, id)
		if len(tags) == 0 {
			continue
		}
		if len(tags) == 1 {
			return tags[0], nil
		}
		return tags[0], []ir.Diagnostic{c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
			"the discriminator mapping names this schema under %d keys (%s); "+
				"discriminatorValue holds the smallest in byte order, and the base's mapping keeps them all",
			len(tags), strings.Join(tags, ", "))}
	}
	// Read ok, not only the name: for a top-level entry of another kind, such as
	// components/x-shared/Cat, ComponentSchemaName still returns a name, and it
	// is no component schema's (GitHub #572).
	if name, ok := ids.ComponentSchemaName(pointer); ok {
		return name, nil
	}
	return "", nil
}

// mappingTagsFor returns every key d's mapping spells for the type id, sorted
// in byte order, or nothing when the mapping does not name it. An empty result
// is what sends the caller to the implicit name, and it is distinct from a
// mapping that names id under the empty key, which returns that one key.
//
// Sorted rather than in source order because the caller elects the first, and
// a mapping's key order is not a property of the document it is written in.
func mappingTagsFor(c lowering.Ctx, d *oas3.Discriminator, id ir.TypeID) []string {
	m := d.GetMapping()
	if m == nil {
		return nil
	}
	// Derived once, not per entry: every subtype reads its base's whole mapping,
	// so n subtypes make n² lookups, and each derivation allocates.
	scope := c.RefScope()
	var tags []string
	for tag, target := range m.All() {
		if tid, ok := mappingTargetID(c, scope, d, target); ok && tid == id {
			tags = append(tags, tag)
		}
	}
	slices.Sort(tags)
	return tags
}

// maxDiscriminatorAncestorDepth bounds how many composition levels
// ancestorDiscriminators climbs (styleguide bounded-everything rule). The visited
// set below is what makes the walk terminate; this is the explicit limit, set far
// beyond any hierarchy a source document plausibly declares.
const maxDiscriminatorAncestorDepth = 256

// ancestorDiscriminators returns the discriminators declared on s's composition
// ancestors, nearest first: the resolved targets of s's own $ref branches in
// source order, then theirs, level by level. Nearest means fewest hops, which
// decides between two ancestors whose mappings name the same subtype.
//
// The visited set bounds the work; the depth cap bounds only the levels. A
// schema reached by several branches would be walked once per path, so the
// frontier multiplies by the fan-out each level (2^level for a chain of
// diamonds). TestAllOf_DiscriminatorValueCyclicComposition (cyclic) and
// TestCompile_SharedCompositionAncestorsDoNotAmplify (acyclic) pin it.
func ancestorDiscriminators(refs *typed.Bindings, s *oas3.Schema) []*oas3.Discriminator {
	var out []*oas3.Discriminator
	visited := make(map[*oas3.Schema]bool)
	// s is visited before the walk starts, so a composition cycle cannot bring it
	// back as one of its own ancestors. Without this a schema in a cycle that
	// declares a discriminator answers to its own mapping, which is a hierarchy of
	// one thing standing above itself.
	visited[s] = true
	level := []*oas3.Schema{s}
	for depth := 0; depth < maxDiscriminatorAncestorDepth && len(level) > 0; depth++ {
		next := make([]*oas3.Schema, 0, len(level))
		for _, cur := range level {
			next = append(next, unvisitedRefTargets(refs, cur, visited)...)
		}
		for _, target := range next {
			if d := target.GetDiscriminator(); d != nil {
				out = append(out, d)
			}
		}
		level = next
	}
	return out
}

// unvisitedRefTargets returns the schemas s's $ref composition branches resolve
// to, in source order, skipping those already seen and marking those it returns.
// Marking as it enqueues rather than when the walk reaches them is what keeps a
// schema two branches both name from being queued, and walked, twice.
func unvisitedRefTargets(refs *typed.Bindings, s *oas3.Schema, visited map[*oas3.Schema]bool) []*oas3.Schema {
	var out []*oas3.Schema
	for b := range allOfRefBranches(s) {
		target := refBranchTarget(refs, b)
		if target == nil || visited[target] {
			continue
		}
		visited[target] = true
		out = append(out, target)
	}
	return out
}

// deferDiscriminatorBesideUnion queues a settlement for each model that holds
// the discriminator written beside the union lowerBesideUnmodeledUnion kept
// verbatim on owner (discriminatorModels). A body that lowered to anything else
// holds none: the census kept its discriminator where it kept the rest.
func deferDiscriminatorBesideUnion(c lowering.Ctx, ts *compile.Types, owner ir.TypeID, s *oas3.Schema, pointer jsontext.Pointer) {
	named := namedBranches(c, s)
	for _, model := range discriminatorModels(ts, owner) {
		ts.Defer(besideUnionDiscriminator{c: c, s: s, pointer: pointer, owner: owner, model: model, named: named})
	}
}

// discriminatorModels returns the models holding the discriminator of the body
// lowered to owner: owner itself when it is a model, and otherwise each model
// variant of the union a multi-typed body lowers to, which lowerUnion builds
// from the same schema. lowerDiscriminator fills no other node a body yields.
func discriminatorModels(ts *compile.Types, owner ir.TypeID) []ir.TypeID {
	td, _ := ts.Node(owner)
	switch n := td.(type) {
	case *ir.Model:
		return []ir.TypeID{owner}
	case *ir.Union:
		var models []ir.TypeID
		for _, v := range n.Variants {
			if vt, _ := ts.Node(v.Type.Target); isKind(vt, ir.KindModel) {
				models = append(models, v.Type.Target)
			}
		}
		return models
	default:
		return nil
	}
}

// namedBranches returns the components s's union branches name by $ref, in
// branch order: the types a discriminator routes to by name when no mapping
// key routes to them. An inline branch has no name and neither has any other
// position, so the discriminator reaches those only through its mapping, which
// the model's Discriminator already holds.
func namedBranches(c lowering.Ctx, s *oas3.Schema) []ir.TypeID {
	scope := c.RefScope()
	branches, _, _ := unionBranches(s)
	var named []ir.TypeID
	for _, b := range branches {
		at, ok := scope.Target(b)
		if !ok || at.Doc != bind.Source {
			continue
		}
		if id, declared, component := scope.ComponentRef(at.Pointer); component && declared {
			named = append(named, id)
		}
	}
	return named
}

// besideUnionDiscriminator settles a discriminator a model holds beside a
// union kept verbatim. A model discriminator says the model is a hierarchy's
// base, so it stays only where every type it routes to is a subtype, the rule
// pass/validate holds it to (ir.IsSubtype). Otherwise it moves, whole, to
// Unmodeled on the node keeping the union it was written for.
//
// It is a settlement because only a lowered node says whether its type is a
// subtype, and a subtype lowered after the model has no Base yet when the
// model lowers.
type besideUnionDiscriminator struct {
	c       lowering.Ctx
	s       *oas3.Schema
	pointer jsontext.Pointer
	owner   ir.TypeID   // keeps the union verbatim
	model   ir.TypeID   // holds the discriminator: owner, or a variant of it
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
func (d besideUnionDiscriminator) Settle(reg ir.TypeRegistry) []ir.Diagnostic {
	owner := reg[d.owner]
	if mt, settled := reg[d.model].(*ir.MapT); settled && mt != nil && !ir.IsNilTypeDef(owner) {
		return nil
	}
	m, isModel := reg[d.model].(*ir.Model)
	if !isModel || m == nil || ir.IsNilTypeDef(owner) {
		return []ir.Diagnostic{d.c.DiagAt(ir.SeverityError, diag.InternalInvariant, d.pointer,
			"internal: %q is no model or %q is not registered, so the discriminator settlement deferred for them is not taken",
			d.model, d.owner)}
	}
	if m.Discriminator == nil || routesToSubtypes(reg, m, d.named) {
		return nil
	}
	at := d.pointer + ids.Ptr("discriminator")
	kept, diags := annotation.PreserveSchemaKeyword(&owner.Common().Unmodeled, d.s, "discriminator",
		ir.ReasonDegradedLowering, d.pointer, d.c.ProvenanceAt)
	if !kept {
		return diags
	}
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
