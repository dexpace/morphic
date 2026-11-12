// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"slices"
	"strconv"
	"strings"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/merge"
	"github.com/dexpace/morphic/ir"
)

// lowerModel hoists an object schema as a Model, or as a MapT when the object
// is a map and nothing else (objectNode): its value constraints, the
// property-set cardinality among them, its own properties and which are
// required, its catch-all (fillAdditional) and, where discriminated, the
// discriminator it declares. It reads no annotations: attachDeclaredAnnotations
// does that for every destination.
//
// Cardinality is read here rather than on lowerComponentSchema's internAlias
// fallback because an object-shaped schema owns its node before that fallback
// would run, so the fallback never fires for one (GitHub #129).
func lowerModel(c lowering.Ctx, ts *compile.Types, depth int, s *oas3.Schema, pointer jsontext.Pointer, discriminated bool) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, func(common ir.TypeCommon) ir.TypeDef {
		cons, consDiags := ConstraintsOf(c, &common.Unmodeled, s, nil, pointer)
		diags = append(diags, consDiags...)
		m := &ir.Model{TypeCommon: common, Constraints: cons}
		diags = append(diags, fillModelProperties(c, ts, depth, newModelFields(c, ts, m), s, pointer)...)
		requireMembers(m, s.GetRequired())
		diags = append(diags, fillAdditional(c, ts, depth, m, s, pointer)...)
		if discriminated {
			d, discDiags := lowerDiscriminator(c, ts, s, m, nil, pointer)
			diags = append(diags, discDiags...)
			m.Discriminator = d
		}
		return objectNode(m)
	})
	return id, diags
}

// objectNode is the node an object lowering yields: the MapT m is when it is a
// map and nothing else, else m (docs/ir/containers.md). It reads the built
// Model rather than the schema, so a keyword that comes to fill a Model field
// keeps the position a Model without a second predicate to update. A composed
// union variant and a false schema skip it: one always holds its branch as a
// Base or Mixin, and the other is closed. An inert `unevaluatedProperties:
// false` beside a catch-all still closes a model, keeping it one until #862.
func objectNode(m *ir.Model) ir.TypeDef {
	if mt, ok := ir.AsMap(m); ok {
		return mt
	}
	return m
}

// modelFields is how the walk fills one model's properties: the context the
// model is lowered in, the types its nodes are registered with, and the index of
// the properties it declares (merge.PropIndex). A field declared more than once
// folds once every lowering has returned, as a completion
// (compile.Types.Complete), queued at the model's first redeclaration: a
// settlement that reads the merged field, such as a form part's flags, finds it
// folded, and a fold mid-walk would follow the order the declarations lower in
// (GitHub #898). It is the walk the fold asks (merge.Walk).
type modelFields struct {
	c   lowering.Ctx
	ts  *compile.Types
	idx *merge.PropIndex
}

// newModelFields returns the fields of m, which holds no property yet, lowered
// in c and registered with ts.
func newModelFields(c lowering.Ctx, ts *compile.Types, m *ir.Model) modelFields {
	return modelFields{c: c, ts: ts, idx: merge.NewPropIndex(m)}
}

// merge lands p on the model, or records it as one more declaration of the
// field its wire name names (merge.PropIndex.MergeProperty), queueing the
// model's folds at its first redeclaration.
func (f modelFields) merge(p ir.Property, source merge.Source, secretBy ir.Inferred) {
	if f.idx.MergeProperty(p, source, secretBy) {
		f.ts.Complete(f)
	}
}

// Settle folds the model's redeclared fields against reg, the registry every
// lowering left, and returns what the folds report (compile.Settlement). A
// fold rewrites the model's property, and adds a node only through Mint.
func (f modelFields) Settle(reg ir.TypeRegistry) []ir.Diagnostic {
	return f.idx.Fold(merge.Merger{Reg: reg, IDs: f.c.IDs(), Doc: f.c.Place(), Walk: f})
}

// Interned reports whether a node stands at pointer of the model's document
// (merge.Walk.Interned).
func (f modelFields) Interned(pointer jsontext.Pointer) bool {
	_, ok := f.ts.Lookup(f.c.IDs().Coordinate(pointer))
	return ok
}

// place returns where p is in the bindings' table, read at f.c's position for
// its pointer, as the model's own document holds it, and false for a site in
// another document, such as a component a $ref there reached, or one c does not
// place.
func (f modelFields) place(p ir.Provenance) (bind.Position, bool) {
	at, placed := f.c.Position(p.Pointer)
	return at, placed && p.Source == f.c.Place().At(p.Pointer).Source
}

