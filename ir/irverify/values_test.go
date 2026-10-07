package irverify_test

import (
	"reflect"
	"slices"
	"strconv"
	"testing"
	"unicode/utf8"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// valueDoc embeds v as the one property default of an otherwise sound
// document — a model referencing itself as its one property's type, the way
// bigValCarriers does, so nothing but v is open to question.
func valueDoc(v ir.Value) *ir.Document {
	m := &ir.Model{
		ID:   "t/x/M",
		Name: named("m"),
		Properties: []ir.Property{{
			ID:      "p/x/M/f",
			Name:    named("f"),
			Type:    ir.TypeRef{Target: "t/x/M"},
			Default: &v,
		}},
	}
	return &ir.Document{IRVersion: ir.IRVersion, Types: ir.TypeRegistry{m.ID: m}}
}

// valueDocPath is the path valueDoc's Default value sits at, before any of the
// Value's own field segments.
const valueDocPath = "doc.Types[t/x/M].Properties[0].Default"

// exampleDoc embeds ex as the one documentation example of an otherwise sound
// model — the Examples carrier ir-design §6 names beside Default. Each of an
// example's value fields is a carrier of its own, so the caller says which one
// holds the value.
func exampleDoc(ex ir.Example) *ir.Document {
	m := &ir.Model{
		ID:       "t/x/M",
		Name:     named("m"),
		Examples: []ir.Example{ex},
	}
	return &ir.Document{IRVersion: ir.IRVersion, Types: ir.TypeRegistry{m.ID: m}}
}

// literalCarrierDoc embeds v as the constant value of an otherwise sound
// Literal type — the const/literal-value carrier.
func literalCarrierDoc(v ir.Value) *ir.Document {
	l := &ir.Literal{ID: "t/x/L", Name: named("l"), Value: v}
	return &ir.Document{IRVersion: ir.IRVersion, Types: ir.TypeRegistry{l.ID: l}}
}

// enumMemberCarrierDoc embeds v as the one member value of an otherwise sound
// Enum type.
func enumMemberCarrierDoc(v ir.Value) *ir.Document {
	e := &ir.Enum{
		ID:        "t/x/E",
		Name:      named("e"),
		ValueType: ir.PrimString,
		Members:   []ir.EnumMember{{Name: named("m"), Value: v}},
	}
	return &ir.Document{IRVersion: ir.IRVersion, Types: ir.TypeRegistry{e.ID: e}}
}

// otpCarrierDoc embeds v as the request tag of one OTP-bound operation, nested
// under one service and one group and reached through Document.Services rather
// than Document.Types, where a walk scoped to Types would never reach it.
func otpCarrierDoc(v ir.Value) *ir.Document {
	const ch ir.ChannelID = "c/x/Proc"
	op := ir.Operation{
		ID:   "op/x/Call",
		Name: named("call"),
		Bindings: ir.OpBindings{OTP: &ir.OTPBinding{
			Behaviour:  "gen_server",
			Kind:       "call",
			Process:    ch,
			RequestTag: &v,
		}},
	}
	return &ir.Document{
		IRVersion: ir.IRVersion,
		Channels:  map[ir.ChannelID]ir.Channel{ch: {ID: ch, Name: named("proc")}},
		Services: []ir.Service{{
			ID:   "s/x/S",
			Name: named("s"),
			Groups: []ir.OperationGroup{{
				Name:       named("g"),
				Operations: []ir.Operation{op},
			}},
		}},
	}
}

// parameterCarrierDoc embeds v as the default of one operation's one parameter,
// which like the OTP request tag is reached only through Document.Services.
func parameterCarrierDoc(v ir.Value) *ir.Document {
	op := ir.Operation{
		ID:     "op/x/Get",
		Name:   named("get"),
		Params: []ir.Parameter{{Name: named("q"), Default: &v}},
	}
	return &ir.Document{
		IRVersion: ir.IRVersion,
		Services: []ir.Service{{
			ID:   "s/x/S",
			Name: named("s"),
			Groups: []ir.OperationGroup{{
				Name:       named("g"),
				Operations: []ir.Operation{op},
			}},
		}},
	}
}

// templateArgCarrierDoc embeds v as the one value argument of a model's
// template instantiation, a TypeSpec valueof parameter.
func templateArgCarrierDoc(v ir.Value) *ir.Document {
	m := &ir.Model{
		ID:            "t/x/M",
		Name:          named("m"),
		Instantiation: &ir.TemplateInstantiation{Template: "T", Args: []ir.TemplateArg{{Value: &v}}},
	}
	return &ir.Document{IRVersion: ir.IRVersion, Types: ir.TypeRegistry{m.ID: m}}
}

// valueCarrierCase is one field the IR carries a Value in: a document that
// plants v there, and the path the walk reaches v at.
type valueCarrierCase struct {
	carrier string // "Type.Field", as irverify.ValueCarriers names it
	doc     func(v ir.Value) *ir.Document
	path    string
}

// valueCarrierCases lists every field the IR carries a Value in, which
// TestVerify_ValueCarrierCasesCoverEveryCarrier holds to the ir sources.
func valueCarrierCases() []valueCarrierCase {
	const example = "doc.Types[t/x/M].Examples[0]"
	return []valueCarrierCase{
		{carrier: "Property.Default", doc: valueDoc, path: valueDocPath},
		{
			carrier: "Parameter.Default", doc: parameterCarrierDoc,
			path: "doc.Services[0].Groups[0].Operations[0].Params[0].Default",
		},
		{carrier: "Example.Value", path: example + ".Value", doc: func(v ir.Value) *ir.Document {
			return exampleDoc(ir.Example{Name: "ex", Value: &v})
		}},
		{carrier: "Example.Headers", path: example + ".Headers", doc: func(v ir.Value) *ir.Document {
			return exampleDoc(ir.Example{Name: "ex", Headers: &v})
		}},
		{carrier: "Example.Input", path: example + ".Input", doc: func(v ir.Value) *ir.Document {
			return exampleDoc(ir.Example{Name: "ex", Input: &v})
		}},
		{carrier: "Example.Output", path: example + ".Output", doc: func(v ir.Value) *ir.Document {
			return exampleDoc(ir.Example{Name: "ex", Output: &v})
		}},
		{carrier: "ErrorExample.Content", path: example + ".Error.Content", doc: func(v ir.Value) *ir.Document {
			return exampleDoc(ir.Example{Name: "ex", Error: &ir.ErrorExample{Content: v}})
		}},
		{carrier: "Literal.Value", doc: literalCarrierDoc, path: "doc.Types[t/x/L].Value"},
		{carrier: "EnumMember.Value", doc: enumMemberCarrierDoc, path: "doc.Types[t/x/E].Members[0].Value"},
		{
			carrier: "OTPBinding.RequestTag", doc: otpCarrierDoc,
			path: "doc.Services[0].Groups[0].Operations[0].Bindings.OTP.RequestTag",
		},
		{carrier: "TemplateArg.Value", doc: templateArgCarrierDoc, path: "doc.Types[t/x/M].Instantiation.Args[0].Value"},
	}
}

// valueCodes are the three codes checkValues can report (ir/irverify/values.go).
// Filtering to them is what lets a fixture in this file skip re-proving it
// satisfies every other structural rule Verify runs: its answer about naming,
// references or bigvals is not this file's business.
var valueCodes = map[string]bool{
	"ir/unknown-value-kind":    true,
	"ir/value-missing-payload": true,
	"ir/value-stray-payload":   true,
}

// valueLoc is a Violation reduced to where it points, for cmp.Diff against a
// table's literal want. Message is free text, asserted separately with
// Contains only in the cases whose whole point is what it says.
type valueLoc struct {
	Code string
	Path string
}

// valueViolations runs Verify and keeps only the violations checkValues can
// produce, as both a cmp.Diff-able projection (locs) and the full Violations a
// case can still inspect for message content (full).
func valueViolations(doc *ir.Document) (locs []valueLoc, full []irverify.Violation) {
	for _, v := range irverify.Verify(doc) {
		if !valueCodes[v.Code] {
			continue
		}
		locs = append(locs, valueLoc{Code: v.Code, Path: v.Path})
		full = append(full, v)
	}
	return locs, full
}

// TestVerify_StrayValuePayloadIsAViolation drives ir/value-stray-payload at
// every payload field ir.Value declares, including the issue's own reproducer:
// a string value carrying a list (GitHub #506). Each case sets exactly one
// field its kind does not select, so exactly one violation is expected.
func TestVerify_StrayValuePayloadIsAViolation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		value ir.Value
		field string // the payload field the kind does not select
	}{
		{
			name:  "string carrying a list (GitHub #506)",
			value: ir.Value{Kind: ir.ValueString, Str: "hello", List: []ir.Value{{Kind: ir.ValueNull}}},
			field: "List",
		},
		{
			name:  "bool carrying a number",
			value: ir.Value{Kind: ir.ValueBool, Bool: true, Num: ir.BigVal("1")},
			field: "Num",
		},
		{
			name: "list carrying an object",
			value: ir.Value{
				Kind:   ir.ValueList,
				List:   []ir.Value{{Kind: ir.ValueNull}},
				Object: []ir.Field{{Name: "a", Value: ir.Value{Kind: ir.ValueNull}}},
			},
			field: "Object",
		},
		{
			name:  "ref carrying a constructor call",
			value: ir.Value{Kind: ir.ValueRefKind, Ref: &ir.ValueRef{}, Ctor: &ir.CtorValue{}},
			field: "Ctor",
		},
		{
			name:  "number carrying a bool",
			value: ir.Value{Kind: ir.ValueNumber, Num: ir.BigVal("1"), Bool: true},
			field: "Bool",
		},
		{
			name:  "bytes carrying a string",
			value: ir.Value{Kind: ir.ValueBytes, Bytes: []byte{0x01}, Str: "x"},
			field: "Str",
		},
		{
			name: "object carrying bytes",
			value: ir.Value{
				Kind:   ir.ValueObject,
				Object: []ir.Field{{Name: "a", Value: ir.Value{Kind: ir.ValueNull}}},
				Bytes:  []byte{0x01},
			},
			field: "Bytes",
		},
		{
			name:  "ctor carrying a ref",
			value: ir.Value{Kind: ir.ValueCtor, Ctor: &ir.CtorValue{}, Ref: &ir.ValueRef{}},
			field: "Ref",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			locs, full := valueViolations(valueDoc(tc.value))
			want := []valueLoc{{Code: "ir/value-stray-payload", Path: valueDocPath + "." + tc.field}}
			assert.Empty(t, cmp.Diff(want, locs))
			require.Len(t, full, 1)
			assert.Contains(t, full[0].Message, string(tc.value.Kind))
			assert.Contains(t, full[0].Message, tc.field)
		})
	}
}

