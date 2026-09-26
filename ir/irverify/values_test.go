package irverify_test

import (
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

// exampleCarrierDoc embeds v as the one documentation example of an otherwise
// sound model — the Examples carrier ir-design §6 names beside Default.
func exampleCarrierDoc(v ir.Value) *ir.Document {
	m := &ir.Model{
		ID:       "t/x/M",
		Name:     named("m"),
		Examples: []ir.Example{{Name: "ex", Value: &v}},
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
// than Document.Types — the one carrier above that a walk scoped to Types would
// never reach at all.
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
// digits, and a ref or ctor naming nothing.
func TestVerify_MissingValuePayloadIsAViolation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		value ir.Value
		field string
	}{
		{name: "number with no digits", value: ir.Value{Kind: ir.ValueNumber}, field: "Num"},
		{name: "ref naming nothing", value: ir.Value{Kind: ir.ValueRefKind}, field: "Ref"},
		{name: "ctor naming nothing", value: ir.Value{Kind: ir.ValueCtor}, field: "Ctor"},
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

// TestVerify_ChecksEveryValueCarrier plants the same stray payload at each
// field the IR carries a Value or *Value in — a default, an example, a
// const/literal value, an enum member value, and an OTP request tag — proving
// the walk reaches every one rather than a hand-picked subset. GitHub #506's
// own hazard was a payload nothing looked at, and a carrier the walk does not
// reach would fail exactly that way: silently.
func TestVerify_ChecksEveryValueCarrier(t *testing.T) {
	t.Parallel()
	stray := ir.Value{Kind: ir.ValueBool, Bool: true, Str: "x"}

	tests := []struct {
		name     string
		doc      *ir.Document
		wantPath string
	}{
		{name: "Property.Default", doc: valueDoc(stray), wantPath: valueDocPath},
		{name: "Example.Value", doc: exampleCarrierDoc(stray), wantPath: "doc.Types[t/x/M].Examples[0].Value"},
		{name: "Literal.Value", doc: literalCarrierDoc(stray), wantPath: "doc.Types[t/x/L].Value"},
		{name: "EnumMember.Value", doc: enumMemberCarrierDoc(stray), wantPath: "doc.Types[t/x/E].Members[0].Value"},
		{
			name:     "OTPBinding.RequestTag",
			doc:      otpCarrierDoc(stray),
			wantPath: "doc.Services[0].Groups[0].Operations[0].Bindings.OTP.RequestTag",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			locs, _ := valueViolations(tc.doc)
			want := []valueLoc{{Code: "ir/value-stray-payload", Path: tc.wantPath + ".Str"}}
			assert.Empty(t, cmp.Diff(want, locs))
		})
	}
}