// Alike is merge.Walk.Alike. A site place does not place, or one whose base the
// bindings cannot tell, is no match.
func (f modelFields) Alike(a, b ir.Provenance) bool {
	base := func(p ir.Provenance) (string, bool) {
		at, placed := f.place(p)
		uri, known := f.c.Bindings().Table().Base(at)
		return uri, placed && known
	}
	ua, okA := base(a)
	ub, okB := base(b)
	return okA && okB && ua == ub
}

// Untyped is merge.Walk.Untyped: the schema written at p states no instance
// type, its own `type` and its allOf's read as allof-disjoint-conjuncts reads a
// conjunct (conjunctTypes). A site place does not place, one holding no schema
// object, and a read that runs out of its budget or passes over what could
// refuse a value of some instance type, state one, which keeps the verdict a
// structure had before its source was read.
func (f modelFields) Untyped(p ir.Provenance) bool {
	at, placed := f.place(p)
	if !placed {
		return false
	}
	js := f.c.Bindings().At(at)
	if js == nil || js.GetSchema() == nil {
		return false
	}
	r := conjunctTypes{refs: f.c.Bindings(), steps: budget.New(budget.AtMost(maxConjunctTypeReads))}
	return r.schema(js.GetSchema()) == anyInstance && !r.steps.Exhausted() && !r.passedOver
}

// Mint registers node, the type of the field the declaration at pointer names
// (merge.Walk.Mint). That pointer denotes the declaration's own schema, so node
// takes the ID of a node no schema occupies (ids.ComposedType) and points back
// at it, as a distributed variant points at its branch. The variants of one
// distributed union fold the same declarations, so each mints this one node,
// alike.
func (f modelFields) Mint(pointer jsontext.Pointer, node ir.TypeDef) ir.TypeID {
	id := f.c.IDs().ComposedType(pointer)
	site, common := commonFor(f.c, id, pointer), node.Common()
	from := common.ID
	common.ID, common.Name, common.Anonymous, common.Provenance = site.ID, site.Name, site.Anonymous, site.Provenance
	if e, ok := node.(*ir.Enum); ok {
		e.Members = membersBeneath(e.Members, from, func(tail string) ir.EnumMemberID {
			return f.c.IDs().ComposedMember(pointer, tail)
		})
	}
	f.ts.Register(id, node)
	return id
}

// membersBeneath returns a copy of ms, the members of the enum identified by
// from, each identified beneath the enum a mint registers instead (idAt), by the
// tail its ID adds to from's path. A member ID outside from's path keeps it, for
// irverify to report (docs/ir/identity.md#enum-members).
func membersBeneath(ms []ir.EnumMember, from ir.TypeID, idAt func(tail string) ir.EnumMemberID) []ir.EnumMember {
	enumPath, ok := ir.IDPath(ir.IDKindType, string(from))
	out := slices.Clone(ms)
	for i, m := range out {
		path, hasPath := ir.IDPath(ir.IDKindEnumMember, string(m.ID))
		if ok && hasPath && strings.HasPrefix(path, enumPath+"/") {
			out[i].ID = idAt(strings.TrimPrefix(path, enumPath))
		}
	}
	return out
}

// fillModelProperties lowers a model's own properties in source order, each with
// its full property-level detail (constraints, visibility, defaults, docs, ...).
// Which are required is requireMembers' to say, once every property is in.
func fillModelProperties(c lowering.Ctx, ts *compile.Types, depth int, fields modelFields, s *oas3.Schema,
	pointer jsontext.Pointer,
) []ir.Diagnostic {
	props := s.GetProperties()
	if props == nil {
		return nil
	}
	var diags []ir.Diagnostic
	for name, js := range props.All() {
		ppointer := pointer + ids.Ptr("properties", name)
		ref, refDiags := CarriedRef(c, ts, depth, js, ppointer)
		diags = append(diags, refDiags...)
		p := ir.Property{
			ID:         c.IDs().Prop(ppointer),
			Name:       ir.NamingFor(name),
			WireName:   name,
			Type:       ref,
			Provenance: c.Place().At(ppointer),
		}
		diags = append(diags, FillPropertyDetail(c, ts, &p, js, ppointer)...)
		declared := p.Secret
		diags = append(diags, PromoteUse(c, KeysLentTo(c, ts, js, p.Type, ppointer), p.Unmodeled, &p.Deprecation, &p.Secret, &p.Provenance)...)
		var secretBy ir.Inferred
		if p.Secret && !declared {
			secretBy = lowering.ExtensionPromotionHeuristic
		}
		fields.merge(p, redeclarationSource(js), secretBy)
	}
	return diags
}