// TestVerify_NullValueWithPayloadIsAViolation holds ir.ValueNull to the same
// contract as every other kind even though it selects no field at all: with
// nothing selected, every payload that is set is stray, not just one of them.
func TestVerify_NullValueWithPayloadIsAViolation(t *testing.T) {
	t.Parallel()
	v := ir.Value{Kind: ir.ValueNull, Bool: true, Str: "x"}
	locs, full := valueViolations(valueDoc(v))
	want := []valueLoc{
		{Code: "ir/value-stray-payload", Path: valueDocPath + ".Bool"},
		{Code: "ir/value-stray-payload", Path: valueDocPath + ".Str"},
	}
	assert.Empty(t, cmp.Diff(want, locs))
	for _, viol := range full {
		assert.Contains(t, viol.Message, "null")
	}
}

// TestVerify_NullValueWithNoPayloadIsClean is the silent half: null is a value
// in its own right, so an empty payload beside it raises no question at all,
// stray or missing.
func TestVerify_NullValueWithNoPayloadIsClean(t *testing.T) {
	t.Parallel()
	locs, _ := valueViolations(valueDoc(ir.Value{Kind: ir.ValueNull}))
	assert.Empty(t, locs)
}

// TestVerify_MissingValuePayloadIsAViolation drives ir/value-missing-payload at
// the three kinds whose zero payload is no value of that kind: a number with no
// digits, and a ref or ctor value with no reference or call at all.
func TestVerify_MissingValuePayloadIsAViolation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		value ir.Value
		field string
	}{
		{name: "number with no digits", value: ir.Value{Kind: ir.ValueNumber}, field: "Num"},
		{name: "ref with no reference", value: ir.Value{Kind: ir.ValueRefKind}, field: "Ref"},
		{name: "ctor with no call", value: ir.Value{Kind: ir.ValueCtor}, field: "Ctor"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			locs, full := valueViolations(valueDoc(tc.value))
			want := []valueLoc{{Code: "ir/value-missing-payload", Path: valueDocPath + "." + tc.field}}
			assert.Empty(t, cmp.Diff(want, locs))
			require.Len(t, full, 1)
			assert.Contains(t, full[0].Message, string(tc.value.Kind))
			assert.Contains(t, full[0].Message, tc.field)
		})
	}
}

