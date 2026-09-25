package ir_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// versioned wraps body — the JSON document's remaining members, or "" for
// none — with this build's irVersion stamped first, for decode fixtures that
// must get past checkVersionOf before exercising the check under test.
func versioned(body string) string {
	if body == "" {
		return fmt.Sprintf(`{"irVersion":%q}`, ir.IRVersion)
	}
	return fmt.Sprintf(`{"irVersion":%q,%s}`, ir.IRVersion, body)
}

// sampleDocument builds one document that touches every TypeDef kind.
func sampleDocument(t *testing.T) ir.Document {
	t.Helper()
	mk := func(id string, td ir.TypeDef) (ir.TypeID, ir.TypeDef) {
		typeID := ir.TypeID(id)
		td.Common().ID = typeID
		return typeID, td
	}
	types := ir.TypeRegistry{}
	for _, entry := range []ir.TypeDef{
		&ir.Primitive{Prim: "string"},
		&ir.Scalar{Base: &ir.TypeRef{Target: "t/p/string"}},
		&ir.Model{Additional: "closed"},
		&ir.Union{Exclusive: true},
		&ir.Enum{ValueType: "string", Closed: true},
		&ir.List{Elem: ir.TypeRef{Target: "t/p/string"}},
		&ir.MapT{Key: ir.TypeRef{Target: "t/p/string"}, Value: ir.TypeRef{Target: "t/p/string"}},
		&ir.Tuple{Elems: []ir.TypeRef{{Target: "t/p/string"}}},
		&ir.Literal{Value: ir.Value{Kind: ir.ValueString, Str: "fixed"}},
		&ir.External{Identity: "erlang:pid"},
		&ir.Any{},
	} {
		id, td := mk("t/k/"+string(entry.Kind()), entry)
		types[id] = td
	}
	return ir.Document{IRVersion: ir.IRVersion, Name: "kinds", Version: "1", Types: types}
}

func TestDocument_JSONRoundTripAllKinds(t *testing.T) {
	t.Parallel()
	assertRoundTrip(t, sampleDocument(t))
}

