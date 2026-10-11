package schema

import (
	"reflect"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// propertyType is what MarkSecretUses looks for in the walk.
var propertyType = reflect.TypeFor[ir.Property]()

// MarkSecretUses sets Property.Secret on every property in doc whose type
// reaches a Sensitive node, so the per-use carrier agrees with the node however
// the use spells its reference: a nullable oneOf or anyOf, an allOf wrapping one
// $ref, or an alias component. It runs once lowering is done, because a $ref
// names a component's ID before that component is lowered.
//
// It only ever sets the flag. A property FillPropertyDetail or the allOf fold
// already marked stays marked.
func MarkSecretUses(c lowering.Ctx, doc *ir.Document) []ir.Diagnostic {
	if doc == nil {
		return nil
	}
	return markSecretUses(c, doc, doc.Types)
}

// markSecretUses is MarkSecretUses over any root, reporting what it could not
// do rather than skipping it in silence: a walk the depth cap cut short, and a
// property it reached without being able to set it.
func markSecretUses(c lowering.Ctx, root any, reg ir.TypeRegistry) []ir.Diagnostic {
	unaddressable := 0
	truncated := ir.WalkValues(root, ir.DocumentPath, func(v reflect.Value, _ string) bool {
		if v.Type() != propertyType {
			return true
		}
		p, ok := addressedProperty(v)
		if !ok {
			unaddressable++
			return true
		}
		if reachesSensitive(reg, p.Type) {
			p.Secret = true
		}
		return true
	})
	var diags []ir.Diagnostic
	if truncated {
		diags = append(diags, c.DiagAt(ir.SeverityError, diag.InternalInvariant, "",
			"internal: the walk marking secret properties passed ir.MaxWalkDepth"))
	}
	if unaddressable > 0 {
		diags = append(diags, c.DiagAt(ir.SeverityError, diag.InternalInvariant, "",
			"internal: %d properties held by value could not be marked secret", unaddressable))
	}
	return diags
}

// addressedProperty returns the property v holds, when v is addressable. Every
// property in a Document is reached through a pointer or a slice, so a miss is
// a value the walk was handed by copy.
func addressedProperty(v reflect.Value) (*ir.Property, bool) {
	if !v.CanAddr() {
		return nil, false
	}
	p, ok := v.Addr().Interface().(*ir.Property)
	return p, ok
}

// reachesSensitive reports whether ref's target, or a node its Base chain
// reaches, is Sensitive. The chain is what a wrapper lowers to: an alias
// component is a Scalar over its target, and an allOf over one $ref a Model
// with that target as its Base. It is bounded by the registry's size, which no
// chain without a cycle can exceed.
func reachesSensitive(reg ir.TypeRegistry, ref ir.TypeRef) bool {
	id := ref.Target
	for range len(reg) + 1 {
		td, ok := reg[id]
		if !ok || ir.IsNilTypeDef(td) {
			return false
		}
		if td.Common().Sensitive {
			return true
		}
		base := baseOf(td)
		if base == nil {
			return false
		}
		id = base.Target
	}
	return false
}

// baseOf returns the Base a node of a kind that has one points at, or nil.
func baseOf(td ir.TypeDef) *ir.TypeRef {
	switch n := td.(type) {
	case *ir.Scalar:
		return n.Base
	case *ir.Model:
		return n.Base
	default:
		return nil
	}
}
