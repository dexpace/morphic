package schema

import (
	"cmp"
	"encoding/base64"
	"encoding/json/jsontext"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/values"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/merge"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// requireMembers lands each name of required on m, once m holds every own
// property: as Required on the property of that wire name, never cleared once
// set, and otherwise in m.Requires, once, in the order required names it. A
// name no own property declares is one a Base or Mixin declares, or one only
// the catch-all governs, and JSON Schema requires it all the same (ir-design
// §4.3). Each object lowering calls it once, on a model that lists none yet.
func requireMembers(m *ir.Model, required []string) {
	byWire := merge.WireNameIndex(m.Properties)
	listed := make(map[string]bool, len(required))
	for _, name := range required {
		if i, ok := byWire[name]; ok {
			m.Properties[i].Required = true
			continue
		}
		if !listed[name] {
			listed[name] = true
			m.Requires = append(m.Requires, name)
		}
	}
}

// onLoop reports whether the $ref at b lies on a loop of $refs, which the load
// phase reports once and the lowering drops to any without a report of its
// own.
func onLoop(c lowering.Ctx, b *oas3.JSONSchema[oas3.Referenceable]) bool {
	e, ok := c.Bindings().Entry(b)
	return ok && e.Chain.Member
}

// subtypeDiscriminatorValue returns the wire tag this allOf subtype carries in
// its discriminator hierarchy, or "" when no ancestor anchors one (ir-design
// §4.3): the mapping key naming the subtype, else its schema name as declared,
// not as its escaped pointer token spells it (GitHub #505). An inline subtype
// has no name, so only a mapping entry can tag it (GitHub #517).
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

// branchHint derives a composition branch's naming hint from its $ref target's
// name, falling back to a positional hint for inline branches. It names both a
// union variant and an allOf branch, which reach it as the same question.
//
// A $ref branch's answer is also subSchemaHint's, and must stay so: an outside
// $ref can name the branch pointer, and hoistSubSchema names it by
// subSchemaHint where no lowering of the composition recorded this hint, so
// the branch takes one name whether or not its composition is lowered.
func branchHint(refs *typed.Bindings, b *oas3.JSONSchema[oas3.Referenceable], i int) string {
	return branchHintAt(refs, b, strconv.Itoa(i))
}

// branchHintAt is branchHint for a branch whose index is already a token, as the
// last one of its pointer is.
func branchHintAt(refs *typed.Bindings, b *oas3.JSONSchema[oas3.Referenceable], index string) string {
	// Only a true reference carries a target name: IsReference() is precisely
	// GetSchema().Ref != "" for a non-bool branch, so a schema whose Ref pointer is
	// set but empty (IsReference() false) suggests no name.
	if b != nil && b.IsReference() {
		if name := targetHint(refs, b); name != "" {
			return name
		}
	}
	return positionalBranchHint(index)
}

// compositionKeywords are the schema keywords whose numbered children are
// composition branches.
var compositionKeywords = map[string]bool{"allOf": true, "oneOf": true, "anyOf": true}

// positionalBranchHint names an inline composition branch by its position, which
// is all there is to name it by: it has no $ref target to take a name from.
func positionalBranchHint(index string) string { return "variant_" + index }

// isDecimalIndex reports whether s is a non-empty run of ASCII digits — the
// shape a composition branch's pointer segment takes.
func isDecimalIndex(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// maxTargetHintHops bounds targetHint's walk along composition branches that
// hold a $ref: a loop of them, and any longer chain, stops there. An acyclic
// chain longer than the cap is legal, though no real document writes one: past
// it the walk keeps the name it has reached, so a union variant naming the
// chain's first branch can be named apart from the node it holds.
const maxTargetHintHops = 64

// targetHint returns the hint of the node a $ref resolves to, so a union
// variant or allOf branch is named as the type it holds is (GitHub #521).
//
// Each position on the way is read as subSchemaHint names it (ownHint); a
// composition branch holding a $ref moves the walk on to its target, keeping
// the last name found. The fragment is percent-decoded, then RFC 6901
// unescaped, so `#/components/schemas/Cat~1Dog` suggests "Cat/Dog" (GitHub
// #505); one spelling no pointer ends the walk with refHint's name. A position
// under /paths keeps ownHint's guess, which the declaration replaces on the
// node (GitHub #729).
func targetHint(refs *typed.Bindings, b *oas3.JSONSchema[oas3.Referenceable]) string {
	hint := ""
	for range maxTargetHintHops {
		ref := b.GetRef().String()
		pointer, ok := resolve.FragmentPointer(ref)
		if !ok {
			return cmp.Or(refHint(ref), hint)
		}
		decl := refs.Declared(b)
		own, follow := ownHint(decl, pointer)
		hint = cmp.Or(own, hint)
		if !follow {
			return hint
		}
		b = decl
	}
	return hint
}

// refHint returns the name a reference suggests when its fragment spells no
// pointer — a whole-document URI, a $anchor, a fragment that decodes to bytes
// that are not UTF-8 (GitHub #520): what follows its last '/', percent-decoded
// when that decodes to UTF-8, so `./Fish%2DTank.yaml` suggests "Fish-Tank.yaml".
func refHint(ref string) string {
	name := ref
	if _, after, ok := strings.CutLast(ref, "/"); ok {
		name = after
	}
	if decoded, err := url.PathUnescape(name); err == nil && utf8.ValidString(decoded) {
		return decoded
	}
	return name
}

// lowerDiscriminator lowers a schema's discriminator. Each mapping entry
// resolves to a target TypeID (a bare name implies
// #/components/schemas/<name>; nil stays nil, preserving infer-by-name
// semantics), and the tag is located via exactly one of Property /
// PropertyName / Index (ir-design §4.3). m is the allOf base model this
// discriminator is declared on, or nil for a oneOf/anyOf union, which has no
// single model and so is named only by PropertyName; otherwise the tag
// resolves to the declaring property's PropID, falling back to PropertyName
// if undeclared. variants are a union's, and nil for a model: what each names
// decides what the mapping names (readsThrough).
func lowerDiscriminator(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, s *oas3.Schema, m *ir.Model, variants []ir.Variant, pointer jsontext.Pointer) (*ir.Discriminator, []ir.Diagnostic) {
	d := s.GetDiscriminator()
	if d == nil {
		return nil, nil
	}
	variantTypes := variantTargets(variants)
	mapping, diags := discriminatorMapping(c, ts, anchors, d, variantTypes, pointer)
	disc := &ir.Discriminator{Mapping: mapping}
	if pid, ok := propIDByName(m, d.GetPropertyName()); ok {
		disc.Property = pid
	} else {
		disc.PropertyName = d.GetPropertyName()
	}
	defaultID, defaultDiags := discriminatorDefault(c, ts, anchors, d, variantTypes, pointer)
	disc.Default = defaultID
	return disc, append(diags, defaultDiags...)
}

// discriminatorMapping resolves a discriminator's wire-value-to-schema mapping
// into TypeIDs, in source order. An entry that does not resolve yields one
// error diagnostic and is dropped — never a synthesized ID that nothing backs
// (issue #14). An all-dropped mapping collapses to nil, preserving infer-by-name
// semantics and a clean round-trip.
func discriminatorMapping(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, d *oas3.Discriminator, variantTypes map[ir.TypeID]bool, pointer jsontext.Pointer) (map[string]ir.TypeID, []ir.Diagnostic) {
	m := d.GetMapping()
	if m == nil || m.Len() == 0 {
		return nil, nil
	}
	var diags []ir.Diagnostic
	out := make(map[string]ir.TypeID, m.Len())
	for tag, target := range m.All() {
		id, ok := resolveMappingTarget(c, ts, anchors, d, target, variantTypes)
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
func discriminatorDefault(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, d *oas3.Discriminator, variantTypes map[ir.TypeID]bool, pointer jsontext.Pointer) (ir.TypeID, []ir.Diagnostic) {
	dm := d.GetDefaultMapping()
	if dm == "" {
		return "", nil
	}
	id, ok := resolveMappingTarget(c, ts, anchors, d, dm, variantTypes)
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
// declaration order (GitHub #530). variantTypes is as readsThrough takes it.
func resolveMappingTarget(c lowering.Ctx, ts *compile.Types, anchors *AnchorIndex, d *oas3.Discriminator, target string, variantTypes map[ir.TypeID]bool) (ir.TypeID, bool) {
	if c.DeclaresSchema(target) {
		return componentIDByName(target), true
	}
	at, decl, ok := c.RefScope().MappingTarget(d, target)
	if !ok {
		return "", false
	}
	at, decl = typePosition(c, at, decl, variantTypes)
	return resolveAt(c, ts, anchors, at, decl)
}

// maxTypePositionHops bounds how many $ref positions typePosition reads
// through (styleguide bounded-everything rule). A loop of them, and any longer
// chain, stops at the position the bound reached, so the mapping names that
// position's alias.
const maxTypePositionHops = 64

// typePosition returns the position, in whichever document, whose type a
// mapping target naming at names, and the schema held there, starting from
// decl, unread at a source component. It is at unless the position reads
// through (readsThrough): a $ref holding nothing an alias would only uses its
// target's type, so it names the position the $ref does, as a union variant
// there does (GitHub #758). A keyword beside a property's $ref stops it
// (GitHub #764), and so does a $ref landing on nothing a type is read at,
// whose own position, lowered as an alias that reports why, is named.
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
// a component, nor where a keyword beside the $ref gives the position an alias
// (refSiteHomesNothing).
//
// Nor where the node at pointer is in variantTypes, the types a union's
// variants name, nil for a model. That variant is a $ref to the position, which
// hoisted an alias there as a $ref to any position does, and the mapping must
// name what the variant names. Read through, it named a type no variant names
// (GitHub #758).
func readsThrough(c lowering.Ctx, at bind.Position, decl *oas3.JSONSchema[oas3.Referenceable],
	variantTypes map[ir.TypeID]bool,
) bool {
	_, named := ids.ComponentSchemaName(at.Pointer)
	if named || ids.ComponentSchemaNamedEmpty(at.Pointer) || decl == nil {
		return false
	}
	s := decl.GetSchema()
	if !resolve.IsRefSite(decl, s) || !refSiteHomesNothing(s) {
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

// lowerEnum hoists a schema with `enum` as a closed Enum. A heterogeneous or
// non-scalar member set becomes a Union of Literals over the members, `null`
// included, with an info diagnostic; an empty list takes emptyEnum.
//
// A member count past the budget lowers as the top type with an error
// diagnostic, checked first because both paths cost per member, amplifying a
// small source (GitHub #75).
//
// A listed `null` member is stripped onto the reference's Nullable bit
// (ir-design §3.3) when schemaAdmitsNull holds, the predicate every Nullable
// site shares. A sibling `type: string` forbids that null, so it stays and the
// set becomes a Union.
func lowerEnum(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, pointer jsontext.Pointer, hint string) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		// The degenerate list first, then the budget. They cannot both hold — a
		// member count of zero exceeds no positive budget — so the order decides
		// nothing, but reading the empty case beside the members it lacks is
		// clearer than reading it after a bound on how many there may be.
		if len(s.GetEnum()) == 0 {
			def, emptyDiags := emptyEnum(c, s, common, pointer)
			diags = append(diags, emptyDiags...)
			return def
		}
		if n := len(s.GetEnum()); c.Limits.EnumMembersExceeded(n) {
			diags = append(diags, c.DiagAt(ir.SeverityError, diag.BudgetExceeded, pointer,
				"enum declares %d members, past the %d-member budget; lowered as any",
				n, c.Limits.MaxEnumMembers))
			return &ir.Any{TypeCommon: common}
		}
		diags = append(diags, enumRepeats(c, s.GetEnum(), pointer)...)
		members, memberPrim, ok := enumMembers(s.GetEnum(), schemaAdmitsNull(c, s), c.IDs(), pointer)
		if !ok {
			def, enumDiags := enumAsUnion(c, ts, s, common, pointer, hint)
			diags = append(diags, enumDiags...)
			return def
		}
		return &ir.Enum{
			TypeCommon: common,
			ValueType:  enumValueType(s, memberPrim),
			Members:    members,
			Closed:     true,
		}
	})
	return id, diags
}

// emptyEnum lowers `enum: []`, a value space that accepts no instance, as a
// closed Enum with no member and one warning. The IR has no bottom type, but a
// closed Enum admits only its members, so this is exact.
//
// It avoids enumAsUnion, which would yield a Union with no variants: a node
// nothing rejects and no reader can act on (GitHub #318).
//
// ValueType is the declared scalar type, else the top type. Whether a
// reference admits null is not settled here: schemaAdmitsNull reads the type
// keyword at each use site (GitHub #288).
func emptyEnum(c lowering.Ctx, s *oas3.Schema, common ir.TypeCommon, pointer jsontext.Pointer) (ir.TypeDef, []ir.Diagnostic) {
	diags := []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.EmptyEnum, pointer,
		"enum declares no member, so this position accepts no value; lowered as a closed enum with no members")}
	return &ir.Enum{
		TypeCommon: common,
		ValueType:  enumValueType(s, ir.PrimAny),
		Closed:     true,
	}, diags
}

// enumMembers converts enum nodes into scalar members, reporting ok=false for a
// non-scalar member, members heterogeneous in kind, or a set that keeps none
// (an all-null set degrades rather than becoming a memberless Enum).
//
// dropNull skips `null` members, for a schema whose nullability the enclosing
// reference carries (see lowerEnum). Kind agreement is read off the members
// kept. The PrimKind is the one every kept member maps to, meaningful only when
// ok.
//
// Each member's ID is keyed by its value under the enum at pointer, and a
// repeated value is kept, its ID numbered by occurrence; enumRepeats reports it.
func enumMembers(nodes []values.Value, dropNull bool, d ids.Document, pointer jsontext.Pointer) ([]ir.EnumMember, ir.PrimKind, bool) {
	members := make([]ir.EnumMember, 0, len(nodes))
	occurrences := make(map[string]int, len(nodes))
	var kind ir.ValueKind
	var prim ir.PrimKind
	for _, node := range nodes {
		val, err := value.FromNode(node)
		if err != nil {
			return nil, "", false
		}
		if dropNull && val.Kind == ir.ValueNull {
			continue
		}
		memberPrim, text, admissible := enumMemberForm(val)
		key, keyed := enumMemberKey(val)
		if !admissible || !keyed {
			return nil, "", false
		}
		if len(members) == 0 {
			kind, prim = val.Kind, memberPrim
		} else if val.Kind != kind {
			return nil, "", false
		}
		occurrences[key]++
		members = append(members, ir.EnumMember{
			ID:    d.EnumMember(pointer, key, occurrences[key]),
			Name:  compile.NamingFor(text),
			Value: val,
		})
	}
	if len(members) == 0 {
		return nil, "", false
	}
	return members, prim, true
}

// enumAsUnion lowers a heterogeneous or non-scalar enum to an exclusive Union of
// hoisted Literals, emitting one info diagnostic.
func enumAsUnion(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, common ir.TypeCommon, pointer jsontext.Pointer, hint string) (ir.TypeDef, []ir.Diagnostic) {
	diags := []ir.Diagnostic{c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
		"heterogeneous or non-scalar enum lowered as a union of literals")}
	nodes := s.GetEnum()
	variants := make([]ir.Variant, 0, len(nodes))
	for i, node := range nodes {
		vh := compile.SubHint(hint, strconv.Itoa(i))
		lptr := pointer + ids.Ptr("enum", strconv.Itoa(i))
		litID, litDiags := hoistLiteral(c, ts, node, lptr, vh)
		diags = append(diags, litDiags...)
		variants = append(variants, ir.Variant{
			Name: compile.NamingHint(vh),
			Type: ir.TypeRef{Target: litID},
		})
	}
	return &ir.Union{
		TypeCommon: common,
		Variants:   variants,
		Exclusive:  true,
	}, diags
}

// hoistLiteral hoists a single node as a Literal type at its own pointer, or
// falls back to the schemaless top type when the node is structurally
// unconvertible — never a Literal whose Value lies about being null. It is
// the single entry point for lowering both a bare `const` schema (pointer may
// be a top-level component, so internNode's ids.ForPointer keeps that
// component's stable named ID) and each individual member of a heterogeneous
// enum (enumAsUnion, always an anonymous sub-pointer).
//
// It returns the interned ID, plus the diagnostic an unconvertible node
// produces — none when the node converts.
func hoistLiteral(c lowering.Ctx, ts *compile.Types, node values.Value, pointer jsontext.Pointer, hint string,
) (ir.TypeID, []ir.Diagnostic) {
	// Captured from inside the build rather than reported around it, which keeps
	// the report tied to the node actually being constructed. Reporting eagerly
	// would be indistinguishable today — a second visit produces the identical
	// diagnostic, which Diags.Append drops — so nothing observable rests on this;
	// it is the honest place for it, not a load-bearing one.
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, hint, func(common ir.TypeCommon) ir.TypeDef {
		val, err := value.FromNode(node)
		if err != nil {
			diags = append(diags, c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, pointer,
				"unconvertible value lowered as the top type: %s", err.Error()))
			return &ir.Any{TypeCommon: common}
		}
		return &ir.Literal{TypeCommon: common, Value: val}
	})
	return id, diags
}

