package schema

import (
	"encoding/json/jsontext"
	"maps"
	"slices"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// deferDiscriminatorBesideUnion queues a settlement for each model that holds
// the discriminator written beside the union lowerBesideUnmodeledUnion kept
// verbatim on owner (discriminatorModels). A body that lowered to anything else
// holds none: the census kept its discriminator where it kept the rest, except
// on a multi-typed body with no object variant, which runs none (GitHub #852).
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
		pointer, internal := scope.TargetPointer(b, b.GetRef().String())
		if !internal {
			continue
		}
		if id, declared, component := scope.ComponentRef(pointer); component && declared {
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
// It moves only what it could copy (GitHub #144). One with no raw node to copy,
// merged in through a `<<` the raw readers miss (GitHub #395), stays on the
// model rather than vanish, where pass/validate reports the routes it fails.
//
// A move leaving a map swaps in its MapT (ir.AsMap) and keeps what that cannot
// hold (rehome); a later settlement leaves the map alone.
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
	kept, diags := PreserveSchemaKeyword(d.c, &owner.Common().Unmodeled, d.s, "discriminator", ir.ReasonDegradedLowering, at)
	if !kept {
		return diags
	}
	m.Discriminator = nil
	if mt, isMap := ir.AsMap(m); isMap {
		reg[d.model] = mt
		diags = append(diags, d.rehome(m, mt)...)
	}
	return append(diags, d.c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
		"the discriminator routes to a type that is not a subtype of the model holding it, so it cannot "+
			"stay there; kept verbatim under Unmodeled beside the union it was written for"))
}

// rehome keeps verbatim each census keyword the model homed and the MapT
// replacing it does not, a `required` naming keys among them. The census ran
// when the position lowered, while the discriminator still kept the node a
// Model, so without this the keyword would reach no home and no report. m has
// already lost its discriminator, which Settle keeps itself. A variant of a
// multi-typed body ran no census (GitHub #852), so nothing is redone for one.
func (d besideUnionDiscriminator) rehome(m *ir.Model, mt *ir.MapT) []ir.Diagnostic {
	if d.model != d.owner {
		return nil
	}
	handled := censusHandled(dispatchOf(d.s))
	before := unhomedKeywords(d.s, m, handled)
	lost := slices.DeleteFunc(unhomedKeywords(d.s, mt, handled), func(keyword string) bool {
		return slices.Contains(before, keyword)
	})
	return recordUnhomedAt(d.c, &mt.Unmodeled, d.s, lost, nodeShape(ir.KindMap), d.pointer)
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
