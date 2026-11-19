// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"slices"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/merge"
	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// lowerModel hoists an object schema as a Model, or as a MapT when the object
// is a map and nothing else (objectNode): its value constraints, those that
// apply to a value of the kinds takes, the property-set cardinality among them,
// its own properties and which are required, its catch-all (fillAdditional)
// and, where discriminated, the discriminator it declares. It reads no
// annotations: fin, finishing the node, writes them for every destination.
//
// Cardinality is read here rather than on lowerComponentSchema's internAlias
// fallback because an object-shaped schema owns its node before that fallback
// would run, so the fallback never fires for one (GitHub #129).
func lowerModel(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema, pointer jsontext.Pointer, discriminated bool,
	takes kindSet, fin finisher,
) (ir.TypeID, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	id := internNode(c, ts, pointer, func(common ir.TypeCommon) (ir.TypeDef, keySet) {
		cons, consDiags := ConstraintsOf(c, &common.Unmodeled, s, nil, pointer, modelTakes(takes))
		diags = append(diags, consDiags...)
		m := &ir.Model{TypeCommon: common, Constraints: cons}
		diags = append(diags, fillModelProperties(c, ts, depth, newModelFields(c, ts, m), s, pointer)...)
		requireMembers(m, s.ObjectKeywords().Required)
		diags = append(diags, fillAdditional(c, ts, depth, m, s, pointer)...)
		if discriminated {
			d, discDiags := lowerDiscriminator(c, ts, s.Discriminator(), pointer, m, nil)
			diags = append(diags, discDiags...)
			m.Discriminator = d
		}
		return objectNode(m), modelReads(s, m, modelTakes(takes))
	}, fin)
	return id, diags
}

// modelReads returns what the build of m read of s, a Model's or a MapT's
// schema: its members and their catch-alls, a type naming an object, the
// discriminator m holds, and the value bounds of the kinds held m's reader took.
func modelReads(s *statement.Schema, m *ir.Model, held kindSet) keySet {
	read := objectKeys | typeReadAs(s, statement.KindObject) | nodeBoundsRead(s, held, m.Constraints, m.Unmodeled)
	if m.Discriminator != nil {
		read |= keyBit("discriminator")
	}
	return read
}

// objectNode is the node an object lowering yields: the MapT m is when it is a
// map and nothing else, else m (docs/ir/containers.md). It reads the built
// Model rather than the schema, so a keyword that comes to fill a Model field
// keeps the position a Model without a second predicate to update. A union
// variant composing its branch holds it as a Base or Mixin, and a false schema
// is closed, so neither is ever a map.
func objectNode(m *ir.Model) ir.TypeDef {
	if mt, ok := ir.AsMap(m); ok {
		return mt
	}
	return m
}

// modelFields is how the walk fills one model's properties: the context the
// model is lowered in, the types its nodes are registered with, the model, and
// the index of what it declares and composes (merge.PropIndex). A member more
// than one declaration decides settles once every lowering has returned
// (settleMembers), so a settlement reading the merged field, such as a form
// part's flags, finds it folded, and a fold mid-walk would follow the order the
// declarations lower in (GitHub #898). It is the walk the fold asks
// (merge.Walk). pointer is where the model is written.
type modelFields struct {
	c       lowering.Ctx
	ts      *compile.Types
	m       *ir.Model
	idx     *merge.PropIndex
	pointer jsontext.Pointer
}

// newModelFields returns the fields of m, which holds no property yet, lowered
// in c and registered with ts.
func newModelFields(c lowering.Ctx, ts *compile.Types, m *ir.Model) modelFields {
	return modelFields{c: c, ts: ts, m: m, idx: merge.NewPropIndex(m), pointer: m.Provenance.Pointer}
}

// merge lands p on the model, or records it as one more declaration of the
// field its wire name names (merge.PropIndex.MergeProperty), noting the model
// at its first redeclaration.
func (f modelFields) merge(p ir.Property, source merge.Source, secretBy ir.Inferred) {
	if f.idx.MergeProperty(p, source, secretBy) {
		membersOf(f.c).note(f)
	}
}

// compose records that the model composes target, written at at, where the
// walk meets it (merge.PropIndex.Compose), and notes the model.
func (f modelFields) compose(target ir.TypeID, at jsontext.Pointer) {
	f.idx.Compose(target, at)
	membersOf(f.c).note(f)
}

// Interned reports whether a node stands at pointer of the model's document
// (merge.Walk.Interned).
func (f modelFields) Interned(pointer jsontext.Pointer) bool {
	_, ok := f.ts.Lookup(f.c.IDs().Coordinate(pointer))
	return ok
}