// TestVerify_ZeroIsValueKindsAreClean is the other half of the missing-payload
// rule: a bool, string, bytes, symbol, list or object value carries a real
// value at its zero payload (false, "", no bytes, the empty atom, [] and {}),
// so an empty one is not the defect the three kinds above report.
func TestVerify_ZeroIsValueKindsAreClean(t *testing.T) {
	t.Parallel()
	for _, kind := range []ir.ValueKind{
		ir.ValueBool, ir.ValueString, ir.ValueBytes,
		ir.ValueSymbol, ir.ValueList, ir.ValueObject,
	} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			locs, _ := valueViolations(valueDoc(ir.Value{Kind: kind}))
			assert.Empty(t, locs)
		})
	}
}

// TestVerify_UnknownValueKindIsAViolation drives ir/unknown-value-kind: a kind
// outside ValueKind's declared set selects nothing, so its payloads are not
// judged at all — one violation names the kind, regardless of how many
// payloads happen to be set beside it.
func TestVerify_UnknownValueKindIsAViolation(t *testing.T) {
	t.Parallel()

	t.Run("empty kind despite payloads set", func(t *testing.T) {
		t.Parallel()
		v := ir.Value{Kind: ir.ValueKind(""), Bool: true, Str: "y", List: []ir.Value{{Kind: ir.ValueNull}}}
		locs, full := valueViolations(valueDoc(v))
		want := []valueLoc{{Code: "ir/unknown-value-kind", Path: valueDocPath + ".Kind"}}
		assert.Empty(t, cmp.Diff(want, locs))
		require.Len(t, full, 1)
		assert.Contains(t, full[0].Message, `""`)
	})

	t.Run("undeclared kind despite a payload set", func(t *testing.T) {
		t.Parallel()
		v := ir.Value{Kind: ir.ValueKind("weird"), List: []ir.Value{{Kind: ir.ValueNull}}}
		locs, full := valueViolations(valueDoc(v))
		want := []valueLoc{{Code: "ir/unknown-value-kind", Path: valueDocPath + ".Kind"}}
		assert.Empty(t, cmp.Diff(want, locs))
		require.Len(t, full, 1)
		assert.Contains(t, full[0].Message, `"weird"`)
	})

	// GitHub #400: a report must never repeat raw ill-formed bytes, so the
	// message has to stay valid UTF-8 even when the kind it quotes is not.
	t.Run("ill-formed kind keeps the message valid UTF-8 (GitHub #400)", func(t *testing.T) {
		t.Parallel()
		v := ir.Value{Kind: ir.ValueKind("\xff")}
		locs, full := valueViolations(valueDoc(v))
		want := []valueLoc{{Code: "ir/unknown-value-kind", Path: valueDocPath + ".Kind"}}
		assert.Empty(t, cmp.Diff(want, locs))
		require.Len(t, full, 1)
		assert.True(t, utf8.ValidString(full[0].Message), "message must not repeat the raw ill-formed bytes")
	})
}

