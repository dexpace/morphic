// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"encoding/json/jsontext"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

func TestEnum_StringClosed(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    E:
      type: string
      enum: [a, b]
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	e, ok := doc.Types[componentID("E")].(*ir.Enum)
	require.True(t, ok, "E should be an enum")
	assert.True(t, e.Closed, "JSON Schema enum is closed")
	assert.Equal(t, ir.PrimString, e.ValueType)
	require.Len(t, e.Members, 2)
	assert.Equal(t, "a", e.Members[0].Name.Source)
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "a"}, e.Members[0].Value)
	assert.Equal(t, "b", e.Members[1].Name.Source)
}

// TestEnum_NullMemberNormalizesToNullable pins ir-design §3.3 for the canonical
// spelling of a nullable enum: on a schema that admits null in its own right,
// the `null` member is stripped and carried by the enclosing reference's
// Nullable bit, leaving a closed Enum of the scalar members (GitHub #44).
//
// The leading-null rows are not duplicates of the trailing-null ones: member
// kinds were reconciled against the member at index 0, so a leading `null`
// fixed a kind no scalar member could match. The rows differ only in where the
// null sits.
func TestEnum_NullMemberNormalizesToNullable(t *testing.T) {
	t.Parallel()
	const enumID = ir.TypeID("t/anon/components/schemas/S/properties/p")
	strMembers := []ir.Value{
		{Kind: ir.ValueString, Str: "red"},
		{Kind: ir.ValueString, Str: "green"},
	}
	numMembers := []ir.Value{
		{Kind: ir.ValueNumber, Num: ir.BigVal("1")},
		{Kind: ir.ValueNumber, Num: ir.BigVal("2")},
	}
	cases := []struct {
		name, version, schema string
		wantValueType         ir.PrimKind
		wantMembers           []ir.Value
	}{
		{
			name:          "3.1 type array, trailing null",
			version:       "3.1.0",
			schema:        `{type: [string, "null"], enum: [red, green, null]}`,
			wantValueType: ir.PrimString,
			wantMembers:   strMembers,
		},
		{
			name:          "3.1 type array, leading null",
			version:       "3.1.0",
			schema:        `{type: [string, "null"], enum: [null, red, green]}`,
			wantValueType: ir.PrimString,
			wantMembers:   strMembers,
		},
		{
			name:          "3.0 nullable keyword, trailing null",
			version:       "3.0.3",
			schema:        `{type: string, nullable: true, enum: [red, green, null]}`,
			wantValueType: ir.PrimString,
			wantMembers:   strMembers,
		},
		{
			// No type keyword to read a value type from, so the Enum's ValueType
			// comes from the members that were kept — never from the null that a
			// leading position would otherwise have classified the set by.
			name:          "3.0 nullable keyword with no type, leading null",
			version:       "3.0.3",
			schema:        `{nullable: true, enum: [null, 1, 2]}`,
			wantValueType: ir.PrimNumber,
			wantMembers:   numMembers,
		},
		{
			// A bare enum: nothing beside the members says whether null is in the
			// value space, so the members say it themselves and the set normalizes
			// exactly as its type-array spelling does. It used to keep the whole
			// enum on the union-of-literals fallback (GitHub #265).
			name:          "bare enum, trailing null",
			version:       "3.1.0",
			schema:        `{enum: [red, green, null]}`,
			wantValueType: ir.PrimString,
			wantMembers:   strMembers,
		},
		{
			name:          "bare enum, leading null",
			version:       "3.1.0",
			schema:        `{enum: [null, 1, 2]}`,
			wantValueType: ir.PrimNumber,
			wantMembers:   numMembers,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := schema.LowerSpec(t, enumPropertySpec(tc.version, tc.schema))
			openapitest.RequireNoErrorDiags(t, diags)
			assert.False(t, openapitest.HasDiag(diags, diag.DegradedConstruct),
				"a normalized nullable enum is not a degradation; got %+v", diags)

			m, ok := doc.Types[componentID("S")].(*ir.Model)
			require.True(t, ok, "S is a model")
			require.Len(t, m.Properties, 1)
			assert.True(t, m.Properties[0].Type.Nullable,
				"the stripped null member is carried by the property's reference")
			assert.Equal(t, enumID, m.Properties[0].Type.Target)

			e, ok := doc.Types[enumID].(*ir.Enum)
			require.True(t, ok, "the property lowers to an Enum, not a union of literals")
			assert.True(t, e.Closed, "a JSON Schema enum stays closed")
			assert.Equal(t, tc.wantValueType, e.ValueType)

			got := make([]ir.Value, 0, len(e.Members))
			for _, member := range e.Members {
				assert.NotEmpty(t, member.Name.Source, "every kept member is named")
				got = append(got, member.Value)
			}
			if diff := cmp.Diff(tc.wantMembers, got); diff != "" {
				t.Errorf("enum members (-want +got):\n%s", diff)
			}
		})
	}
}

