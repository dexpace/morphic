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
// its zero value (ir/value.go). A kind missing from this table is reported as
// undeclared, so TestValuePayloads_CoverTheDeclaredKinds holds its keys to the
// ir sources' const block.
//
// zeroIsValue separates a kind whose empty payload is still a value — false, "",
// an empty list — from one whose empty payload is nothing: a number with no
// digits, a ref or ctor value with no reference or call at all. A reference or
// call that is present but names nothing is an empty ID, which the reference
// rules own rather than this table (GitHub #473).
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
//
// The visitor also tracks every non-nil pointer it is handed, because the walk
// hands a pointer over before the seen set that stops it descending twice
// ([ir.WalkValues]): a pointer met again on the way down is a cycle the JSON
// encoder refuses, and the walk's own guard would otherwise hide it. The rule is
// generic over pointers rather than special-cased to ir.CtorValue — the one
// pointer that reaches itself today (TestValueCycle_OnlyCtorValueReachesItself)
// — so a future self-reachable pointer is held the moment it exists.
//
// It keeps a stack of the pointers it is currently inside: each one's type,
// address and the path it was entered at. Before judging a visit the stack is
// trimmed to the frames whose path encloses the current one (trimPointerStack),
// and a pointer already on it is ir/value-cycle at the current walk path, naming
// the quoted path it was entered at (pointerOnPath). A pointer met again off the
// path is a value two places share, which encodes twice without trouble, and
// stays clean. The stack needs no bound of its own: the walk never descends past
// ir.MaxWalkDepth, so it cannot grow past that, and a guard for it would be a
// statement no document reaches — the namingChannels precedent (naming.go).
//
// Two limits follow from the ancestor test reading rendered paths rather than
// the values. A map key that itself contains "." or "[" could make a sibling
// path read as nested under an unrelated pointer's path, reporting a cycle only
// when that sibling also shares the pointer; and an embedded pointer field would
// share its owner's path rather than add a segment, though the document graph
// has no embedded pointer. A list whose element holds the same list is a second
// cycle mechanism that involves no pointer at all; it hangs Verify and is left
// out of scope here, tracked as its own issue (GitHub #736).
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

// valuePointer is one pointer the walk is inside: its type and address identify
// the meeting, and path is where the walk entered it. Type and address together
// are what make two meetings the same pointer — the address alone can be handed
// back to a later allocation, and the type alone names every value of that type.
type valuePointer struct {
	typ  reflect.Type
	addr uintptr
	path string
}

// trimPointerStack drops the frames the walk has left so the stack holds exactly
// the pointers enclosing path. Frames are pushed in the order the walk enters
// them, so their paths run outermost to innermost and only the innermost can be
// the one just left: popping while the top does not enclose path stops at the
// first frame that does, and every frame below that one encloses it too.
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
// it, the way [ir.WalkValues] spells the two: an equal path (a pointer's element
// and its embedded fields share its own), ancestor followed by ".", or ancestor
// followed by "[".
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