func TestOperation_AuthEmptyNonNilRoundTrips(t *testing.T) {
	t.Parallel()
	// An empty non-nil Auth ("explicitly public") must survive the JSON round
	// trip distinct from nil ("inherit the service default"): nil omits the
	// key entirely (omitzero), while an empty slice is still written as [].
	op := ir.Operation{ID: "op/x", Auth: []ir.AuthRequirement{}}
	raw, err := json.Marshal(op)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"auth":[]`, "empty non-nil Auth serializes as []")

	var back ir.Operation
	require.NoError(t, json.Unmarshal(raw, &back))
	require.NotNil(t, back.Auth, "empty Auth must not deserialize to nil")
	assert.Empty(t, back.Auth)

	var nilOp ir.Operation
	rawNil, err := json.Marshal(nilOp)
	require.NoError(t, err)
	assert.NotContains(t, string(rawNil), `"auth"`, "nil Auth omits the key rather than writing null")
	var backNil ir.Operation
	require.NoError(t, json.Unmarshal(rawNil, &backNil))
	assert.Nil(t, backNil.Auth, "nil Auth stays nil")
}

func TestDocument_MarshalIsDeterministic(t *testing.T) {
	t.Parallel()
	assertDeterministicMarshal(t, sampleDocument(t))
}

func TestTypeRegistry_KindTagIsAdjacent(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(&ir.Model{ID: "t/x"})
	require.NoError(t, err)
	var probe struct {
		Kind ir.TypeKind `json:"kind"`
		ID   ir.TypeID   `json:"id"`
	}
	require.NoError(t, json.Unmarshal(raw, &probe))
	assert.Equal(t, ir.KindModel, probe.Kind)
	assert.Equal(t, ir.TypeID("t/x"), probe.ID)
}

func TestTypeRegistry_UnmarshalRejectsUnknownKind(t *testing.T) {
	t.Parallel()
	var reg ir.TypeRegistry
	err := json.Unmarshal([]byte(`{"t/x":{"kind":"bogus"}}`), &reg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bogus")
}

func TestTypeRegistry_UnmarshalErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		data    string
		wantSub string
	}{
		{
			name:    "outer_not_a_map",
			data:    `["not", "a", "map"]`,
			wantSub: "type registry:",
		},
		{
			// Both of the next two cases guard the byte-comparison
			// implementation this one is written not to be: such a guard sits
			// before the decode, and loosening it there swallows an unrelated
			// value and reports success, leaving the registry silently unset.
			// A length test takes this one. Neither can fire against the guard
			// as written, which sits after the decode, where a non-object has
			// already produced its own error.
			name:    "outer_is_a_non_null_scalar",
			data:    `true`,
			wantSub: "type registry:",
		},
		{
			// A pre-decode guard matching "null" as a substring rather than as
			// the whole document takes this one.
			name:    "outer_is_the_string_null",
			data:    `"null"`,
			wantSub: "type registry:",
		},
		{
			name:    "entry_not_an_object",
			data:    `{"t/x":123}`,
			wantSub: "reading kind tag:",
		},
		{
			name:    "entry_has_no_kind_member",
			data:    `{"t/x":{}}`,
			wantSub: `no "kind" member`,
		},
		{
			name:    "entry_kind_is_not_a_string",
			data:    `{"t/x":{"kind":123}}`,
			wantSub: `"kind" is a JSON number, not a string`,
		},
		{
			name:    "unknown_kind",
			data:    `{"t/x":{"kind":"nope"}}`,
			wantSub: `unknown kind "nope"`,
		},
		{
			name:    "concrete_body_type_mismatch",
			data:    `{"t/x":{"kind":"primitive","prim":123}}`,
			wantSub: "t/x (primitive):",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var reg ir.TypeRegistry
			err := json.Unmarshal([]byte(tt.data), &reg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantSub)
		})
	}
}

// TestTypeRegistry_UnmarshalNullIsNoOp locks in the json.Unmarshaler
// convention issue #46 asks for: the json package calls UnmarshalJSONFrom
// even for a literal null, and the implementation treats that as a no-op
// instead of allocating an empty map, the same way time.Time.UnmarshalJSON
// does.
//
// The padded case decodes whitespace around the literal — still a valid JSON
// encoding of null, and the spelling a byte
// comparison against "null" would miss.
//
// Where the no-op convention and plain-map semantics part ways is a null
// decoded over an already-populated destination: this registry keeps its
// entries, while the stdlib nils a plain map such as Document.Channels. #46
// chose the Unmarshaler convention. The difference cannot arise for a decode
// into a fresh Document — what internal/harness's round-trip oracle does — and
// there the two agree on nil.
func TestTypeRegistry_UnmarshalNullIsNoOp(t *testing.T) {
	t.Parallel()

	t.Run("zero_value_stays_nil", func(t *testing.T) {
		t.Parallel()
		var reg ir.TypeRegistry
		require.NoError(t, json.Unmarshal([]byte(`null`), &reg))
		assert.Nil(t, reg)
	})

	t.Run("padded_null_stays_nil", func(t *testing.T) {
		t.Parallel()
		var reg ir.TypeRegistry
		require.NoError(t, json.Unmarshal([]byte(" \n null \n "), &reg))
		assert.Nil(t, reg)
	})

	t.Run("populated_registry_is_untouched", func(t *testing.T) {
		t.Parallel()
		reg := ir.TypeRegistry{"t/x": &ir.Any{}}
		require.NoError(t, json.Unmarshal([]byte(`null`), &reg))
		assert.Len(t, reg, 1, "a null decode must not clear an existing registry")
	})
}

// TestDocument_TypesNullRoundTrips reproduces issue #46: a document whose
// "types" key is spelled as JSON null, rather than omitted, must decode to
// the same nil TypeRegistry that omitting the key produces — the fixed
// point invariant 7 promises for golden diffing and the planned caching
// layer. Before the fix, the first decode left Types as a non-nil empty map;
// since Document.Types carries omitempty, that map vanished on marshal and
// came back nil on the next decode, so the same document read differently
// depending on how many round trips it had already been through.
func TestDocument_TypesNullRoundTrips(t *testing.T) {
	t.Parallel()

	var doc ir.Document
	require.NoError(t, json.Unmarshal([]byte(versioned(`"types":null`)), &doc))
	require.Nil(t, doc.Types, `a null "types" key must decode to nil, not an empty map`)

	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), `"types"`, "omitempty must drop a nil Types field")

	var doc2 ir.Document
	require.NoError(t, json.Unmarshal(raw, &doc2))
	assert.Empty(t, cmp.Diff(doc, doc2), "decoding must reach a fixed point after one round trip")
}

// TestDocument_EmptyMapFieldsCollapseUniformly draws the line issue #46 stops
// at. The null fix makes "types":null behave like the sibling registry fields
// already did: nil in, nil out. It deliberately does not make the empty object
// durable — "types":{} still decodes to a non-nil, empty registry that
// omitempty drops on marshal, so a second decode yields nil. The table pins
// that this is generic map/omitempty behavior rather than a TypeRegistry
// defect: Channels, Messages and Auth have no custom UnmarshalJSONFrom at all
// and collapse identically.
//
// Leaving it follows the standard #47 already recorded for the IR's omitempty
// collections: the collapse is a defect only where nil and empty denote
// different things, as on the
// []AuthRequirement fields (Operation.Auth, Server.Auth, Service.Auth) — not
// on the Document.Auth scheme registry exercised below, where an empty
// registry and an absent one both mean a document that declares none.
// Changing it for every optional map field would be a separate, repo-wide
// change.
func TestDocument_EmptyMapFieldsCollapseUniformly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
	}{
		{name: "types", data: versioned(`"types":{}`)},
		{name: "channels", data: versioned(`"channels":{}`)},
		{name: "messages", data: versioned(`"messages":{}`)},
		{name: "auth", data: versioned(`"auth":{}`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var doc ir.Document
			require.NoError(t, json.Unmarshal([]byte(tt.data), &doc))

			raw, err := json.Marshal(doc)
			require.NoError(t, err)

			var doc2 ir.Document
			require.NoError(t, json.Unmarshal(raw, &doc2))
			assert.NotEmpty(t, cmp.Diff(doc, doc2),
				"%s: empty-object collapse-on-remarshal is shared map/omitempty behavior, not a TypeRegistry-specific "+
					"defect — a field that stops collapsing has diverged from the registry fields next to it", tt.name)
		})
	}
}

// lenientDecodeOptions are options that would, for a plain v2 decode, relax
// exactly the checks TestDocument_UnmarshalRefusesMalformedInput exercises:
// duplicate names, invalid UTF-8, case-sensitive name matching, and unknown
// members. Passing them alongside every case in that table is what proves a
// refusal belongs to the document rather than to whatever the caller asked
// for — UnmarshalJSONFrom re-decodes under the IR's own options regardless
// (json.go).
var lenientDecodeOptions = []json.Options{
	jsontext.AllowDuplicateNames(true),
	jsontext.AllowInvalidUTF8(true),
	json.MatchCaseInsensitiveNames(true),
	json.RejectUnknownMembers(false),
}

// TestDocument_UnmarshalRefusesMalformedInput pins every refusal
// UnmarshalJSONFrom makes: an absent or incompatible irVersion — read before
// anything else, so it is reported even when other members are also
// unrecognized — a non-object document, an unknown member (at the document's
// own level and inside a nested TypeDef body), and a duplicate member name
// (top-level and nested).
// Every case runs twice: once under default options, once under
// lenientDecodeOptions, since the guarantee is that the caller's options
// cannot relax any of them.
func TestDocument_UnmarshalRefusesMalformedInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		data    string
		wantSub string
		wantErr error // checked with errors.Is when non-nil
	}{
		{
			name:    "no irVersion",
			data:    `{"name":"x"}`,
			wantSub: "no irVersion",
			wantErr: ir.ErrVersionAbsent,
		},
		{
			// stringMember scans the raw bytes for the literal key "irVersion";
			// it does not consult MatchCaseInsensitiveNames, so a differently
			// cased key is absent even under lenientDecodeOptions.
			name:    "irVersion spelled in a different case is still absent",
			data:    `{"IRVERSION":"0.5.0"}`,
			wantSub: "no irVersion",
			wantErr: ir.ErrVersionAbsent,
		},
		{
			name:    "incompatible irVersion",
			data:    `{"irVersion":"9.9.9"}`,
			wantSub: `declares irVersion "9.9.9"`,
			wantErr: ir.ErrVersionIncompatible,
		},
		{
			name: "incompatible irVersion wins over an unknown member",
			data: `{"irVersion":"9.9.9","bogus":1}`,
			// The version is read before any other member (json.go), so an
			// incompatible version is reported even though "bogus" would also
			// fail RejectUnknownMembers — never an unknown-member error instead.
			wantSub: `declares irVersion "9.9.9"`,
			wantErr: ir.ErrVersionIncompatible,
		},
		{
			name:    "null document",
			data:    `null`,
			wantSub: "want a JSON object, got null",
		},
		{
			name:    "array document",
			data:    `[]`,
			wantSub: "want a JSON object, got [",
		},
		{
			name:    "unknown member at document level",
			data:    versioned(`"bogus":1`),
			wantSub: `unknown object member name "bogus"`,
		},
		{
			name:    "unknown member inside a TypeDef body",
			data:    versioned(`"types":{"t/x":{"kind":"primitive","prim":"string","bogus":1}}`),
			wantSub: "ir: type t/x (primitive):",
		},
		{
			name:    "duplicate member name at document level",
			data:    versioned(`"name":"a","name":"b"`),
			wantSub: `duplicate object member name "name"`,
		},
		{
			name:    "duplicate member name inside a TypeDef body",
			data:    versioned(`"types":{"t/x":{"kind":"primitive","prim":"string","prim":"int32"}}`),
			wantSub: `duplicate object member name "prim"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var doc ir.Document
			err := json.Unmarshal([]byte(tt.data), &doc)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantSub)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			}
		})
		t.Run(tt.name+"/caller options are ignored", func(t *testing.T) {
			t.Parallel()
			var doc ir.Document
			err := json.Unmarshal([]byte(tt.data), &doc, lenientDecodeOptions...)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantSub)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			}
		})
	}
}

