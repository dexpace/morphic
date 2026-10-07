package irverify

import (
	"reflect"
	"strconv"
	"strings"

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
// its zero value (ir/value.go). A kind missing here is reported as undeclared;
// TestValuePayloads_CoverTheDeclaredKinds holds the keys to the ir sources.
//
// zeroIsValue separates a kind whose empty payload is still a value (false, "",
// an empty list) from one whose empty payload is nothing: a number with no
// digits, or a ref or ctor with no reference or call at all. A present
// reference naming nothing is an empty ID, which the reference rules own
// (GitHub #473).
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
// payload while another is populated gives a consumer two answers (GitHub
// #506), and the JSON form hides it, since every empty payload is omitted.
//
// The walk reaches values, not their carriers, so a new carrier is held at once.
// A pointer met again inside itself is ir/value-cycle (valuePointer).
func checkValues(doc *ir.Document, _ declarations) ([]Violation, bool) {
	var vs []Violation
	var stack []valuePointer
	truncated := ir.WalkValues(doc, ir.DocumentPath, func(v reflect.Value, path string) bool {
		stack = trimPointerStack(stack, path)
		if v.Kind() == reflect.Pointer && !v.IsNil() {
			if entered, onPath := pointerOnPath(stack, v); onPath {
				vs = append(vs, Violation{
					Code: "ir/value-cycle",
					Message: "value reaches a pointer it is already inside (entered at " +
						strconv.Quote(entered) + "), so the document cannot be encoded",
					Path: path,
				})
			} else {
				stack = append(stack, valuePointer{typ: v.Type(), addr: v.Pointer(), path: path})
			}
		}
		if v.Type() == valueType {
			vs = appendValueViolations(vs, v, path)
		}
		return true
	})
	return vs, truncated
}

// valuePointer is one pointer the walk is inside, and path is where the walk
// entered it. checkValues stacks these because [ir.WalkValues] hands a pointer
// over before the seen set that stops a second descent, so a pointer met again
// on the way down is a cycle the JSON encoder refuses and the walk's guard would
// hide. The rule is generic over pointers, not special-cased to ir.CtorValue,
// the one that reaches itself today. Self-containing slices hold no pointer;
// GitHub #736 tracks them. Type and address together identify the pointer: an
// address alone can be reused, and a type alone names every value.
type valuePointer struct {
	typ  reflect.Type
	addr uintptr
	path string
}

// trimPointerStack drops the frames the walk has left so the stack holds exactly
// the pointers enclosing path. Frames are pushed in the order the walk enters
// them, so their paths run outermost to innermost and only the innermost can be
// the one just left: popping while the top does not enclose path stops at the
// first frame that does, and every frame below that one encloses it too. The
// stack needs no bound of its own, since the walk stops at ir.MaxWalkDepth.
func trimPointerStack(stack []valuePointer, path string) []valuePointer {
	for len(stack) > 0 && !enclosesPath(stack[len(stack)-1].path, path) {
		stack = stack[:len(stack)-1]
	}
	return stack
}

// pointerOnPath reports whether v's pointer is already one the walk is inside,
// returning the path it was entered at.
func pointerOnPath(stack []valuePointer, v reflect.Value) (entered string, onPath bool) {
	for _, frame := range stack {
		if frame.typ == v.Type() && frame.addr == v.Pointer() {
			return frame.path, true
		}
	}
	return "", false
}

// enclosesPath reports whether path is ancestor itself or a path nested under
// it, the way [ir.WalkValues] spells the two: an equal path, ancestor followed
// by ".", or ancestor followed by "[".
//
// Reading rendered paths has two limits. A map key containing "." or "[" can
// make a sibling read as nested, reporting a cycle only when that sibling also
// shares the pointer; and an embedded pointer field would share its owner's
// path, though the document graph has none.
func enclosesPath(ancestor, path string) bool {
	return path == ancestor ||
		strings.HasPrefix(path, ancestor+".") ||
		strings.HasPrefix(path, ancestor+"[")
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