// TestEnum_NullMemberKeepsUnionFallback pins the other half of #44: a member
// set the normalization must not touch still lowers to a union of literals,
// with its info diagnostic and every member preserved.
//
// The last two rows bound the rule: a type keyword excluding null conjoins with
// the enum, so its `null` member admits nothing and normalizing would widen the
// declared type; an all-null set has no member left to build an Enum from.
//
// wantNullable is asserted beside the variants because the two must agree: a
// fallback keeping the null while the reference denies it leaves a position an
// emitter cannot generate.
func TestEnum_NullMemberKeepsUnionFallback(t *testing.T) {
	t.Parallel()
	null := ir.Value{Kind: ir.ValueNull}
	cases := []struct {
		name, version, schema string
		wantVariants          []ir.Value
		wantNullable          bool
	}{
		{
			name:    "heterogeneous members beside a null member",
			version: "3.1.0",
			schema:  `{type: [string, "null"], enum: [1, a, null]}`,
			wantVariants: []ir.Value{
				{Kind: ir.ValueNumber, Num: ir.BigVal("1")},
				{Kind: ir.ValueString, Str: "a"},
				null,
			},
			wantNullable: true,
		},
		{
			// The bare spelling of the row above: no type keyword, so the members
			// decide, and they say the same thing the type array did.
			name:    "heterogeneous members, no type keyword",
			version: "3.1.0",
			schema:  `{enum: [1, a, null]}`,
			wantVariants: []ir.Value{
				{Kind: ir.ValueNumber, Num: ir.BigVal("1")},
				{Kind: ir.ValueString, Str: "a"},
				null,
			},
			wantNullable: true,
		},
		{
			name:    "type keyword excludes null",
			version: "3.1.0",
			schema:  `{type: string, enum: [red, green, null]}`,
			wantVariants: []ir.Value{
				{Kind: ir.ValueString, Str: "red"},
				{Kind: ir.ValueString, Str: "green"},
				null,
			},
			wantNullable: false,
		},
		{
			name:         "every member is null",
			version:      "3.1.0",
			schema:       `{type: ["null"], enum: [null]}`,
			wantVariants: []ir.Value{null},
			wantNullable: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := schema.LowerSpec(t, enumPropertySpec(tc.version, tc.schema))
			openapitest.RequireNoErrorDiags(t, diags)
			assert.True(t, openapitest.HasDiagAt(diags, diag.DegradedConstruct, ir.SeverityInfo),
				"the degraded-enum diagnostic still fires; got %+v", diags)

			owner, ok := doc.Types[componentID("S")].(*ir.Model)
			require.True(t, ok, "S is a model")
			require.Len(t, owner.Properties, 1)
			assert.Equal(t, tc.wantNullable, owner.Properties[0].Type.Nullable,
				"the reference agrees with the members the fallback kept")

			u, ok := doc.Types[ir.TypeID("t/anon/components/schemas/S/properties/p")].(*ir.Union)
			require.True(t, ok, "the property lowers to a union of literals")
			require.Len(t, u.Variants, len(tc.wantVariants))

			got := make([]ir.Value, 0, len(u.Variants))
			for _, v := range u.Variants {
				lit, isLit := doc.Types[v.Type.Target].(*ir.Literal)
				require.True(t, isLit, "every variant is a hoisted Literal")
				got = append(got, lit.Value)
			}
			if diff := cmp.Diff(tc.wantVariants, got); diff != "" {
				t.Errorf("every declared member survives as a literal (-want +got):\n%s", diff)
			}
		})
	}
}