// TestVerify_EmptyNonNilUnselectedSliceIsClean pins payloadSet's own contract:
// "set" means exactly what the encoder writes, and encoding/json/v2 omits a
// slice field whose length is zero regardless of nilness. Treating every
// non-nil slice as set would report a value the JSON form cannot distinguish
// from one that never populated the field at all.
func TestVerify_EmptyNonNilUnselectedSliceIsClean(t *testing.T) {
	t.Parallel()
	v := ir.Value{Kind: ir.ValueString, Str: "x", List: []ir.Value{}}
	locs, _ := valueViolations(valueDoc(v))
	assert.Empty(t, locs)
}

// TestVerify_NestedStrayPayloadIsReportedAtItsPath proves the walk continues
// below a value rather than stopping at the first one it finds: the same stray
// payload is reported at its own nested path whether it sits inside a list
// element, an object member, or a constructor argument.
func TestVerify_NestedStrayPayloadIsReportedAtItsPath(t *testing.T) {
	t.Parallel()
	stray := ir.Value{Kind: ir.ValueBool, Bool: true, Str: "x"}

	tests := []struct {
		name     string
		outer    ir.Value
		wantPath string
	}{
		{
			name:     "inside a list element",
			outer:    ir.Value{Kind: ir.ValueList, List: []ir.Value{stray}},
			wantPath: valueDocPath + ".List[0].Str",
		},
		{
			name:     "inside an object member",
			outer:    ir.Value{Kind: ir.ValueObject, Object: []ir.Field{{Name: "m", Value: stray}}},
			wantPath: valueDocPath + ".Object[0].Value.Str",
		},
		{
			name:     "inside a constructor argument",
			outer:    ir.Value{Kind: ir.ValueCtor, Ctor: &ir.CtorValue{Name: "f", Args: []ir.Value{stray}}},
			wantPath: valueDocPath + ".Ctor.Args[0].Str",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			locs, _ := valueViolations(valueDoc(tc.outer))
			want := []valueLoc{{Code: "ir/value-stray-payload", Path: tc.wantPath}}
			assert.Empty(t, cmp.Diff(want, locs))
		})
	}
}