// TestDocument_UnmarshalRefusesInvalidUTF8 pins that a string containing
// invalid UTF-8 is refused during decode, even when the caller explicitly
// allows it: the document is re-decoded under the IR's own strict options
// regardless (json.go). The fixture is built with a raw \xff escape — a
// literal invalid byte a Go string can hold even though it is not valid
// UTF-8 — rather than as a backtick string, which would not interpret the
// escape at all.
func TestDocument_UnmarshalRefusesInvalidUTF8(t *testing.T) {
	t.Parallel()
	data := []byte("{\"irVersion\":\"" + ir.IRVersion + "\",\"name\":\"bad\xffname\"}")
	tests := []struct {
		name string
		opts []json.Options
	}{
		{"default options", nil},
		{"caller allows invalid UTF-8", []json.Options{jsontext.AllowInvalidUTF8(true)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var doc ir.Document
			err := json.Unmarshal(data, &doc, tc.opts...)
			require.Error(t, err)
			assert.ErrorContains(t, err, "invalid UTF-8")
		})
	}
}

// unsortedTypesDoc returns a Document for the encode tests below: Types entries
// inserted out of sorted order, a Name that HTML and JS escaping would rewrite, a
// channel binding holding a nil RawConfig, and an Unmodeled payload spelled the
// way each raw-value option would respell it — an integer past float64's
// precision, a float with a trailing zero, members out of order, and an escape
// the canonical form drops.
func unsortedTypesDoc() ir.Document {
	types := ir.TypeRegistry{}
	for _, id := range []ir.TypeID{"t/z", "t/a", "t/m"} {
		types[id] = &ir.Primitive{ID: id, Prim: ir.PrimString}
	}
	return ir.Document{IRVersion: ir.IRVersion, Name: "a<b>c&d\u2028e", Types: types,
		Channels: map[ir.ChannelID]ir.Channel{"c/x": {ID: "c/x", Bindings: map[string]ir.RawConfig{"kafka": nil}}},
		Unmodeled: ir.Unmodeled{"openapi:x-probe": {
			Reason: ir.ReasonVendorExtension,
			Value:  ir.RawValue(`{"z":12345678901234567890,"a":1.50,"s":"a\/b"}`),
		}},
	}
}

