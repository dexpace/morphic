package graphql

import (
	"sort"

	"github.com/vektah/gqlparser/v2/ast"

	"github.com/dexpace/morphic/ir"
)

// lowerTypes interns every named type definition except the three root operation
// types, which become the operation surface rather than data types. Definitions
// are lowered in name order for deterministic diagnostics; the interned output is
// order-independent (IDs derive from structural pointers, not visitation order).
func (l *lowerer) lowerTypes() {
	for _, name := range sortedDefNames(l.defs) {
		if l.roots.isRoot(name) {
			continue
		}
		l.lowerDefinition(l.defs[name])
	}
}

// sortedDefNames returns the definition names in ascending order.
func sortedDefNames(defs map[string]*mergedDef) []string {
	names := make([]string, 0, len(defs))
	for name := range defs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// lowerDefinition interns one definition under its structural pointer.
func (l *lowerer) lowerDefinition(md *mergedDef) {
	pointer := typePtr(md.def.Name)
	l.intern(pointer, namedTypeID(pointer), func() ir.TypeDef { return l.buildDefinition(md) })
}

// buildDefinition dispatches on the definition kind. An unexpected kind degrades
// to Any with a diagnostic rather than dropping the type.
func (l *lowerer) buildDefinition(md *mergedDef) ir.TypeDef {
	switch md.def.Kind {
	case ast.Object:
		return l.lowerModel(md, false, false)
	case ast.Interface:
		return l.lowerModel(md, true, false)
	case ast.InputObject:
		return l.lowerInput(md)
	case ast.Union:
		return l.lowerUnion(md)
	case ast.Enum:
		return l.lowerEnum(md)
	case ast.Scalar:
		return l.lowerScalar(md)
	default:
		pointer := typePtr(md.def.Name)
		l.diags = append(l.diags, diagf(ir.SeverityWarning, codeDegradedConstruct,
			positionProvenance(md.def.Position, l.srcIndex),
			"unsupported definition kind %q; lowered as any", md.def.Kind))
		return &ir.Any{TypeCommon: l.typeCommon(namedTypeID(pointer), pointer, md.def)}
	}
}

// typeCommon builds the TypeCommon shared by every named node: identity, docs,
// deprecation, the namespaced directive extensions, and provenance.
func (l *lowerer) typeCommon(id ir.TypeID, pointer string, d *ast.Definition) ir.TypeCommon {
	return ir.TypeCommon{
		ID:          id,
		Name:        namingFor(d.Name),
		Docs:        docsFrom(d.Description),
		Deprecation: deprecationFrom(d.Directives),
		Extensions:  lowerDirectives(d.Directives),
		Provenance:  l.nodeProvenance(pointer, d.Position),
	}
}

// lowerModel lowers an object, interface, or input-object body into a Model.
// Abstract marks interfaces; InputOnly marks input objects; implements A & B
// populates Implements, whose targets are the Abstract interface models.
func (l *lowerer) lowerModel(md *mergedDef, abstract, inputOnly bool) ir.TypeDef {
	d := md.def
	pointer := typePtr(d.Name)
	m := &ir.Model{
		TypeCommon: l.typeCommon(namedTypeID(pointer), pointer, d),
		Properties: l.lowerFields(d, inputOnly),
		Implements: l.implementsRefs(d),
		Abstract:   abstract,
		InputOnly:  inputOnly,
	}
	l.applyInaccessibleType(&m.TypeCommon, d.Directives)
	l.recordExtends(&m.TypeCommon, md)
	return m
}

// implementsRefs resolves a definition's implemented interfaces to N-ary
// conformance references (GraphQL implements A & B; targets are Abstract models).
func (l *lowerer) implementsRefs(d *ast.Definition) []ir.TypeRef {
	if len(d.Interfaces) == 0 {
		return nil
	}
	refs := make([]ir.TypeRef, 0, len(d.Interfaces))
	for _, iface := range d.Interfaces {
		refs = append(refs, ir.TypeRef{Target: l.namedRef(iface, d.Position)})
	}
	return refs
}

// lowerInput lowers an input object into an InputOnly Model, or into a tagged
// exclusive union when it carries @oneOf (ir-design §8.4: @oneOf inputs are
// spec-level tagged input unions and must never collapse to optional fields).
func (l *lowerer) lowerInput(md *mergedDef) ir.TypeDef {
	if md.def.Directives.ForName("oneOf") != nil {
		return l.lowerOneOfInput(md)
	}
	return l.lowerModel(md, false, true)
}

// lowerOneOfInput lowers a @oneOf input object into a key-tagged exclusive Union
// with one variant per field (WireTagged with a nil Discriminator: the wire
// shape is a single-key object keyed by the variant wire name — ir-design §4.4).
func (l *lowerer) lowerOneOfInput(md *mergedDef) ir.TypeDef {
	d := md.def
	pointer := typePtr(d.Name)
	u := &ir.Union{
		TypeCommon: l.typeCommon(namedTypeID(pointer), pointer, d),
		Exclusive:  true,
		WireTagged: true,
	}
	for _, f := range d.Fields {
		fp := fieldPtr(d.Name, f.Name)
		u.Variants = append(u.Variants, ir.Variant{
			Name:        namingFor(f.Name),
			Type:        l.typeRef(f.Type, fp),
			WireName:    f.Name,
			Docs:        docsFrom(f.Description),
			Deprecation: deprecationFrom(f.Directives),
			Extensions:  lowerDirectives(f.Directives),
		})
	}
	u.Extensions = mergeExtensions(u.Extensions, oneOfInputMarker())
	l.applyInaccessibleType(&u.TypeCommon, d.Directives)
	l.recordExtends(&u.TypeCommon, md)
	return u
}

// lowerUnion lowers a union type into a __typename-tagged exclusive Union
// (ir-design §4.4: WireTagged with a __typename Discriminator whose tag lives
// inside each variant payload). Members are always object types.
func (l *lowerer) lowerUnion(md *mergedDef) ir.TypeDef {
	d := md.def
	pointer := typePtr(d.Name)
	u := &ir.Union{
		TypeCommon: l.typeCommon(namedTypeID(pointer), pointer, d),
		Exclusive:  true,
		WireTagged: true,
	}
	mapping := make(map[string]ir.TypeID, len(d.Types))
	for i, member := range d.Types {
		memberID := l.namedRef(member, memberPos(d, i))
		u.Variants = append(u.Variants, ir.Variant{Name: namingFor(member), Type: ir.TypeRef{Target: memberID}})
		mapping[member] = memberID
	}
	if len(mapping) > 0 {
		u.Discriminator = &ir.Discriminator{PropertyName: "__typename", Mapping: mapping}
	}
	l.applyInaccessibleType(&u.TypeCommon, d.Directives)
	l.recordExtends(&u.TypeCommon, md)
	return u
}

// memberPos returns the source position of a union's i-th member type, falling
// back to the definition position when member positions were not recorded.
func memberPos(d *ast.Definition, i int) *ast.Position {
	if i < len(d.TypePositions) && d.TypePositions[i] != nil {
		return d.TypePositions[i]
	}
	return d.Position
}

// lowerEnum lowers an enum into a closed string Enum. GraphQL enum values
// serialize as their name strings on the JSON wire, so ValueType is string and
// each member value is the name.
func (l *lowerer) lowerEnum(md *mergedDef) ir.TypeDef {
	d := md.def
	pointer := typePtr(d.Name)
	e := &ir.Enum{
		TypeCommon: l.typeCommon(namedTypeID(pointer), pointer, d),
		ValueType:  ir.PrimString,
		Closed:     true,
	}
	for _, v := range d.EnumValues {
		e.Members = append(e.Members, ir.EnumMember{
			Name:        namingFor(v.Name),
			Value:       ir.Value{Kind: ir.ValueString, Str: v.Name},
			Docs:        docsFrom(v.Description),
			Deprecation: deprecationFrom(v.Directives),
			Extensions:  lowerDirectives(v.Directives),
		})
	}
	l.applyInaccessibleType(&e.TypeCommon, d.Directives)
	l.recordExtends(&e.TypeCommon, md)
	return e
}

// lowerScalar lowers a custom scalar into an opaque Scalar with a nil Base
// (ir-design §4.2: GraphQL custom scalars declare no base; emitters map nil-base
// scalars to their opaque-scalar strategy). @specifiedBy is preserved verbatim
// among the directive extensions.
func (l *lowerer) lowerScalar(md *mergedDef) ir.TypeDef {
	d := md.def
	pointer := typePtr(d.Name)
	s := &ir.Scalar{TypeCommon: l.typeCommon(namedTypeID(pointer), pointer, d)}
	l.applyInaccessibleType(&s.TypeCommon, d.Directives)
	l.recordExtends(&s.TypeCommon, md)
	return s
}