// TestVerify_ChecksEveryValueCarrier plants the same stray payload at every
// field the IR carries a Value in, as valueCarrierCases lists them, proving the
// walk reaches each one. GitHub #506's own hazard was a payload nothing looked
// at, and a carrier the walk does not reach would fail exactly that way:
// silently.
func TestVerify_ChecksEveryValueCarrier(t *testing.T) {
	t.Parallel()
	stray := ir.Value{Kind: ir.ValueBool, Bool: true, Str: "x"}
	for _, tc := range valueCarrierCases() {
		t.Run(tc.carrier, func(t *testing.T) {
			t.Parallel()
			locs, _ := valueViolations(tc.doc(stray))
			want := []valueLoc{{Code: "ir/value-stray-payload", Path: tc.path + ".Str"}}
			assert.Empty(t, cmp.Diff(want, locs))
		})
	}
}

// TestVerify_ValueCarrierCasesCoverEveryCarrier holds valueCarrierCases to the
// ir sources: every field declared to hold a Value has a case, and no case
// names a field that is gone. A carrier with no case is one no test shows the
// walk reaching, which a hand-kept list cannot notice on its own.
func TestVerify_ValueCarrierCasesCoverEveryCarrier(t *testing.T) {
	t.Parallel()
	cases := valueCarrierCases()
	carriers := make([]string, 0, len(cases))
	for _, tc := range cases {
		carriers = append(carriers, tc.carrier)
	}
	declared := irverify.ValueCarriers(t)
	require.NotEmpty(t, declared, "the ir sources must declare Value carriers")
	assert.ElementsMatch(t, declared, carriers,
		"valueCarrierCases must plant a value in exactly the fields the ir sources declare")
}