// TestEnum_EmptyMemberListMatchesNoValue pins what `enum: []` lowers to. It is
// legal JSON Schema and matches no instance, so the position it is written at
// accepts nothing: a closed Enum with no member, reported once (GitHub #278).
//
// The rows are the positions the keyword can sit at, since what the position
// admits otherwise depends on its siblings: a type keyword, a property set, a
// composition, a nullable type array. The last row is the control: a populated
// enum must lower as before, so a fix reaching every enum rather than the empty
// one fails here.
func TestEnum_EmptyMemberListMatchesNoValue(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, schema  string
		wantValueType ir.PrimKind
		wantMembers   int
		wantEmpty     bool
		wantKept      string
	}{
		{
			name:          "bare",
			schema:        `    S: {enum: []}`,
			wantValueType: ir.PrimAny,
			wantEmpty:     true,
		},
		{
			name:          "beside a type keyword",
			schema:        `    S: {type: string, enum: []}`,
			wantValueType: ir.PrimString,
			wantEmpty:     true,
		},
		{
			name: "beside a property set",
			schema: `    S:
      type: object
      properties: {x: {type: string}}
      enum: []`,
			wantValueType: ir.PrimAny,
			wantEmpty:     true,
			wantKept:      "openapi:properties",
		},
		{
			name: "beside an allOf",
			schema: `    Base: {type: object, properties: {x: {type: string}}}
    S:
      allOf: [{$ref: '#/components/schemas/Base'}]
      enum: []`,
			wantValueType: ir.PrimAny,
			wantEmpty:     true,
			wantKept:      "openapi:allOf",
		},
		{
			name:          "beside a nullable type array",
			schema:        `    S: {type: [string, "null"], enum: []}`,
			wantValueType: ir.PrimString,
			wantEmpty:     true,
		},
		{
			name:          "a populated enum is untouched",
			schema:        `    S: {type: string, enum: [red, green]}`,
			wantValueType: ir.PrimString,
			wantMembers:   2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(tc.schema+"\n"))
			openapitest.RequireNoErrorDiags(t, diags)

			e, ok := typeByName(doc, "S").(*ir.Enum)
			require.True(t, ok, "the enum keyword lowers to an Enum, got %T", typeByName(doc, "S"))
			assert.True(t, e.Closed, "an enum admits its members and nothing else")
			assert.Len(t, e.Members, tc.wantMembers, "the member list is the declared one")
			assert.Equal(t, tc.wantValueType, e.ValueType)

			want := 0
			if tc.wantEmpty {
				want = 1
			}
			assert.Equal(t, want, openapitest.CountDiagsAt(diags, diag.EmptyEnum, ir.SeverityWarning),
				"a value space with no member is reported once; got %+v", diags)

			if tc.wantKept == "" {
				return
			}
			assert.Contains(t, e.Unmodeled, tc.wantKept,
				"the keyword the election passed over stays beside the enum")
		})
	}
}

// TestEnum_EmptyMemberListIsAUnionSibling pins the same keyword at the one
// position that reads it through a different predicate. A oneOf/anyOf can be
// the whole type only when nothing structural is written beside it, and an
// empty enum is a value set like any other, so the union conjoins with a set
// holding no member and lowering the union alone would state the opposite of
// the source.
//
// It is a second case rather than a second row because declaresShape and
// composesAsModel test the keyword themselves, through enumWritten, and a union
// reaches lower()'s dispatch only through them.
func TestEnum_EmptyMemberListIsAUnionSibling(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    S:
      enum: []
      oneOf: [{type: string}, {type: integer}]
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	e, ok := typeByName(doc, "S").(*ir.Enum)
	require.True(t, ok, "the empty value set is the shape the position lowers to, got %T",
		typeByName(doc, "S"))
	assert.Empty(t, e.Members)
	assert.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.EmptyEnum, ir.SeverityWarning),
		"reported once here too; got %+v", diags)
	assert.Contains(t, e.Unmodeled, "openapi:oneOf",
		"the union the enum cannot carry stays beside it rather than replacing it")
}

func TestConst_BecomesLiteral(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    K:
      const: "fixed"
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	k, ok := doc.Types[componentID("K")].(*ir.Literal)
	require.True(t, ok, "K should be a literal")
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "fixed"}, k.Value)
}

func TestHoistLiteral_UnconvertibleConstBecomesAny(t *testing.T) {
	t.Parallel()
	// A custom tag is structurally unconvertible (no scalarValue case resolves
	// it), forcing hoistLiteral's fallback. Before the fix this silently
	// produced a Literal asserting the value is null, which the spec never said.
	spec := openapitest.ComponentSpec("    K:\n      const: !foo bar\n")
	doc, diags := schema.LowerSpec(t, spec)
	k, ok := doc.Types[componentID("K")].(*ir.Any)
	require.True(t, ok, "an unconvertible const hoists the schemaless top type at its own pointer")
	assert.Equal(t, ir.KindAny, k.Kind())
	for _, td := range doc.Types {
		_, isLiteral := td.(*ir.Literal)
		assert.False(t, isLiteral, "no Literal is produced anywhere; nothing lies about the value being null")
	}
	require.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityWarning),
		"exactly one warning fires for the unconvertible value")
	d, ok := openapitest.FirstDegradedWarning(diags)
	require.True(t, ok)
	assert.Equal(t, jsontext.Pointer("/components/schemas/K"), d.Provenance.Pointer)
}