// TestDocument_MarshalIgnoresCallerOptions pins that every caller option which
// could change Document's encoded bytes is overridden by canonicalOptions
// (json.go). Each option changes the bytes of unsortedTypesDoc when it is not
// pinned; each case must still produce bytes identical to a plain
// json.Marshal(doc) with no options at all. FormatNilSliceAsNull is pinned too
// but has no case: no IR type holds a nil slice the encoder reaches without an
// omission option, so no document can show it.
func TestDocument_MarshalIgnoresCallerOptions(t *testing.T) {
	t.Parallel()
	doc := unsortedTypesDoc()
	plain, err := json.Marshal(&doc)
	require.NoError(t, err)
	// The canonical value of each pinned option, read off the plain encoding: the
	// comparisons below prove only that a caller cannot move the bytes, so a pin
	// set to the wrong value would pass them.
	for _, want := range []string{
		"\"name\":\"a<b>c&d\u2028e\"",                           // no HTML or JS escaping
		`"value":{"z":12345678901234567890,"a":1.50,"s":"a/b"}`, // raw spellings and order kept, escapes minimal
		`"bindings":{"kafka":{}}`,                               // a nil map is {}, never null
		`"source":0`,                                            // numbers are not stringified
		`"docs":{}`,                                             // a zero struct is not dropped
	} {
		assert.Contains(t, string(plain), want)
	}

	tests := []struct {
		name string
		opt  json.Options
	}{
		{"EscapeForHTML", jsontext.EscapeForHTML(true)},
		{"EscapeForJS", jsontext.EscapeForJS(true)},
		{"FormatNilMapAsNull", json.FormatNilMapAsNull(true)},
		{"StringifyNumbers", json.StringifyNumbers(true)},
		{"OmitZeroStructFields", json.OmitZeroStructFields(true)},
		{"WithMarshalers", json.WithMarshalers(json.MarshalFunc(func(ir.TypeID) ([]byte, error) {
			return []byte(`"replaced"`), nil
		}))},
		{"CanonicalizeRawInts", jsontext.CanonicalizeRawInts(true)},
		{"CanonicalizeRawFloats", jsontext.CanonicalizeRawFloats(true)},
		{"ReorderRawObjects", jsontext.ReorderRawObjects(true)},
		{"PreserveRawStrings", jsontext.PreserveRawStrings(true)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := json.Marshal(&doc, tt.opt)
			require.NoError(t, err)
			assert.Equal(t, string(plain), string(got), "%s must not change the encoded bytes", tt.name)
		})
	}
}

