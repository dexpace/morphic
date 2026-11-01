// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"strconv"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/merge"
	"github.com/dexpace/morphic/ir"
)

// lowerModel hoists an object schema as a Model, or as a MapT when the object
// is a map and nothing else (objectNode): its value constraints, the
// property-set cardinality among them, its own properties and which are
// required, its catch-all (fillAdditional) and a declared discriminator. It
// reads no annotations: attachDeclaredAnnotations does that for every
// destination.
//
// Cardinality is read here rather than on lowerComponentSchema's internAlias
// fallback because an object-shaped schema owns its node before that fallback
// would run, so the fallback never fires for one (GitHub #129).
func lowerModel(c lowering.Ctx, ts *compile.Types, depth int, s *oas3.Schema, pointer jsontext.Pointer) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, func(common ir.TypeCommon) ir.TypeDef {
		cons, consDiags := ConstraintsOf(c, &common.Unmodeled, s, pointer)
		diags = append(diags, consDiags...)
		m := &ir.Model{TypeCommon: common, Constraints: cons}
		diags = append(diags, fillModelProperties(c, ts, depth, propIndex(c, ts, m), s, pointer)...)
		requireMembers(m, s.GetRequired())
		diags = append(diags, fillAdditional(c, ts, depth, m, s, pointer)...)
		d, discDiags := lowerDiscriminator(c, ts, s, m, nil, pointer)
		diags = append(diags, discDiags...)
		if d != nil {
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

// propIndex returns the index the walk fills m's properties through. A field
// declared more than once folds once every lowering has returned
// (merge.PropIndex), as a completion (compile.Types.Complete): a settlement
// that reads the merged field, such as a form part's flags, finds it folded.
func propIndex(c lowering.Ctx, ts *compile.Types, m *ir.Model) *merge.PropIndex {
	return merge.NewPropIndex(m, func(fold merge.Settle) { ts.Complete(settledMerge{c: c, ts: ts, fold: fold}) })
}

// settledMerge is the folds of one model's redeclared fields, taken once every
// node exists (compile.Settlement): a fold compares the nodes the declarations
// name, so mid-walk it would follow the order they lower in (GitHub #898).
type settledMerge struct {
	c    lowering.Ctx
	ts   *compile.Types
	fold merge.Settle
}

// Settle takes the fold against reg and returns what it reports. The fold
// rewrites the model's property, and adds a node only through mint.
func (s settledMerge) Settle(reg ir.TypeRegistry) []ir.Diagnostic {
	var diags []ir.Diagnostic
	s.fold(&merge.Merger{
		Resolve: func(id ir.TypeID) (ir.TypeDef, bool) { td, ok := reg[id]; return td, ok },
		Mint:    s.mint,
		Interned: func(pointer jsontext.Pointer) bool {
			_, ok := s.ts.Lookup(s.c.IDs().Coordinate(pointer))
			return ok
		},
		PropAt: s.c.IDs().ComposedProp,
		Report: func(sev ir.Severity, code string, pointer jsontext.Pointer, format string, args ...any) {
			diags = append(diags, s.c.DiagAt(sev, code, pointer, format, args...))
		},
	})
	return diags
}

// mint registers m, the composition of the field the declaration at pointer
// names. That pointer denotes the declaration's own schema, so m takes the ID of
// a node no schema occupies (ids.ComposedType) and points back at it, as a
// distributed variant points at its branch. The variants of one distributed
// union fold the same declarations, so each mints this one node, alike.
func (s settledMerge) mint(pointer jsontext.Pointer, m *ir.Model) ir.TypeID {
	id := s.c.IDs().ComposedType(pointer)
	m.TypeCommon = commonFor(s.c, id, pointer)
	s.ts.Register(id, m)
	return id
}

// fillModelProperties lowers a model's own properties in source order, each with
// its full property-level detail (constraints, visibility, defaults, docs, ...).
// Which are required is requireMembers' to say, once every property is in.
func fillModelProperties(c lowering.Ctx, ts *compile.Types, depth int, idx *merge.PropIndex, s *oas3.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
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
			Name:       compile.NamingFor(name),
			WireName:   name,
			Type:       ref,
			Provenance: c.ProvenanceAt(ppointer),
		}
		diags = append(diags, FillPropertyDetail(c, ts, &p, js, ppointer)...)
		idx.MergeProperty(p, redeclarationSource(js))
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
// catch-all. Neither spelling touches Additional, which this sets only for
// `false` and `unevaluatedProperties: false`.
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
		m.AdditionalProps = &ir.AdditionalProps{Value: ref}
	}
	patterns, patternDiags := patternProps(c, ts, depth, s, pointer)
	diags = append(diags, patternDiags...)
	if len(patterns) > 0 {
		if m.AdditionalProps == nil {
			m.AdditionalProps = &ir.AdditionalProps{Value: ts.PrimRef(ir.PrimAny)}
		}
		m.AdditionalProps.Patterns = patterns
	}
	if annotation.IsFalseSchema(s.GetUnevaluatedProperties()) {
		m.Additional = ir.AdditionalClosedAfterComposition
	}
	return diags
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