func TestEnumAsUnion_UnconvertibleMemberBecomesAny(t *testing.T) {
	t.Parallel()
	// The convertible member ("ok") must still hoist a real Literal; only the
	// genuinely unconvertible member ("!foo bar") falls back to the top type.
	spec := openapitest.ComponentSpec("    M:\n      enum: [ok, !foo bar]\n")
	doc, diags := schema.LowerSpec(t, spec)
	u, ok := doc.Types[componentID("M")].(*ir.Union)
	require.True(t, ok, "heterogeneous enum still lowers to a union of literals")
	require.Len(t, u.Variants, 2)

	member0, ok := doc.Types[u.Variants[0].Type.Target].(*ir.Literal)
	require.True(t, ok, "the convertible member still hoists a Literal")
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "ok"}, member0.Value)

	member1, ok := doc.Types[u.Variants[1].Type.Target].(*ir.Any)
	require.True(t, ok, "the unconvertible member hoists the schemaless top type, not a lying null Literal")
	assert.Equal(t, ir.KindAny, member1.Kind())

	require.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityWarning),
		"exactly one warning for the unconvertible member, distinct from the heterogeneous-enum info diagnostic")
	d, ok := openapitest.FirstDegradedWarning(diags)
	require.True(t, ok)
	assert.Equal(t, jsontext.Pointer("/components/schemas/M/enum/1"), d.Provenance.Pointer)
}

func TestEnum_UnquotedDatesStayClosedEnum(t *testing.T) {
	t.Parallel()
	// The exact issue repro: YAML 1.1 resolves an unquoted date to !!timestamp.
	// Before the fix, scalarValue had no case for it, so both enum members
	// failed to convert, enumMembers bailed to enumAsUnion, and the union
	// fallback turned every member into a null literal — the actual dates never
	// survived. One diagnostic stands beside that, not about a date: the
	// component-level default is kept as residue (it binds a use of the type, so
	// the type node has no field for it; see
	// TestProperty_UnquotedDateDefaultPreserved for the default path).
	spec := openapitest.ComponentSpec(`    D:
      type: string
      format: date
      default: 2021-01-01
      enum: [2021-01-01, 2022-02-02]
`)
	doc, diags := schema.LowerSpec(t, spec)
	require.Len(t, diags, 1, "the dates convert cleanly: no diagnostic is about a member")
	assert.Equal(t, jsontext.Pointer("/components/schemas/D/default"), diags[0].Provenance.Pointer)
	e, ok := doc.Types[componentID("D")].(*ir.Enum)
	require.True(t, ok, "D stays a closed Enum, never degrades to a Union of literals")
	assert.True(t, e.Closed)
	assert.Equal(t, ir.PrimDate, e.ValueType)
	require.Len(t, e.Members, 2)
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "2021-01-01"}, e.Members[0].Value)
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "2022-02-02"}, e.Members[1].Value)
}

