package schema

import (
	"encoding/json/jsontext"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// discriminatorMovedWhy ends the degraded-lowering diagnostic when the
// discriminator leaves the model for Unmodeled beside the union.
const discriminatorMovedWhy = ", and it routes to a target that is not a subtype of this model, " +
	"so the discriminator is kept verbatim too"

// discriminatorRoutesToSubtypes reports whether every target the discriminator
// on s routes to is a subtype of s, the rule pass/validate holds a model
// discriminator to. Targets are the mapping values and defaultMapping, or the
// union branches when no mapping entry resolves. It reads the raw schemas, not
// the registry, where a subtype declared after s has no Base yet, so the answer
// cannot depend on declaration order. s declares a discriminator.
func discriminatorRoutesToSubtypes(c lowering.Ctx, s *oas3.Schema) bool {
	targets, mapped := mappingSchemas(c, s.GetDiscriminator())
	if !mapped {
		branches, _, _ := unionBranches(s)
		for _, b := range branches {
			targets = appendSchema(targets, refBranchTarget(b))
		}
	}
	for _, t := range targets {
		if !reachesByBase(t, s) {
			return false
		}
	}
	return true
}

// appendSchema appends t to dst unless it is nil, which is how a target that
// resolves to nothing drops out.
func appendSchema(dst []*oas3.Schema, t *oas3.Schema) []*oas3.Schema {
	if t == nil {
		return dst
	}
	return append(dst, t)
}

// mappingSchemas returns the schemas d's mapping values and defaultMapping
// resolve to, and whether any mapping entry resolved. An unresolvable value
// contributes nothing.
func mappingSchemas(c lowering.Ctx, d *oas3.Discriminator) ([]*oas3.Schema, bool) {
	var out []*oas3.Schema
	if m := d.GetMapping(); m != nil {
		for _, target := range m.All() {
			out = appendSchema(out, mappingSchema(c, d, target))
		}
	}
	mapped := len(out) > 0
	if dm := d.GetDefaultMapping(); dm != "" {
		out = appendSchema(out, mappingSchema(c, d, dm))
	}
	return out, mapped
}

// mappingSchema returns the schema a mapping value names, or nil when it names
// none this compilation resolves. A component names its own node, whatever it
// declares, so it is not read through; any other position that is only a $ref
// names the position the $ref does (typePosition).
func mappingSchema(c lowering.Ctx, d *oas3.Discriminator, target string) *oas3.Schema {
	scope := c.RefScope()
	pointer := ids.Ptr("components", "schemas", target)
	if !c.DeclaresSchema(target) {
		var ok bool
		if pointer, ok = scope.MappingPointer(d, target); !ok {
			return nil
		}
	}
	js := scope.DeclaredAt(pointer)
	if _, named := ids.ComponentSchemaName(pointer); named {
		return js.GetSchema()
	}
	if t := refBranchTarget(js); t != nil {
		return t
	}
	return js.GetSchema()
}

// reachesByBase reports whether base is an ancestor of target along the allOf
// base chain: the branch selectAllOfBase elects at each level, which is what
// lowers to Model.Base. A mixin does not count, as isSubtype ignores Mixins.
// The chain is bounded and cycle-checked.
func reachesByBase(target, base *oas3.Schema) bool {
	seen := map[*oas3.Schema]bool{target: true}
	for cur, hop := target, 0; hop < maxDiscriminatorAncestorDepth; hop++ {
		branches := cur.GetAllOf()
		i := selectAllOfBase(branches)
		if i < 0 {
			return false
		}
		next := refBranchTarget(branches[i])
		if next == base {
			return true
		}
		if next == nil || seen[next] {
			return false
		}
		seen[next] = true
		cur = next
	}
	return false
}

// moveDiscriminatorToUnmodeled takes the discriminator off the model that
// lowered beside a kept union and stores it verbatim in that model's Unmodeled,
// whole: propertyName, mapping and defaultMapping. A model discriminator that
// routes to a non-subtype is one pass/validate rejects, and the union that
// would have given it variants is kept verbatim, not lowered.
func moveDiscriminatorToUnmodeled(c lowering.Ctx, ts *compile.Types, id ir.TypeID, s *oas3.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	td, ok, diags := registeredNode(c, ts, id, pointer)
	if !ok {
		return diags
	}
	if m, isModel := td.(*ir.Model); isModel {
		m.Discriminator = nil
	}
	_, keepDiags := PreserveSchemaKeyword(c, &td.Common().Unmodeled, s, "discriminator",
		ir.ReasonDegradedLowering, pointer+ids.Ptr("discriminator"))
	return append(diags, keepDiags...)
}

// collapsesBesideDiscriminator reports whether s is a nullable-X union that
// lowerOneOfAnyOf collapses to a reference (nullUnionCollapse) and that also
// declares a discriminator. A reference has no field for one, so the position
// needs a node of its own to keep it on.
func collapsesBesideDiscriminator(s *oas3.Schema) bool {
	if s.GetDiscriminator() == nil || hasUnionSiblings(s) {
		return false
	}
	_, _, _, collapses := nullUnionCollapse(s, "")
	return collapses
}

// keepUnplacedDiscriminator keeps, verbatim under p, the discriminator s
// declares and the lowering at this position has no field to hold, and reports
// it with msg at pointer. It is the one mechanism every such site shares, so a
// union lowering that cannot place a discriminator cannot drop it quietly. It
// does nothing when s declares none. p is the Unmodeled of whatever owns the
// position: a node the position hoisted, or a carrier's own.
func keepUnplacedDiscriminator(c lowering.Ctx, p *ir.Unmodeled, s *oas3.Schema, pointer jsontext.Pointer, msg string) []ir.Diagnostic {
	if s.GetDiscriminator() == nil {
		return nil
	}
	_, diags := PreserveSchemaKeyword(c, p, s, "discriminator",
		ir.ReasonDegradedLowering, pointer+ids.Ptr("discriminator"))
	return append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer, "%s", msg))
}

// preserveCollapsedDiscriminator keeps the discriminator of a union that
// collapsed to a nullable reference. The collapse lowers {X, null} to nullable
// X, which routes nothing by tag, so the discriminator (propertyName, mapping
// and defaultMapping) would otherwise vanish without a word.
func preserveCollapsedDiscriminator(c lowering.Ctx, p *ir.Unmodeled, s *oas3.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	if !collapsesBesideDiscriminator(s) {
		return nil
	}
	return keepUnplacedDiscriminator(c, p, s, pointer,
		"a discriminator beside a oneOf/anyOf that collapses to a nullable reference "+
			"routes through no union; kept verbatim under Unmodeled")
}