// ctorCycleDoc builds GitHub #573's exact reproducer: a constructor value whose
// argument is a value holding the same constructor, so the walk meets that
// *ir.CtorValue at doc.Types[t/x/M].Examples[0].Value.Ctor and again from inside
// itself. Its Scalar is declared in Types so the document is otherwise sound.
func ctorCycleDoc() (*ir.Document, *ir.CtorValue) {
	s := &ir.Scalar{ID: "t/x/S", Name: ir.Naming{Source: "S", Canonical: "s"},
		Base: &ir.TypeRef{Target: "t/prim/string"}}
	prim := &ir.Primitive{ID: "t/prim/string", Prim: ir.PrimString}
	c := &ir.CtorValue{Scalar: s.ID, Name: "from"}
	v := ir.Value{Kind: ir.ValueCtor, Ctor: c}
	c.Args = []ir.Value{v} // c.Args[0].Ctor == c
	m := &ir.Model{ID: "t/x/M", Name: ir.Naming{Source: "M", Canonical: "m"},
		Examples: []ir.Example{{Value: &v}}}
	doc := &ir.Document{IRVersion: ir.IRVersion, Types: ir.TypeRegistry{s.ID: s, m.ID: m, prim.ID: prim}}
	return doc, c
}

// TestVerify_ValueCycleIsAViolation is GitHub #573's reproducer: a document
// whose value graph reaches a pointer it is already inside verifies clean and
// then cannot be encoded, the defect class #507 names. One violation reports the
// second meeting — at the walk's current path — and names the path the pointer
// was entered at.
func TestVerify_ValueCycleIsAViolation(t *testing.T) {
	t.Parallel()
	doc, _ := ctorCycleDoc()
	vs := irverify.Verify(doc)
	require.Len(t, vs, 1, "the pointer cycle is the document's one defect")
	assert.Equal(t, "ir/value-cycle", vs[0].Code)
	assert.Equal(t, "doc.Types[t/x/M].Examples[0].Value.Ctor.Args[0].Ctor", vs[0].Path)
	assert.Contains(t, vs[0].Message, strconv.Quote("doc.Types[t/x/M].Examples[0].Value.Ctor"),
		"the message must name the quoted path the pointer was entered at")
}

// TestVerify_SharedAcyclicPointerIsClean is the other side of the rule: a
// pointer two places share but that never reaches itself is not a cycle — the
// walk meets it twice off one path and the document encodes twice without
// trouble. Meeting the second example is also what trims the first example's
// frames from the on-path stack, so a stack that never popped would report it.
func TestVerify_SharedAcyclicPointerIsClean(t *testing.T) {
	t.Parallel()
	s := &ir.Scalar{ID: "t/x/S", Name: ir.Naming{Source: "S", Canonical: "s"},
		Base: &ir.TypeRef{Target: "t/prim/string"}}
	prim := &ir.Primitive{ID: "t/prim/string", Prim: ir.PrimString}
	c := &ir.CtorValue{Scalar: s.ID, Name: "from"}
	m := &ir.Model{ID: "t/x/M", Name: ir.Naming{Source: "M", Canonical: "m"},
		Examples: []ir.Example{
			{Value: &ir.Value{Kind: ir.ValueCtor, Ctor: c}},
			{Value: &ir.Value{Kind: ir.ValueCtor, Ctor: c}},
		}}
	doc := &ir.Document{IRVersion: ir.IRVersion, Types: ir.TypeRegistry{s.ID: s, m.ID: m, prim.ID: prim}}

	assert.Empty(t, irverify.Verify(doc), "a shared but acyclic pointer encodes clean")
}

// TestVerify_ValueCyclePathIsSpelledAsTheWalkWould holds the violation's path to
// the walk's own spelling: the walk hands the cycled *ir.CtorValue over at two
// paths, and the violation is the second — the meeting from inside the pointer —
// not the first.
func TestVerify_ValueCyclePathIsSpelledAsTheWalkWould(t *testing.T) {
	t.Parallel()
	doc, c := ctorCycleDoc()
	ctorType := reflect.TypeFor[*ir.CtorValue]()
	addr := reflect.ValueOf(c).Pointer()

	var meetings []string
	ir.WalkValues(doc, ir.DocumentPath, func(v reflect.Value, path string) bool {
		if v.Kind() == reflect.Pointer && v.Type() == ctorType && v.Pointer() == addr {
			meetings = append(meetings, path)
		}
		return true
	})
	require.Len(t, meetings, 2, "the walk must hand the cycled constructor over twice")

	vs := irverify.Verify(doc)
	require.Len(t, vs, 1)
	assert.Equal(t, meetings[1], vs[0].Path, "the violation must be spelled as the second meeting")
	assert.NotEqual(t, meetings[0], vs[0].Path)
}