// TestEnum_ValueTypeReadsTheFormatItsMembersFit holds an Enum's ValueType to the
// table scalars read (GitHub #607), and the format to one home: the ValueType
// that took it, or else Unmodeled with a report. A format the members do not fit
// is not taken, since no target could back the enum with it.
func TestEnum_ValueTypeReadsTheFormatItsMembersFit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		schema string
		want   ir.PrimKind
		kept   bool
	}{
		{"a sized integer", `{type: integer, format: int64, enum: [1, 2]}`, ir.PrimInt64, false},
		{"a date", `{type: string, format: date, enum: ["2020-01-01"]}`, ir.PrimDate, false},
		{"a width under number", `{type: number, format: int32, enum: [1]}`, ir.PrimInt32, false},
		{"a float that rounds", `{type: number, format: float, enum: [0.1]}`, ir.PrimFloat32, false},
		{"a nullable type", `{type: [integer, "null"], format: int64, enum: [1, null]}`, ir.PrimInt64, false},
		{"no members to fit", `{type: integer, format: int64, enum: []}`, ir.PrimInt64, false},
		{"a format the table lacks", `{type: string, format: email, enum: [a@b.c]}`, ir.PrimString, true},
		{"byte, read before the table", `{type: string, format: byte, enum: [YWJj]}`, ir.PrimString, true},
		{"a pairing the table lacks", `{type: integer, format: float, enum: [1]}`, ir.PrimInteger, true},
		{"binary, which no member's value is", `{type: string, format: binary, enum: [abc]}`, ir.PrimString, true},
		{"a member past the width", `{type: integer, format: int8, enum: [1, 1000]}`, ir.PrimInteger, true},
		{"a fraction under a width", `{type: number, format: int32, enum: [1.5]}`, ir.PrimNumber, true},
		{"a member no date", `{type: string, format: date, enum: [soon]}`, ir.PrimString, true},
		{"no declared type", `{format: int64, enum: [1, 2]}`, ir.PrimNumber, true},
		{"several declared types", `{type: [integer, string], format: int64, enum: [1, 2]}`, ir.PrimNumber, true},
		{"no format", `{type: integer, enum: [1, 2]}`, ir.PrimInteger, false},
		{"a redaction", `{type: string, format: password, enum: [a]}`, ir.PrimString, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec("    E: "+tc.schema+"\n"))
			openapitest.RequireNoErrorDiags(t, diags)
			e, ok := doc.Types[componentID("E")].(*ir.Enum)
			require.True(t, ok)
			assert.Equal(t, tc.want, e.ValueType)
			_, kept := e.Unmodeled["openapi:format"]
			assert.Equal(t, tc.kept, kept)
			assert.Equal(t, tc.kept, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, "/components/schemas/E"),
				"the format is reported exactly where it is kept: %+v", diags)
		})
	}
}

// TestEnum_NarrowsARedeclaredPrimitiveInEitherOrder holds the allOf judge's reading
// of ValueType: a field redeclared as an enum whose format the members fit narrows
// the primitive it restates, whichever branch declares it. With the format left
// verbatim the two orders kept different declarations.
func TestEnum_NarrowsARedeclaredPrimitiveInEitherOrder(t *testing.T) {
	t.Parallel()
	const (
		enum  = `{type: object, properties: {f: {type: integer, format: int8, enum: [1]}}}`
		plain = `{type: object, properties: {f: {type: integer, format: int64}}}`
	)
	for name, branches := range map[string]string{"enum first": enum + ", " + plain, "enum last": plain + ", " + enum} {
		doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec("    M: {allOf: ["+branches+"]}\n"))
		assert.Empty(t, diags, name)
		model, ok := doc.Types[componentID("M")].(*ir.Model)
		require.True(t, ok, name)
		require.Len(t, model.Properties, 1, name)
		e, ok := doc.Types[model.Properties[0].Type.Target].(*ir.Enum)
		require.True(t, ok, name)
		assert.Equal(t, ir.PrimInt8, e.ValueType, name)
	}
}

// TestEnumAsUnion_ExclusiveWhereNoInstanceMatchesTwoEntries holds the union an
// enum lowers to to what Exclusive states: a repeated entry matches an instance
// twice, and so does the top type an unconvertible entry lowers to.
func TestEnumAsUnion_ExclusiveWhereNoInstanceMatchesTwoEntries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		schema string
		want   bool
	}{
		{"distinct entries", `{enum: [1, "a"]}`, true},
		{"a repeated entry", `{enum: [1, "a", 1]}`, false},
		{"one value spelled two ways", `{enum: [1, "a", 1.0]}`, false},
		{"a repeated composite", `{enum: [[1], "a", [1.0]]}`, false},
		{"an unconvertible entry", `{enum: [1, !foo bar]}`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := schema.LowerSpec(t, openapitest.ComponentSpec("    E: "+tc.schema+"\n"))
			u, ok := doc.Types[componentID("E")].(*ir.Union)
			require.True(t, ok)
			assert.Equal(t, tc.want, u.Exclusive)
		})
	}
}

func TestEnum_ValueTypeVariants(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    EInt: {type: integer, enum: [1, 2]}
    ENum: {type: number, enum: [1.5, 2.5]}
    EBool: {type: boolean, enum: [true, false]}
    ENoTypeBool: {enum: [true, false]}
    ENoTypeNum: {enum: [1, 2]}
    ENoTypeStr: {enum: [a, b]}
    EBytes: {enum: [!!binary aGk=, !!binary Ynll]}
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	want := map[string]ir.PrimKind{
		"EInt": ir.PrimInteger, "ENum": ir.PrimNumber, "EBool": ir.PrimBool,
		"ENoTypeBool": ir.PrimBool, "ENoTypeNum": ir.PrimNumber,
		"ENoTypeStr": ir.PrimString, "EBytes": ir.PrimBytes,
	}
	for name, prim := range want {
		e, ok := typeByName(doc, name).(*ir.Enum)
		require.True(t, ok, "%s is an enum", name)
		assert.Equal(t, prim, e.ValueType, "%s value type", name)
	}
}