// place returns where p is in the bindings' table, read at f.c's position for
// its pointer, as the model's own document holds it, and false for a site in
// another document, such as a component a $ref there reached.
func (f modelFields) place(p ir.Provenance) (bind.Position, bool) {
	return f.c.Position(p.Pointer), p.Source == f.c.Place().At(p.Pointer).Source
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

// Admits is merge.Walk.Admits: the schema written at p admits every instance
// of kind, as the run reads what its statement admits (admissionOf), its value
// sets, unions and the keywords beside a $ref among them, and no keyword it
// takes in refuses some instance of kind (narrowedWithin), a variant read with
// every schema holding it (variantHolders). A site place does not place, one
// holding no schema object, and a read cut short or meeting a keyword it does
// not follow that could refuse a kind (looseIn) are not exact, which keeps the
// verdict a structure had before its source was read.
func (f modelFields) Admits(p ir.Provenance, kind merge.Instance) (admits, exact bool) {
	at, placed := f.place(p)
	if !placed {
		return false, false
	}
	st := f.c.Statements().At(at)
	if st.Truth != statement.Stated {
		return false, false
	}
	kinds, partial, inexact := anyInstance, kindSet(0), false
	for _, read := range variantHolders(f.c, at, st) {
		r := admissionOf(f.c, read)
		kinds, partial, inexact = kinds&r.admits, partial|r.partial, inexact || r.cut || r.loose
	}
	if inexact {
		return false, false
	}
	want := instanceKinds(kind)
	return want != 0 && kinds&want == want && partial&want == 0, true
}

// variantHolders returns st, written at at, and where st is an entry of a
// oneOf or an anyOf, every schema an instance of that variant also meets: the
// one writing the union, and each holding that one in turn as an entry of its
// allOf or a branch of its union, as a distributed variant composes what each
// writes beside it. A schema st is an allOf entry of, and no union branch, is
// no variant, so st alone is read. Each holder stands two tokens up the
// pointer, so the climb ends within half its tokens.
func variantHolders(c lowering.Ctx, at bind.Position, st *statement.Statement) []*statement.Statement {
	out := []*statement.Statement{st}
	for range strings.Count(string(at.Pointer), "/") / 2 {
		holder, keyword, ok := holderOf(c, at, st)
		if !ok || len(out) == 1 && keyword == "allOf" {
			break
		}
		out = append(out, holder)
		at, st = bind.Position{Doc: at.Doc, Pointer: at.Pointer.Parent().Parent()}, holder
	}
	return out
}

// holderOf returns the statement writing the oneOf, anyOf or allOf an entry of
// which st, at at, is, with that keyword, and false where st is no such entry:
// its pointer ends elsewhere, names an entry its holder lacks, or st is read
// elsewhere.
func holderOf(c lowering.Ctx, at bind.Position, st *statement.Statement) (*statement.Statement, string, bool) {
	index, err := strconv.Atoi(at.Pointer.LastToken())
	if err != nil {
		return nil, "", false
	}
	keyword := at.Pointer.Parent().LastToken()
	holder := c.Statements().At(bind.Position{Doc: at.Doc, Pointer: at.Pointer.Parent().Parent()})
	var entries []statement.Sub
	switch keyword {
	case "oneOf":
		entries = holder.ChoiceKeywords().OneOf
	case "anyOf":
		entries = holder.ChoiceKeywords().AnyOf
	case "allOf":
		entries = holder.AllOf
	default:
		return nil, "", false
	}
	if index < 0 || index >= len(entries) || c.Statements().Sub(entries[index]) != st {
		return nil, "", false
	}
	return holder, keyword, true
}

// instanceKinds returns the kinds an instance of kind may be, none for the
// null, which a reader of what a schema admits leaves to nulls.
func instanceKinds(kind merge.Instance) kindSet {
	switch kind {
	case merge.InstanceString:
		return kindString
	case merge.InstanceInteger:
		return kindInteger
	case merge.InstanceNumber:
		return kindNumber
	case merge.InstanceBoolean:
		return kindBoolean
	case merge.InstanceArray:
		return kindArray
	case merge.InstanceObject:
		return kindObject
	default:
		return 0
	}
}

// Mint registers node, the type of the field the declaration at pointer names
// (merge.Walk.Mint). That pointer denotes the declaration's own schema, so node
// takes the ID of a node no schema occupies (ids.ComposedType) and points back
// at it, as a distributed variant points at its branch. The variants of one
// distributed union fold the same declarations, so each mints this one node,
// alike. A model composes each part where brought says, and is noted so its
// members settle (Members). A node minted where no schema is written is placed
// by the positions it is assembled from (assembled).
func (f modelFields) Mint(pointer jsontext.Pointer, node ir.TypeDef, brought, assembled []jsontext.Pointer) ir.TypeID {
	id := f.c.IDs().ComposedType(pointer)
	site, common := commonFor(f.c, id, pointer), node.Common()
	site.Provenance = f.c.Place().At(pointer, assembled...)
	from := common.ID
	common.ID, common.Name, common.Anonymous, common.Provenance = site.ID, site.Name, site.Anonymous, site.Provenance
	if e, ok := node.(*ir.Enum); ok {
		e.Members = membersBeneath(e.Members, from, func(tail string) ir.EnumMemberID {
			return f.c.IDs().ComposedMember(pointer, tail)
		})
	}
	f.ts.Register(id, node)
	if m, ok := node.(*ir.Model); ok {
		minted := newModelFields(f.c, f.ts, m)
		for i, part := range merge.ComposedOf(m) {
			minted.compose(part, brought[i])
		}
	}
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
func fillModelProperties(c lowering.Ctx, ts *compile.Types, depth int, fields modelFields, s *statement.Schema,
	pointer jsontext.Pointer,
) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, prop := range s.ObjectKeywords().Properties {
		js := c.Statements().Sub(prop)
		ppointer := pointer + ids.Ptr("properties", prop.Name)
		ref, refDiags := CarriedRef(c, ts, depth, js, ppointer)
		diags = append(diags, refDiags...)
		p := ir.Property{
			ID:         c.IDs().Prop(ppointer),
			Name:       ir.NamingFor(prop.Name),
			WireName:   prop.Name,
			Type:       ref,
			Provenance: c.Place().At(ppointer),
		}
		diags = append(diags, FillPropertyDetail(c, ts, &p, js, ppointer)...)
		declared := p.Secret
		diags = append(diags, PromoteUse(c, KeysLentTo(c, js, p.Type, ppointer), p.Unmodeled, &p.Deprecation, &p.Secret, &p.Provenance)...)
		var secretBy ir.Inferred
		if p.Secret && !declared {
			secretBy = promotion.ExtensionPromotionHeuristic
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
func redeclarationSource(js *statement.Statement) merge.Source {
	return func() (ir.RawValue, error) {
		switch js.Truth {
		case statement.Anything, statement.Nothing, statement.Unread:
			return ir.RawValue(strconv.FormatBool(js.Truth == statement.Anything)), nil
		default:
			return annotation.RawFromNode(js.Node)
		}
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
// unevaluatedProperties into the model's openness and catch-all shape
// (docs/compilers/openapi.md#additional-properties).
//
// `additionalProperties: true` types the catch-all as the top type, as `{}`
// does: the two are one schema in JSON Schema, and both state that members
// beyond the declared ones are allowed. Only an absent keyword writes no
// catch-all, unless patterns need one. Neither spelling touches Additional,
// which this sets only for `false` and for an `unevaluatedProperties: false`
// no additionalProperties leaves inert (annotation.ClosesAfterComposition).
func fillAdditional(c lowering.Ctx, ts *compile.Types, depth int, m *ir.Model, s *statement.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	return fillCatchAll(c, ts, depth, m, s.ObjectKeywords().AdditionalProperties, s, pointer, annotation.ClosesAfterComposition(s))
}

// fillCatchAll is fillAdditional with its sources named: the
// additionalProperties additional, and the patternProperties s writes, both
// written in the schema object at pointer, and whether an
// unevaluatedProperties: false closes m after composition. A composition reads
// them off the object closing it (fillComposedAdditional).
func fillCatchAll(c lowering.Ctx, ts *compile.Types, depth int, m *ir.Model, additional statement.Sub,
	s *statement.Schema, pointer jsontext.Pointer, closes bool,
) []ir.Diagnostic {
	var diags []ir.Diagnostic
	ap := c.Statements().Sub(additional)
	switch {
	case ap.Truth == statement.Unwritten:
		// Left out, or a null, which states nothing: no catch-all is written.
	case ap.Truth.ReadsFalse():
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
			// otherwise. Beside one after composition the source writes no schema
			// for it, so none is supplied: such a key is admitted only where
			// composition evaluates it.
			m.AdditionalProps = &ir.AdditionalProps{}
			if !closes {
				top := ts.PrimRef(ir.PrimAny)
				m.AdditionalProps.Value = &top
			}
		}
		m.AdditionalProps.Patterns = patterns
	}
	forbidRest(m)
	if closes {
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
func patternProps(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema, pointer jsontext.Pointer) ([]ir.PatternProps, []ir.Diagnostic) {
	pp := s.ObjectKeywords().PatternProperties
	if len(pp) == 0 {
		return nil, nil
	}
	var diags []ir.Diagnostic
	out := make([]ir.PatternProps, 0, len(pp))
	for _, p := range pp {
		ref, refDiags := Ref(c, ts, depth, c.Statements().Sub(p), pointer+ids.Ptr("patternProperties", p.Name))
		diags = append(diags, refDiags...)
		out = append(out, ir.PatternProps{Pattern: p.Name, Value: ref})
	}
	return out, diags
}
