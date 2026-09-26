package irverify

import (
	"reflect"
	"strconv"

	"github.com/dexpace/morphic/ir"
)

var valueType = reflect.TypeFor[ir.Value]()

// payloadRule is what one ir.ValueKind asks of a Value: the field its payload
// lives in, "" for a kind that carries none, and whether that field's zero value
// is itself a value of the kind.
type payloadRule struct {
	field       string
	zeroIsValue bool
}

// valuePayloads is ir.Value's own contract, one row per declared kind: Kind
// selects the payload field that carries meaning, and every other payload holds
// its zero value (ir/value.go). A kind missing from this table is reported as
// undeclared, so TestValuePayloads_CoverTheDeclaredKinds holds its keys to the
// ir sources' const block.
//
// zeroIsValue separates a kind whose empty payload is still a value — false, "",
// an empty list — from one whose empty payload is nothing: a number with no
// digits, a reference or a constructor call that names nothing.
var valuePayloads = map[ir.ValueKind]payloadRule{
	ir.ValueNull:    {},
	ir.ValueBool:    {field: "Bool", zeroIsValue: true},
	ir.ValueString:  {field: "Str", zeroIsValue: true},
	ir.ValueNumber:  {field: "Num"},
	ir.ValueBytes:   {field: "Bytes", zeroIsValue: true},
	ir.ValueSymbol:  {field: "Str", zeroIsValue: true},
	ir.ValueList:    {field: "List", zeroIsValue: true},
	ir.ValueObject:  {field: "Object", zeroIsValue: true},
	ir.ValueRefKind: {field: "Ref"},
	ir.ValueCtor:    {field: "Ctor"},
}

// checkValues asserts every ir.Value is what its Kind says it is: a declared
// kind, no payload the kind does not select, and the payload it does select
// present where an empty one would be no value at all.
//
// Nothing else holds the rule ir.Value states. A value whose kind names one
// payload while another is populated gives a consumer two answers and no way to
// tell which to trust (GitHub #506), and the JSON form no longer shows the
// question: every payload is omitted when empty, so an encoded value spells
// only the payloads that are set.
//
// Values are reached through the walk rather than through the fields that carry
// them — defaults, consts, literals, enum members, examples, constructor
// arguments — so a new carrier is held the moment it exists. The walk continues
// below each value, since lists, objects and constructor arguments nest more.
func checkValues(doc *ir.Document, _ declarations) ([]Violation, bool) {
	var vs []Violation
	truncated := ir.WalkValues(doc, ir.DocumentPath, func(v reflect.Value, path string) bool {
		if v.Type() == valueType {
			vs = appendValueViolations(vs, v, path)
		}
		return true
	})
	return vs, truncated
}

// appendValueViolations reports one value's defects. An undeclared kind selects
// nothing, so its payloads are not judged: every one would be reported as stray
// when the repair is the kind.
func appendValueViolations(vs []Violation, v reflect.Value, path string) []Violation {
	kind := v.FieldByName("Kind").String()
	rule, declared := valuePayloads[ir.ValueKind(kind)]
	if !declared {
		return append(vs, Violation{
			Code:    "ir/unknown-value-kind",
			Message: "value carries undeclared kind " + strconv.Quote(kind),
			Path:    path + ".Kind",
		})
	}
	t := v.Type()
	for i := range t.NumField() {
		name := t.Field(i).Name
		if name == "Kind" {
			continue
		}
		set := payloadSet(v.Field(i))
		switch {
		case name != rule.field && set:
			vs = append(vs, Violation{
				Code:    "ir/value-stray-payload",
				Message: "value of kind " + strconv.Quote(kind) + " populates " + name + ", a payload its kind does not select",
				Path:    path + "." + name,
			})
		case name == rule.field && !set && !rule.zeroIsValue:
			vs = append(vs, Violation{
				Code:    "ir/value-missing-payload",
				Message: "value of kind " + strconv.Quote(kind) + " leaves " + name + " empty, which is no value of that kind",
				Path:    path + "." + name,
			})
		}
	}
	return vs
}

// payloadSet reports whether a payload field carries anything. It is the
// encoder's own test — each payload is omitted when zero, and a slice when
// empty — so a payload counts as set exactly when the JSON form would show it.
func payloadSet(field reflect.Value) bool {
	if field.Kind() == reflect.Slice {
		return field.Len() > 0
	}
	return !field.IsZero()
}