// typeDefImplementers are the concrete types behind ir.TypeDef, which a walk over
// reflect.Type cannot reach through the interface: the marker method that seals
// the sum is unexported and ir exports no list of its kinds. So the list is
// hand-kept, and the ir package's own
// TestTypeDef_EverySealedImplementationHasAKind holds the sealed set to the
// declared TypeKind constants — a kind added there without a line here is a kind
// this survey stops covering, which is the limit the survey runs under.
var typeDefImplementers = []ir.TypeDef{
	&ir.Primitive{},
	&ir.Scalar{},
	&ir.Model{},
	&ir.Union{},
	&ir.Enum{},
	&ir.List{},
	&ir.MapT{},
	&ir.Tuple{},
	&ir.Literal{},
	&ir.External{},
	&ir.Any{},
}

// typeChildren returns the types directly reachable from t in the IR's type
// graph: a pointer, slice, array or map's element types, a struct's field types,
// and ir.TypeDef's implementers. A named type is followed by its kind, so
// ir.TypeID (a string) reaches nothing while ir.Model reaches its fields. No
// other interface is expanded: ir.TypeDef is the one interface the graph holds
// whose implementers a type walk cannot see.
func typeChildren(t reflect.Type) []reflect.Type {
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		return []reflect.Type{t.Elem()}
	case reflect.Map:
		return []reflect.Type{t.Key(), t.Elem()}
	case reflect.Struct:
		out := make([]reflect.Type, 0, t.NumField())
		for i := range t.NumField() {
			out = append(out, t.Field(i).Type)
		}
		return out
	case reflect.Interface:
		if t != reflect.TypeFor[ir.TypeDef]() {
			return nil
		}
		out := make([]reflect.Type, 0, len(typeDefImplementers))
		for _, td := range typeDefImplementers {
			out = append(out, reflect.TypeOf(td))
		}
		return out
	default:
		return nil
	}
}

// reachableFrom returns every type reachable from root through the IR's type
// graph, root included.
func reachableFrom(root reflect.Type) map[reflect.Type]bool {
	seen := map[reflect.Type]bool{}
	queue := []reflect.Type{root}
	for len(queue) > 0 {
		t := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if seen[t] {
			continue
		}
		seen[t] = true
		queue = append(queue, typeChildren(t)...)
	}
	return seen
}

// selfReachable returns, sorted, the names of the types of the given kind that a
// value of their own element type can reach — the property a cycle needs and a
// value two places merely share lacks.
func selfReachable(kind reflect.Kind) []string {
	var out []string
	for t := range reachableFrom(reflect.TypeFor[*ir.Document]()) {
		if t.Kind() != kind {
			continue
		}
		if reachableFrom(t.Elem())[t] {
			out = append(out, t.String())
		}
	}
	slices.Sort(out)
	return out
}

// TestValueCycle_OnlyCtorValueReachesItself is the executable survey behind
// checkValues' cycle rule: it walks the IR's type graph and reports which
// pointer and slice types can reach themselves, so a new self-reachable type
// reddens here rather than shipping a check that names the wrong shape.
//
// The one self-reachable pointer is ir.CtorValue, through Value.Ctor and
// CtorValue.Args. The three self-reachable slices are []ir.Value, []ir.Field and
// []ir.OperationGroup: the first two are the list mechanism GitHub #736 tracks,
// and the third is the same mechanism through the service tree, named so that
// issue's scope is not read as complete.
func TestValueCycle_OnlyCtorValueReachesItself(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{"*ir.CtorValue"}, selfReachable(reflect.Pointer),
		"a pointer that newly reaches itself needs a review of ir/value-cycle's name and its follow-up")
	assert.Equal(t, []string{"[]ir.Field", "[]ir.OperationGroup", "[]ir.Value"}, selfReachable(reflect.Slice),
		"slice cycles are out of scope for ir/value-cycle and tracked separately")
}