// TestDocument_MarshalRefusesInvalidUTF8RegardlessOfCaller pins that
// canonicalOptions' AllowInvalidUTF8(false) cannot be overridden: a Document
// with invalid UTF-8 in a string field fails to encode even when the caller
// explicitly passes jsontext.AllowInvalidUTF8(true).
func TestDocument_MarshalRefusesInvalidUTF8RegardlessOfCaller(t *testing.T) {
	t.Parallel()
	doc := ir.Document{IRVersion: ir.IRVersion, Name: "bad\xffname"}
	tests := []struct {
		name string
		opts []json.Options
	}{
		{"default options", nil},
		{"caller allows invalid UTF-8", []json.Options{jsontext.AllowInvalidUTF8(true)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := json.Marshal(&doc, tc.opts...)
			require.Error(t, err)
			assert.ErrorContains(t, err, "invalid UTF-8")
		})
	}
}

// TestDocument_MarshalRefusesDuplicateNamesRegardlessOfCaller pins that
// canonicalOptions' AllowDuplicateNames(false) cannot be overridden: a raw
// payload naming one member twice is ambiguous, so the document refuses to
// encode it even when the caller would allow it.
func TestDocument_MarshalRefusesDuplicateNamesRegardlessOfCaller(t *testing.T) {
	t.Parallel()
	doc := ir.Document{IRVersion: ir.IRVersion, Unmodeled: ir.Unmodeled{"openapi:x-dup": {
		Reason: ir.ReasonVendorExtension, Value: ir.RawValue(`{"a":1,"a":2}`),
	}}}
	tests := []struct {
		name string
		opts []json.Options
	}{
		{"default options", nil},
		{"caller allows duplicate names", []json.Options{jsontext.AllowDuplicateNames(true)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := json.Marshal(&doc, tc.opts...)
			assert.ErrorIs(t, err, jsontext.ErrDuplicateName)
		})
	}
}

// TestDocument_MarshalSortsMapKeysWithoutBeingAsked pins that a plain
// json.Marshal(doc), with no Deterministic option from the caller, still
// emits Types in sorted-key order: canonicalOptions forces it (invariant 7).
func TestDocument_MarshalSortsMapKeysWithoutBeingAsked(t *testing.T) {
	t.Parallel()
	doc := unsortedTypesDoc()
	raw, err := json.Marshal(&doc)
	require.NoError(t, err)
	got := string(raw)
	aIdx, mIdx, zIdx := strings.Index(got, `"t/a"`), strings.Index(got, `"t/m"`), strings.Index(got, `"t/z"`)
	require.True(t, aIdx >= 0 && mIdx >= 0 && zIdx >= 0, "all three type IDs must appear: %s", got)
	assert.True(t, aIdx < mIdx && mIdx < zIdx, "Types must marshal in sorted-key order without Deterministic being asked for: %s", got)
}

