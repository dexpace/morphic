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

// deferDiscriminatorBesideUnion queues the settlement of whatever discriminator
// the model at id holds beside the union lowerBesideUnmodeledUnion kept
// verbatim. A body that lowered to anything else holds none: the census kept
// its discriminator where it kept the rest.
func deferDiscriminatorBesideUnion(c lowering.Ctx, ts *compile.Types, id ir.TypeID, s *oas3.Schema, pointer jsontext.Pointer) {
	td, _ := ts.Node(id)
	if _, isModel := td.(*ir.Model); !isModel {
		return
	}
	ts.Defer(besideUnionDiscriminator{c: c, s: s, pointer: pointer, model: id, named: namedBranches(c, s)})
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
// Unmodeled beside the union it was written for.
//
// It is a settlement because only a lowered node says whether its type is a
// subtype, and a subtype lowered after the model has no Base yet when the
// model lowers.
type besideUnionDiscriminator struct {
	c       lowering.Ctx
	s       *oas3.Schema
	pointer jsontext.Pointer
	model   ir.TypeID
	named   []ir.TypeID // namedBranches
}

// Settle keeps the model's discriminator or moves it. A model holding none is
// left alone: it declared none, or it was settled already, by the settlement a
// reference deferred when it built the model before its declaration did.
func (d besideUnionDiscriminator) Settle(reg ir.TypeRegistry) []ir.Diagnostic {
	m, isModel := reg[d.model].(*ir.Model)
	if !isModel || m == nil {
		return []ir.Diagnostic{d.c.DiagAt(ir.SeverityError, diag.InternalInvariant, d.pointer,
			"internal: %q is no model, so the discriminator deferred for it is dropped", d.model)}
	}
	if m.Discriminator == nil || routesToSubtypes(reg, m, d.named) {
		return nil
	}
	m.Discriminator = nil
	at := d.pointer + ids.Ptr("discriminator")
	_, diags := PreserveSchemaKeyword(d.c, &m.Unmodeled, d.s, "discriminator", ir.ReasonDegradedLowering, at)
	return append(diags, d.c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
		"the discriminator routes to a type that is not a subtype of this model, so it cannot stay on "+
			"the model; kept verbatim under Unmodeled beside the union it was written for"))
}

// routesToSubtypes reports whether every type m's discriminator routes to is a
// subtype of m: each mapping target, the default, and each of named. Each is
// one ir.IsSubtype walk, as pass/validate spends on a mapping target; no walk
// is shared yet (GitHub #850).
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
