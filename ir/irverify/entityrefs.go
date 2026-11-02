// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"reflect"
	"strconv"

	"github.com/dexpace/morphic/ir"
)

// entityRefType is the reflect.Type of ir.EntityRef.
var entityRefType = reflect.TypeFor[ir.EntityRef]()

// replacedByIndex is the index of Deprecation.ReplacedBy among its fields.
var replacedByIndex = fieldIndex(reflect.TypeFor[ir.Deprecation](), "ReplacedBy")

// replacementCarrier is where a type declaring an ID beside a Deprecation holds
// the two, by field index, and the EntityRef field naming an entity of its ID's
// class.
type replacementCarrier struct{ id, deprecation, ref int }

// replacementCarriers are the IR types declaring an ID beside a Deprecation,
// read once from the types themselves so the walk reaches each field by index
// and allocates nothing. TestReplacementCarriers_AreEveryOneTheIRDeclares holds
// the list to the ir sources.
var replacementCarriers = carriersOf(
	reflect.TypeFor[ir.AuthScheme](), reflect.TypeFor[ir.EnumMember](), reflect.TypeFor[ir.Message](),
	reflect.TypeFor[ir.Operation](), reflect.TypeFor[ir.Parameter](), reflect.TypeFor[ir.Property](),
	reflect.TypeFor[ir.TypeCommon](),
)

// carriersOf indexes the ID and Deprecation fields of each of types, and the
// field of an EntityRef of the ID's class.
func carriersOf(types ...reflect.Type) map[reflect.Type]replacementCarrier {
	out := make(map[reflect.Type]replacementCarrier, len(types))
	for _, t := range types {
		id := fieldIndex(t, "ID")
		ref := -1
		for i := range entityRefType.NumField() {
			if entityRefType.Field(i).Type == t.Field(id).Type {
				ref = i
			}
		}
		out[t] = replacementCarrier{id: id, deprecation: fieldIndex(t, "Deprecation"), ref: ref}
	}
	return out
}

// fieldIndex is the index of t's field name, which t declares itself.
func fieldIndex(t reflect.Type, name string) int {
	f, _ := t.FieldByName(name)
	return f.Index[0]
}

// checkEntityRefs asserts every ir.EntityRef names one entity, setting exactly
// one field: none names nothing (ir/entity-ref-empty), and two leave a reader
// to guess which is meant (ir/entity-ref-ambiguous). Whether the entity exists
// is checkReferentialIntegrity's. It also asserts no entity names itself as its
// replacement (ir/replaced-by-itself).
func checkEntityRefs(*ir.Document) walkCheck {
	var vs []Violation
	return walkCheck{
		visit: func(v reflect.Value, at ir.WalkPath) bool {
			if v.Kind() != reflect.Struct {
				return true
			}
			if v.Type() == entityRefType {
				vs = appendEntityRefShape(vs, v, at)
				return false
			}
			if c, carries := replacementCarriers[v.Type()]; carries {
				vs = appendSelfReplacement(vs, v, c, at)
			}
			return true
		},
		report: reporting(&vs),
	}
}

// appendEntityRefShape reports ref, an ir.EntityRef at at, unless exactly one
// of its fields is set.
func appendEntityRefShape(vs []Violation, ref reflect.Value, at ir.WalkPath) []Violation {
	set := 0
	for _, field := range ref.Fields() {
		if field.String() != "" {
			set++
		}
	}
	switch {
	case set == 0:
		return append(vs, Violation{
			Code: "ir/entity-ref-empty", Message: "entity reference names no entity", Path: at.String(),
		})
	case set > 1:
		return append(vs, Violation{
			Code:    "ir/entity-ref-ambiguous",
			Message: "entity reference sets " + strconv.Itoa(set) + " fields; it names one entity, of one class",
			Path:    at.String(),
		})
	default:
		return vs
	}
}

// appendSelfReplacement reports each replacement the Deprecation of node, a
// carrier c indexes, names that is node itself: of its ID's class and value.
func appendSelfReplacement(vs []Violation, node reflect.Value, c replacementCarrier, at ir.WalkPath) []Violation {
	dep := node.Field(c.deprecation)
	own := node.Field(c.id).String()
	if dep.IsNil() || own == "" {
		return vs
	}
	refs := dep.Elem().Field(replacedByIndex)
	for i := range refs.Len() {
		if refs.Index(i).Field(c.ref).String() == own {
			vs = append(vs, Violation{
				Code:    "ir/replaced-by-itself",
				Message: "deprecation names " + strconv.Quote(own) + ", the entity it deprecates, as its replacement",
				Path:    at.String() + ".Deprecation.ReplacedBy[" + strconv.Itoa(i) + "]",
			})
		}
	}
	return vs
}