// TestEnum_ByteMembersAreNamedAndTyped pins the pair of defects an undeclared
// ValueKind produced: a !!binary member is a ValueBytes, which the old
// classification described as a string and gave no text form at all, so every
// byte member of an enum arrived with an empty source name — two members, one
// name, no diagnostic.
func TestEnum_ByteMembersAreNamedAndTyped(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec("    EBytes: {enum: [!!binary aGk=, !!binary Ynll]}\n")
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	e, ok := typeByName(doc, "EBytes").(*ir.Enum)
	require.True(t, ok, "a homogeneous byte enum stays an enum")
	assert.Equal(t, ir.PrimBytes, e.ValueType, "byte members declare the byte primitive, not string")
	require.Len(t, e.Members, 2)
	assert.Equal(t, "aGk=", e.Members[0].Name.Source, "a byte member is named by its base64 text")
	assert.Equal(t, "Ynll", e.Members[1].Name.Source)
	assert.NotEqual(t, e.Members[0].Name.Source, e.Members[1].Name.Source,
		"distinct byte members must not collapse onto one name")
	assert.NotEmpty(t, e.Members[0].Name.Canonical, "a named member has canonical words")
}

func TestEnum_HeterogeneousBecomesUnionWithBadValue(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec("    Mixed:\n      enum: [active, .inf]\n")
	doc, diags := schema.LowerSpec(t, spec)
	u, ok := typeByName(doc, "Mixed").(*ir.Union)
	require.True(t, ok, "heterogeneous enum lowers to a union of literals")
	assert.Len(t, u.Variants, 2)
	assert.True(t, openapitest.HasDiagAt(diags, diag.DegradedConstruct, ir.SeverityInfo),
		"heterogeneous-enum info diagnostic")
	assert.True(t, openapitest.HasDiagAt(diags, diag.DegradedConstruct, ir.SeverityWarning),
		"unconvertible literal value warning")
}