// enumValueType picks an Enum's ValueType from the schema's declared scalar
// type, falling back to the primitive its members classified to. It does not
// re-derive that primitive from a ValueKind: enumMemberForm decided it once,
// for the same members, so there is nothing here to disagree with.
func enumValueType(s *oas3.Schema, memberPrim ir.PrimKind) ir.PrimKind {
	if types := effectiveTypes(s); len(types) == 1 {
		switch types[0] {
		case oas3.SchemaTypeString:
			return ir.PrimString
		case oas3.SchemaTypeInteger:
			return ir.PrimInteger
		case oas3.SchemaTypeNumber:
			return ir.PrimNumber
		case oas3.SchemaTypeBoolean:
			return ir.PrimBool
		}
	}
	return memberPrim
}

// enumMemberForm classifies one lowered value as an Enum member: the PrimKind
// its kind declares and the literal text that names the member. ok=false means
// the kind has no place in an Enum, and the caller degrades the whole enum to a
// Union of Literals with a diagnostic.
//
// Its switch has one arm per ir.ValueKind, as enumMemberKey's does; the
// exhaustive linter fails when ir adds a kind either lacks. The default arm
// refuses a kind rather than assert a type the source never wrote.
func enumMemberForm(v ir.Value) (ir.PrimKind, string, bool) {
	switch v.Kind {
	case ir.ValueString:
		return ir.PrimString, v.Str, true
	case ir.ValueSymbol:
		// An interned symbol has no primitive of its own; PrimString is the form
		// it takes on a JSON wire. Unreachable from OpenAPI, whose values come
		// from YAML nodes, but ValueKind is IR-wide and this is its enum home.
		return ir.PrimString, v.Str, true
	case ir.ValueNumber:
		return ir.PrimNumber, string(v.Num), true
	case ir.ValueBool:
		return ir.PrimBool, strconv.FormatBool(v.Bool), true
	case ir.ValueBytes:
		return ir.PrimBytes, base64.StdEncoding.EncodeToString(v.Bytes), true
	case ir.ValueNull, ir.ValueList, ir.ValueObject, ir.ValueRefKind, ir.ValueCtor:
		// Composite, reference and null values are not enum members: none has a
		// primitive to declare or a literal text form to name a member by.
		return "", "", false
	default:
		return "", "", false
	}
}