// redeclarationSource renders the schema written at a property position
// verbatim, as the merge keeps it when the position declares a field another
// declares too and the fold cannot carry all of what it says. It is a
// function rather than the bytes because nearly every property is declared
// once, and rendering a node nobody will keep is the cost the merge asks for
// only when it has something to keep.
//
// A boolean schema has no node of its own to render, so its value is spelled
// out; a `$ref` is rendered as the `$ref` the position wrote, not the target.
func redeclarationSource(js *oas3.JSONSchema[oas3.Referenceable]) merge.Source {
	return func() (ir.RawValue, error) {
		if annotation.IsNullLiteral(js) {
			return ir.RawValue("null"), nil
		}
		if b := js.GetBool(); b != nil {
			return ir.RawValue(strconv.FormatBool(*b)), nil
		}
		return annotation.RawFromNode(js.GetSchema().GetRootNode())
	}
}

// requireMembers lands each name of required on m, once m holds every own
// property: as Required on the property of that wire name, never cleared once
// set, and otherwise in m.Requires, once, in the order required names it. A
// name no own property declares is one a Base or Mixin declares, or one only
// the catch-all governs, and JSON Schema requires it all the same
// (docs/ir/models.md). Each object lowering calls it once, on a model that
// lists none yet.
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

// fillAdditional lowers additionalProperties, patternProperties, and
// unevaluatedProperties into the model's openness and catch-all shape.
//
// `additionalProperties: true` types the catch-all as the top type, as `{}`
// does: the two are one schema in JSON Schema, and both state that members
// beyond the declared ones are allowed. Only an absent keyword writes no
// catch-all, unless patterns need one. Neither spelling touches Additional,
// which this sets only for `false` and `unevaluatedProperties: false`.
func fillAdditional(c lowering.Ctx, ts *compile.Types, depth int, m *ir.Model, s *oas3.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	var diags []ir.Diagnostic
	ap := s.GetAdditionalProperties()
	switch {
	case !annotation.Written(ap):
		// Left out, or a null, which states nothing: no catch-all is written.
	case annotation.IsFalseSchema(ap):
		m.Additional = ir.AdditionalClosed
	default:
		ref, refDiags := Ref(c, ts, depth, ap, pointer+ids.Ptr("additionalProperties"))
		diags = append(diags, refDiags...)
		m.AdditionalProps = &ir.AdditionalProps{Value: &ref}
	}
	patterns, patternDiags := patternProps(c, ts, depth, s, pointer)
	diags = append(diags, patternDiags...)
	if len(patterns) > 0 {
		if m.AdditionalProps == nil {
			// A key no pattern matches is unconstrained unless a closure says
			// otherwise. Beside `unevaluatedProperties: false` it stays the top
			// type, though JSON Schema then admits such a key only where
			// composition evaluates it (GitHub #862).
			top := ts.PrimRef(ir.PrimAny)
			m.AdditionalProps = &ir.AdditionalProps{Value: &top}
		}
		m.AdditionalProps.Patterns = patterns
	}
	forbidRest(m)
	if annotation.IsFalseSchema(s.GetUnevaluatedProperties()) {
		m.Additional = ir.AdditionalClosedAfterComposition
	}
	return diags
}

// forbidRest drops from a closed model's catch-all the value typing the keys no
// pattern matches, which the closure allows none of, and the catch-all itself
// when no pattern is left (docs/ir/models.md#catch-alls). Each writer of a
// closure beside a catch-all calls it once the closure is decided.
func forbidRest(m *ir.Model) {
	if m.Additional != ir.AdditionalClosed || m.AdditionalProps == nil {
		return
	}

	m.AdditionalProps.Value = nil
	if len(m.AdditionalProps.Patterns) == 0 {
		m.AdditionalProps = nil
	}
}

// patternProps lowers patternProperties into pattern/value bindings in source
// order.
func patternProps(c lowering.Ctx, ts *compile.Types, depth int, s *oas3.Schema, pointer jsontext.Pointer) ([]ir.PatternProps, []ir.Diagnostic) {
	pp := s.GetPatternProperties()
	if pp == nil || pp.Len() == 0 {
		return nil, nil
	}
	var diags []ir.Diagnostic
	out := make([]ir.PatternProps, 0, pp.Len())
	for pattern, js := range pp.All() {
		ref, refDiags := Ref(c, ts, depth, js, pointer+ids.Ptr("patternProperties", pattern))
		diags = append(diags, refDiags...)
		out = append(out, ir.PatternProps{Pattern: pattern, Value: ref})
	}
	return out, diags
}
