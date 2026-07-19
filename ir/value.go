package ir

// ValueKind names the shape of a Value payload. Values are typed data kept
// separate from the type graph (the TypeSpec Type-vs-Value split): defaults,
// constants, literal types, enum member values, and examples live here
// (ir-design §6).
type ValueKind string

// Value kinds. The Kind field of a Value selects which payload field carries
// meaning; all other payload fields hold their zero value.
const (
	// ValueNull is the null value.
	ValueNull ValueKind = "null"
	// ValueBool is a boolean value carried in Value.Bool.
	ValueBool ValueKind = "bool"
	// ValueString is a string value carried in Value.Str.
	ValueString ValueKind = "string"
	// ValueNumber is an arbitrary-precision numeric value carried in Value.Num.
	ValueNumber ValueKind = "number"
	// ValueBytes is a byte-string value carried in Value.Bytes.
	ValueBytes ValueKind = "bytes"
	// ValueSymbol is an interned-symbol value carried in Value.Str, distinct
	// from ValueString (Erlang atoms: on the native wire ok != <<"ok">>).
	ValueSymbol ValueKind = "symbol"
	// ValueList is an ordered sequence of values carried in Value.List.
	ValueList ValueKind = "list"
	// ValueObject is an ordered set of named values carried in Value.Object.
	ValueObject ValueKind = "object"
	// ValueRefKind is a reference to a declared constant carried in Value.Ref.
	// Its string form is "ref"; the constant is named ValueRefKind because
	// ValueRef is the referenced struct.
	ValueRefKind ValueKind = "ref"
	// ValueCtor is a value built by a named scalar constructor, carried in
	// Value.Ctor (TypeSpec scalar constructors such as plainDate.fromISO).
	ValueCtor ValueKind = "ctor"
)

// Value is typed data kept separate from the type graph: defaults, constants,
// literal types, enum member values, and examples (ir-design §6). Kind selects
// which payload field is meaningful; the remaining fields hold their zero
// value. The empty Value{Kind: ValueNull} marshals to {"kind":"null"}.
type Value struct {
	// Kind selects the meaningful payload field.
	Kind ValueKind `json:"kind"`
	// Bool is the payload for ValueBool.
	Bool bool `json:"bool,omitempty"`
	// Str is the payload for ValueString and ValueSymbol.
	Str string `json:"str,omitempty"`
	// Num is the payload for ValueNumber, an arbitrary-precision decimal string.
	Num BigVal `json:"num,omitempty"`
	// Bytes is the payload for ValueBytes, base64-encoded in JSON form.
	Bytes []byte `json:"bytes,omitempty"`
	// List is the payload for ValueList, an ordered sequence of values.
	List []Value `json:"list,omitempty"`
	// Object is the payload for ValueObject, an ordered set of named values.
	// Object member order carries meaning, so it is a slice, never a map.
	Object []Field `json:"object,omitempty"`
	// Ref is the payload for ValueRefKind, a reference to a declared constant.
	Ref *ValueRef `json:"ref,omitempty"`
	// Ctor is the payload for ValueCtor, a constructor-built value.
	Ctor *CtorValue `json:"ctor,omitempty"`
}

// Field is one named member of a ValueObject, in source order.
type Field struct {
	// Name is the member name.
	Name string `json:"name,omitempty"`
	// Value is the member value.
	Value Value `json:"value"`
}

// ValueRef references a declared constant: a TypeSpec enum-member default or a
// reference to a named const (ir-design §6).
type ValueRef struct {
	// Type identifies the declaring type.
	Type TypeID `json:"type,omitempty"`
	// Member names the referenced member within Type.
	Member string `json:"member,omitempty"`
}

// CtorValue captures a value built by a named scalar constructor, such as
// utcDateTime.now() or plainDate.fromISO("2024-05-06"). Such values are
// inherently non-literal, so frontends must not fold them (ir-design §6).
type CtorValue struct {
	// Scalar identifies the scalar whose constructor is invoked.
	Scalar TypeID `json:"scalar,omitempty"`
	// Name is the constructor name ("fromISO", "now", custom inits).
	Name string `json:"name,omitempty"`
	// Args are the constructor arguments in source order.
	Args []Value `json:"args,omitempty"`
}