// TestDocument_MarshalHonoursCallerIndent pins the one caller option
// MarshalJSONTo does not override: formatting. jsontext.WithIndent produces
// multiline output that, once compacted, is byte-identical to the plain
// (unindented) canonical encoding.
func TestDocument_MarshalHonoursCallerIndent(t *testing.T) {
	t.Parallel()
	doc := unsortedTypesDoc()
	plain, err := json.Marshal(&doc)
	require.NoError(t, err)

	indented, err := json.Marshal(&doc, jsontext.WithIndent("  "))
	require.NoError(t, err)
	assert.Contains(t, string(indented), "\n  \"name\"", "indent must actually take effect")

	compacted := jsontext.Value(indented)
	require.NoError(t, compacted.Compact())
	assert.Equal(t, string(plain), string(compacted), "indentation must be the only difference from the plain encoding")
}

// TestTypeDef_UnmarshalRefusesForeignKindTag pins that a concrete kind
// refuses a "kind" tag naming any other kind, rather than silently adopting
// it: json.go's unmarshalKinded compares the tag against its own constant.
func TestTypeDef_UnmarshalRefusesForeignKindTag(t *testing.T) {
	t.Parallel()
	var m ir.Model
	err := json.Unmarshal([]byte(`{"kind":"enum"}`), &m)
	require.Error(t, err)
	assert.ErrorContains(t, err, `model carries the kind tag "enum"`)
}

// TestTypeRegistry_UnmarshalNamesTheFirstBadEntryByID pins the decode order: with
// two bad entries the error names the one first in ID order, on every run. A
// map-ordered decode names either, so twenty runs would all agree only by chance.
func TestTypeRegistry_UnmarshalNamesTheFirstBadEntryByID(t *testing.T) {
	t.Parallel()
	data := []byte(`{"t/b":{"kind":"nope"},"t/a":{"kind":"nope"}}`)
	for range 20 {
		var reg ir.TypeRegistry
		err := json.Unmarshal(data, &reg)
		require.Error(t, err)
		assert.ErrorContains(t, err, "ir: type t/a:")
	}
}

// TestTypeDef_UnknownMemberErrorNamesThePublicType pins that an
// unknown-member error on a concrete TypeDef kind names the public type
// (ir.Model), not the internal kinded[T] wrapper it decoded through:
// json.go's named rewrite.
func TestTypeDef_UnknownMemberErrorNamesThePublicType(t *testing.T) {
	t.Parallel()
	var m ir.Model
	err := json.Unmarshal([]byte(`{"kind":"model","bogus":1}`), &m, json.RejectUnknownMembers(true))
	require.Error(t, err)
	serr, ok := errors.AsType[*json.SemanticError](err)
	require.True(t, ok, "want a *json.SemanticError in the chain, got %T: %v", err, err)
	assert.Equal(t, reflect.TypeFor[ir.Model](), serr.GoType,
		"the error must name ir.Model, not the internal kinded[...] wrapper")
}

// TestDocument_UnknownMemberErrorNamesThePublicType is the same guarantee for the
// Document itself, which decodes through an alias of its own: an unknown member
// at the top level must be reported against ir.Document.
func TestDocument_UnknownMemberErrorNamesThePublicType(t *testing.T) {
	t.Parallel()
	var doc ir.Document
	err := json.Unmarshal([]byte(`{"irVersion":"`+ir.IRVersion+`","bogus":1}`), &doc)
	require.Error(t, err)
	serr, ok := errors.AsType[*json.SemanticError](err)
	require.True(t, ok, "want a *json.SemanticError in the chain, got %T: %v", err, err)
	assert.Equal(t, reflect.TypeFor[ir.Document](), serr.GoType,
		"the error must name ir.Document, not the alias it decodes through")
}

// TestDocument_ForeignVersionIsQuotedBounded pins that a refused irVersion is
// echoed into the error only up to a fixed length: the stamp is the one member
// read before anything is checked, so its size is the document's to choose.
func TestDocument_ForeignVersionIsQuotedBounded(t *testing.T) {
	t.Parallel()
	var doc ir.Document
	err := json.Unmarshal([]byte(`{"irVersion":"`+strings.Repeat("9", 100_000)+`"}`), &doc)
	require.ErrorIs(t, err, ir.ErrVersionIncompatible)
	assert.Less(t, len(err.Error()), 300, "the error must not echo the whole stamp")
}