func TestEnum_NonScalarAndMidListMismatch(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    ObjEnum:
      enum:
        - {a: 1}
        - {b: 2}
    MidMismatch:
      enum: [alpha, beta, 3]
`)
	doc, diags := schema.LowerSpec(t, spec)
	require.NotNil(t, doc)
	_, objIsUnion := typeByName(doc, "ObjEnum").(*ir.Union)
	assert.True(t, objIsUnion, "object-valued enum degrades to a union")
	_, midIsUnion := typeByName(doc, "MidMismatch").(*ir.Union)
	assert.True(t, midIsUnion, "kind change mid-list degrades to a union")
	_ = diags
}

func TestEnumMemberIDs_SharedCanonicalNameStillDistinct(t *testing.T) {
	t.Parallel()
	doc, _ := schema.LowerSpec(t, openapitest.ComponentSpec(`    E:
      type: string
      enum: [a-b, a_b, "A B"]
`))
	members := enumOf(t, doc, componentID("E"))
	require.Len(t, members, 3)
	assert.Equal(t, members[0].Name.Canonical, members[1].Name.Canonical, "the names render from the same words")
	assert.Equal(t, members[0].Name.Canonical, members[2].Name.Canonical)
	assert.Equal(t, []string{"s:a-b", "s:a_b", "s:A B"}, memberKeys(members))
}

func TestEnumMemberIDs_SignedNumbersAreThreeIDs(t *testing.T) {
	t.Parallel()
	doc, _ := schema.LowerSpec(t, openapitest.ComponentSpec(`    N:
      type: integer
      enum: [-1, 1, 0]
`))
	assert.Equal(t, []string{"n:-1e0", "n:1e0", "n:0"}, memberKeys(enumOf(t, doc, componentID("N"))))
}

// TestEnumMemberIDs_OneNumberSpelledTwiceIsARepeat holds a number's key to its
// value: 10, 10.0 and 1e1 are one number, so the second and third are repeats,
// each keeping the spelling it was written with.
func TestEnumMemberIDs_OneNumberSpelledTwiceIsARepeat(t *testing.T) {
	t.Parallel()
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(`    N:
      type: number
      enum: [10, 10.0, 1e1, 100]
`))
	members := enumOf(t, doc, componentID("N"))
	assert.Equal(t, []string{"n:1e1", "n:1e1/2", "n:1e1/3", "n:1e2"}, memberKeys(members))
	spelled := make([]ir.BigVal, 0, len(members))
	for _, m := range members {
		spelled = append(spelled, m.Value.Num)
	}
	assert.Equal(t, []ir.BigVal{"10", "10.0", "1e1", "100"}, spelled, "the value is kept as written")
	assert.Equal(t, 2, openapitest.CountDiagsAt(diags, diag.DuplicateEnumValue, ir.SeverityWarning))
}

// TestEnumMemberIDs_ANumberPastABinaryFloatIsANumber holds a literal no binary
// float scales to as the number it is: 1e2000000000 and 10e1999999999 are one
// value, so they key as numbers, the second a repeat of the first, and not as
// the strings YAML leaves a literal past float64 as.
func TestEnumMemberIDs_ANumberPastABinaryFloatIsANumber(t *testing.T) {
	t.Parallel()
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(`    N:
      type: number
      enum: [1e2000000000, 10e1999999999]
`))
	assert.Equal(t, []string{"n:1e2000000000", "n:1e2000000000/2"}, memberKeys(enumOf(t, doc, componentID("N"))))
	assert.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.DuplicateEnumValue, ir.SeverityWarning))
}

func TestEnumMemberIDs_NamesThatCanonicalizeToNothingStillGetIDs(t *testing.T) {
	t.Parallel()
	doc, _ := schema.LowerSpec(t, openapitest.ComponentSpec(`    E:
      type: string
      enum: ["", "-", "!"]
`))
	members := enumOf(t, doc, componentID("E"))
	require.Len(t, members, 3)
	for _, m := range members {
		assert.Empty(t, m.Name.Canonical, "%q has no words", m.Name.Source)
	}
	assert.Equal(t, []string{"s:", "s:-", "s:!"}, memberKeys(members))
}

func TestEnumMemberIDs_StringAndNumberOfEqualTextDiffer(t *testing.T) {
	t.Parallel()
	doc, _ := schema.LowerSpec(t, openapitest.ComponentSpec(`    S:
      type: string
      enum: ["1"]
    N:
      type: integer
      enum: [1]
    B:
      type: string
      enum: ["true"]
    T:
      type: boolean
      enum: [true]
`))
	keys := map[string]bool{}
	for _, name := range []string{"S", "N", "B", "T"} {
		keys[memberKeys(enumOf(t, doc, componentID(name)))[0]] = true
	}
	assert.Equal(t, map[string]bool{"s:1": true, "n:1e0": true, "s:true": true, "b:true": true}, keys)
}

func TestEnumMemberIDs_ReorderKeepsTheIDSet(t *testing.T) {
	t.Parallel()
	byValue := func(order string) map[string]ir.EnumMemberID {
		doc, _ := schema.LowerSpec(t, openapitest.ComponentSpec(`    E:
      type: string
      enum: `+order+"\n"))
		out := map[string]ir.EnumMemberID{}
		for _, m := range enumOf(t, doc, componentID("E")) {
			out[m.Value.Str] = m.ID
		}
		return out
	}
	assert.Equal(t, byValue("[x, y, z]"), byValue("[z, x, y]"), "reordering renames no member")
}

func TestEnumMemberIDs_RepeatedValueIsKeptSuffixedAndWarned(t *testing.T) {
	t.Parallel()
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(`    E:
      type: string
      enum: [a, b, a, a]
`))
	assert.Equal(t, []string{"s:a", "s:b", "s:a/2", "s:a/3"}, memberKeys(enumOf(t, doc, componentID("E"))))

	var at []string
	for _, d := range diags {
		if d.Code == diag.DuplicateEnumValue {
			assert.Equal(t, ir.SeverityWarning, d.Severity)
			at = append(at, string(d.Provenance.Pointer))
		}
	}
	assert.Equal(t, []string{"/components/schemas/E/enum/2", "/components/schemas/E/enum/3"}, at)
	msg := openapitest.DiagMessageAt(t, diags, diag.DuplicateEnumValue, ir.SeverityWarning, "/components/schemas/E/enum/3")
	assert.Contains(t, msg, "the entry at index 0;", "the third a repeats the first, not the second")
}

// TestEnumMemberIDs_RepeatIsReportedAtItsSourceIndex covers a nullable enum,
// whose null is not a member: the pointer a repeat is reported at, and the index
// it names, are positions in the enum as written, not in the member list.
func TestEnumMemberIDs_RepeatIsReportedAtItsSourceIndex(t *testing.T) {
	t.Parallel()
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(`    E:
      type: [string, "null"]
      enum: [null, a, b, a]
`))
	assert.Equal(t, []string{"s:a", "s:b", "s:a/2"}, memberKeys(enumOf(t, doc, componentID("E"))))
	msg := openapitest.DiagMessageAt(t, diags, diag.DuplicateEnumValue, ir.SeverityWarning, "/components/schemas/E/enum/3")
	assert.Contains(t, msg, "the entry at index 1;")
}

// TestEnumRepeats_AreReportedWhateverTheSetLowersTo pins the report to the list
// as written: a repeat is reported where the set degrades to a union, numbers
// compare by value there too, and a repeated null is reported where a nullable
// enum keeps no null member. A list or an object compares as an instance does:
// by its elements' values, an object regardless of member order.
func TestEnumRepeats_AreReportedWhateverTheSetLowersTo(t *testing.T) {
	t.Parallel()
	_, diags := schema.LowerSpec(t, openapitest.ComponentSpec(`    Mixed: {enum: [a, 1, a, 1.0]}
    Nullable: {type: [string, "null"], enum: [a, null, null]}
    Nulls: {enum: [null, null]}
    Lists: {enum: [[1, 2], [2, 1], [1, 2.0]]}
    Objects: {enum: [{x: 1, y: [a]}, {y: [a], x: 1.0}, {x: 1}, {x: "1"}]}
`))
	var at []string
	for _, d := range diags {
		if d.Code == diag.DuplicateEnumValue {
			assert.Equal(t, ir.SeverityWarning, d.Severity)
			at = append(at, string(d.Provenance.Pointer))
		}
	}
	assert.ElementsMatch(t, []string{
		"/components/schemas/Mixed/enum/2", "/components/schemas/Mixed/enum/3",
		"/components/schemas/Nullable/enum/2", "/components/schemas/Nulls/enum/1",
		"/components/schemas/Lists/enum/2", "/components/schemas/Objects/enum/1",
	}, at)
	msg := openapitest.DiagMessageAt(t, diags, diag.DuplicateEnumValue, ir.SeverityWarning, "/components/schemas/Mixed/enum/3")
	assert.Contains(t, msg, "the entry at index 1;", "1.0 repeats 1, not the a between them")
}

func TestEnumMemberIDs_AreScopedToTheirEnum(t *testing.T) {
	t.Parallel()
	doc, _ := schema.LowerSpec(t, openapitest.ComponentSpec(`    A:
      type: string
      enum: [x]
    B:
      type: string
      enum: [x]
    Holder:
      type: object
      properties:
        p: {type: string, enum: [x]}
`))
	a := enumOf(t, doc, componentID("A"))[0].ID
	b := enumOf(t, doc, componentID("B"))[0].ID
	inline := enumOf(t, doc, "t/anon/components/schemas/Holder/properties/p")[0].ID
	assert.Equal(t, ir.EnumMemberID("e/openapi/components/schemas/A/s:x"), a)
	assert.Equal(t, ir.EnumMemberID("e/openapi/components/schemas/B/s:x"), b)
	assert.Equal(t, ir.EnumMemberID("e/anon/components/schemas/Holder/properties/p/s:x"), inline)
}

// enumPropertySpec puts a schema at one property of a model S, so a test can
// read both the node the schema lowers to and the reference the position holds.
func enumPropertySpec(version, body string) string {
	return openapitest.ComponentSpecVer(version, `    S:
      type: object
      properties:
        p: `+body+"\n")
}

// enumOf returns the members of the enum at id.
func enumOf(t *testing.T, doc *ir.Document, id ir.TypeID) []ir.EnumMember {
	t.Helper()
	enum, ok := doc.Types[id].(*ir.Enum)
	require.True(t, ok, "%s is an enum", id)
	return enum.Members
}

// memberKeys returns each member's ID past its enum's path: the key, and a
// repeat's occurrence number after it.
func memberKeys(members []ir.EnumMember) []string {
	keys := make([]string, 0, len(members))
	for _, m := range members {
		_, key, _ := strings.Cut(strings.TrimPrefix(string(m.ID), "e/openapi/components/schemas/"), "/")
		keys = append(keys, key)
	}
	return keys
}
