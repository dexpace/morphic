package graphql

import (
	"encoding/json"

	"github.com/vektah/gqlparser/v2/ast"

	"github.com/dexpace/morphic/ir"
)

// maxTypeDepth caps type-reference recursion (styleguide bounded-recursion
// rule). Named types terminate on their interned ID, so this bound only guards a
// pathologically deep list-wrapper nest, which no real schema produces.
const maxTypeDepth = 256

// lowerFields lowers a definition's fields into IR properties in source order.
// inputOnly toggles the input-field default/optionality rule.
func (l *lowerer) lowerFields(d *ast.Definition, inputOnly bool) []ir.Property {
	if len(d.Fields) == 0 {
		return nil
	}
	props := make([]ir.Property, 0, len(d.Fields))
	for _, f := range d.Fields {
		props = append(props, l.lowerField(f, d.Name, inputOnly))
	}
	return props
}

// lowerField lowers one field into a Property. Non-null (!) maps to Required and
// a non-nullable type ref; its absence maps to a nullable ref (ir-design §3.3).
// An input field that is non-null but carries a default is not required — the
// client may omit it — so the default relaxes Required.
func (l *lowerer) lowerField(f *ast.FieldDefinition, typeName string, inputOnly bool) ir.Property {
	pointer := fieldPtr(typeName, f.Name)
	prov := l.nodeProvenance(pointer, f.Position)
	p := ir.Property{
		ID:          propID(pointer),
		Name:        namingFor(f.Name),
		WireName:    f.Name,
		Type:        l.typeRef(f.Type, pointer),
		Required:    f.Type.NonNull && (!inputOnly || f.DefaultValue == nil),
		Default:     l.irValue(f.DefaultValue, prov),
		Args:        l.lowerArgs(f.Arguments, pointer),
		Docs:        docsFrom(f.Description),
		Deprecation: deprecationFrom(f.Directives),
		Extensions:  lowerDirectives(f.Directives),
		Provenance:  prov,
	}
	l.applyInaccessibleProp(&p, f.Directives)
	return p
}

// lowerArgs lowers a field's arguments into IR parameters (Property.Args) in
// source order. A non-null argument without a default is required; a default
// makes it optional even when non-null.
func (l *lowerer) lowerArgs(args ast.ArgumentDefinitionList, fieldPointer string) []ir.Parameter {
	if len(args) == 0 {
		return nil
	}
	params := make([]ir.Parameter, 0, len(args))
	for _, a := range args {
		argPointer := fieldPointer + ptr("args", a.Name)
		prov := l.nodeProvenance(argPointer, a.Position)
		params = append(params, ir.Parameter{
			Name:        namingFor(a.Name),
			Type:        l.typeRef(a.Type, argPointer),
			Required:    a.Type.NonNull && a.DefaultValue == nil,
			Default:     l.irValue(a.DefaultValue, prov),
			Docs:        docsFrom(a.Description),
			Deprecation: deprecationFrom(a.Directives),
			Extensions:  lowerDirectives(a.Directives),
		})
	}
	return params
}

// typeRef lowers a GraphQL type wrapper into an IR TypeRef. A named type resolves
// to its interned ID; a list wrapper hoists a real List node (containers are
// nodes with IDs, never flags on a reference). Nullability is the absence of !
// at each layer, so every [T!]! combination is captured per-layer.
func (l *lowerer) typeRef(t *ast.Type, pointer string) ir.TypeRef {
	if t == nil {
		return l.primRef(ir.PrimAny)
	}
	l.depth++
	defer func() { l.depth-- }()
	if l.depth > maxTypeDepth {
		l.diags = append(l.diags, diagf(ir.SeverityError, codeDegradedConstruct,
			l.nodeProvenance(pointer, t.Position),
			"type nesting exceeds %d; lowered as any", maxTypeDepth))
		return l.primRef(ir.PrimAny)
	}
	nullable := !t.NonNull
	if t.NamedType != "" {
		return ir.TypeRef{Target: l.namedRef(t.NamedType, t.Position), Nullable: nullable}
	}
	return ir.TypeRef{Target: l.listID(t, pointer), Nullable: nullable}
}

// listID interns the List node for a list wrapper at pointer and returns its ID.
func (l *lowerer) listID(t *ast.Type, pointer string) ir.TypeID {
	listPtr := pointer + "/list"
	id := anonTypeID(listPtr)
	return l.intern(listPtr, id, func() ir.TypeDef {
		return &ir.List{
			TypeCommon: l.anonCommon(id, listPtr, "list", t.Position),
			Elem:       l.typeRef(t.Elem, listPtr),
		}
	})
}

// namedRef resolves a GraphQL type name to a TypeID. Int/Float/String/Boolean
// map to shared primitives; ID maps to a named string scalar to preserve its
// distinct identity; every other name is a defined type (a dangling reference is
// diagnosed and left for the validate pass).
func (l *lowerer) namedRef(name string, pos *ast.Position) ir.TypeID {
	switch name {
	case "Int":
		return l.primRef(ir.PrimInt32).Target
	case "Float":
		return l.primRef(ir.PrimFloat64).Target
	case "String":
		return l.primRef(ir.PrimString).Target
	case "Boolean":
		return l.primRef(ir.PrimBool).Target
	case "ID":
		return l.idScalarID()
	}
	if _, ok := l.defs[name]; !ok {
		l.reportUnknownType(name, pos)
	}
	return namedTypeID(typePtr(name))
}

// idScalarID interns the built-in ID scalar as a named string-based scalar.
// GraphQL ID serializes as a string but is a distinct named type from String
// (it also accepts integer inputs and means "identifier"), so representing it as
// a Scalar over string — not a bare String primitive — is the lossless choice.
func (l *lowerer) idScalarID() ir.TypeID {
	pointer := ptr("scalars", "ID")
	id := namedTypeID(pointer)
	return l.intern(pointer, id, func() ir.TypeDef {
		base := l.primRef(ir.PrimString)
		return &ir.Scalar{
			TypeCommon: ir.TypeCommon{
				ID:         id,
				Name:       namingFor("ID"),
				Extensions: ir.Extensions{"graphql:builtin-scalar": json.RawMessage(`"ID"`)},
			},
			Base: &base,
		}
	})
}

// anonCommon builds the TypeCommon of a hoisted anonymous node (a list wrapper):
// no source name, only a context hint, marked Anonymous.
func (l *lowerer) anonCommon(id ir.TypeID, pointer, hint string, pos *ast.Position) ir.TypeCommon {
	return ir.TypeCommon{
		ID:         id,
		Anonymous:  true,
		Name:       ir.Naming{Hint: hint},
		Provenance: l.nodeProvenance(pointer, pos),
	}
}
