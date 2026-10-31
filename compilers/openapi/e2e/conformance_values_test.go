// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

func assertDefaults(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	m, ok := doc.Types[componentID("S")].(*ir.Model)
	require.True(t, ok)
	n, ok := propByWire(m, "n")
	require.True(t, ok)
	require.NotNil(t, n.Default)
	assert.Equal(t, ir.ValueNumber, n.Default.Kind)
	assert.Equal(t, ir.BigVal("9007199254740993"), n.Default.Num)

	// A declaration-site default reaches the referencing property, and the
	// declaration keeps it verbatim because the type node has no field for it.
	viaRef, ok := propByWire(m, "viaRef")
	require.True(t, ok)
	require.NotNil(t, viaRef.Default, "the referent's default binds the reference site")
	assert.Equal(t, ir.BigVal("41"), viaRef.Default.Num)

	decl, ok := doc.Types[componentID("DeclaredDefault")].(*ir.Scalar)
	require.True(t, ok)
	entry, ok := decl.Unmodeled["openapi:default"]
	require.True(t, ok, "the declaration's own default is kept as residue")
	assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
	assert.JSONEq(t, `41`, string(entry.Value))

	assertNonScalarDefaults(t, m)
}

// assertNonScalarDefaults pins the Value payloads a default reaches beyond string
// and number: a boolean, a base64 !!binary scalar, and a sequence. Each selects a
// different payload field, and Kind is what says which one carries meaning.
func assertNonScalarDefaults(t *testing.T, m *ir.Model) {
	t.Helper()
	flag, ok := propByWire(m, "flag")
	require.True(t, ok)
	require.NotNil(t, flag.Default)
	assert.Equal(t, ir.Value{Kind: ir.ValueBool, Bool: true}, *flag.Default)

	cleared, ok := propByWire(m, "cleared")
	require.True(t, ok)
	require.NotNil(t, cleared.Default, "a written null is a default, not the absence of one")
	assert.Equal(t, ir.Value{Kind: ir.ValueNull}, *cleared.Default)
	assert.True(t, cleared.Type.Nullable, "and the type admits it")

	blob, ok := propByWire(m, "blob")
	require.True(t, ok)
	require.NotNil(t, blob.Default)
	assert.Equal(t, ir.ValueBytes, blob.Default.Kind)
	assert.Equal(t, []byte("hi"), blob.Default.Bytes, "a !!binary default is decoded, not kept as text")

	list, ok := propByWire(m, "list")
	require.True(t, ok)
	require.NotNil(t, list.Default)
	assert.Equal(t, ir.ValueList, list.Default.Kind)
	require.Len(t, list.Default.List, 2)
	assert.Equal(t, ir.BigVal("1"), list.Default.List[0].Num)
	assert.Equal(t, ir.BigVal("2"), list.Default.List[1].Num)
}

// assertYAMLTimestampScalars covers a YAML 1.1 quirk: an unquoted date like
// 2021-01-01 resolves to tag !!timestamp, not !!str. It must survive as the
// literal string everywhere OpenAPI's JSON data model can carry one — enum,
// const, a property default, a schema-level example, and a media-type example —
// with nothing dropped or degraded to null.
//
// "Everywhere" spans both channels a date can land in. It used to mean only the
// ir.Value one, and the raw-JSON one went on rewriting a date to RFC 3339 with
// this fixture green (GitHub #242); assertRawPreservedDates is the other half.
func assertYAMLTimestampScalars(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	// R's `not` announces the §4.7 carve-out, and D's `format: date` announces
	// that an ir.Enum has no Encoding field to put it in, so it is kept verbatim
	// beside the enum rather than dropped. Those two are the only notices any
	// site here is allowed to say, and neither is about a date: one that was
	// dropped or degraded reports itself as a warning, which is what this fixture
	// exists to catch.
	for _, d := range diags {
		assert.Equal(t, ir.SeverityInfo, d.Severity,
			"every unquoted date converts cleanly; nothing is dropped or degraded: %+v", d)
		if d.Code == "openapi/degraded-construct" && d.Provenance.Pointer == "/components/schemas/D" {
			continue
		}
		assert.Equal(t, "openapi/validation-only-keyword", d.Code,
			"the §4.7 carve-out is the only other notice this fixture expects: %+v", d)
	}

	d, ok := doc.Types[componentID("D")].(*ir.Enum)
	require.True(t, ok, "D stays a closed Enum of the real dates, not a union of null literals")
	assert.Equal(t, ir.PrimString, d.ValueType)
	require.Len(t, d.Members, 2)
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "2021-01-01"}, d.Members[0].Value)
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "2022-02-02"}, d.Members[1].Value)

	k, ok := doc.Types[componentID("K")].(*ir.Literal)
	require.True(t, ok, "K's const hoists a real Literal, not the schemaless top type")
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "2021-01-01"}, k.Value)

	s, ok := doc.Types[componentID("S")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, s.Properties, 1)
	prop := s.Properties[0]
	require.NotNil(t, prop.Default, "the property default is preserved")
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "2021-01-01"}, *prop.Default)
	require.Len(t, prop.Examples, 1, "the schema-level example is preserved")
	require.NotNil(t, prop.Examples[0].Value)
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "2021-01-01"}, *prop.Examples[0].Value)

	op, ok := opByName(doc, "getItem")
	require.True(t, ok)
	require.NotNil(t, op.Responses[0].Payload)
	require.Len(t, op.Responses[0].Payload.Contents, 1)
	mediaExamples := op.Responses[0].Payload.Contents[0].Examples
	require.Len(t, mediaExamples, 1, "the media-type example is preserved")
	require.NotNil(t, mediaExamples[0].Value)
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "2021-01-01"}, *mediaExamples[0].Value)

	assertRawPreservedDates(t, doc)
}

// assertRawPreservedDates pins the source spelling of a date kept as raw JSON,
// at both sites that channel has: a vendor extension and the §4.7
// validation-only carve-out. Each wants the text the source wrote — resolving
// the tag and re-rendering it hands the construct a padding, a time and a zone
// nobody asked for, and the resolved form cannot be read back to the spelling.
func assertRawPreservedDates(t *testing.T, doc *ir.Document) {
	t.Helper()
	r, ok := doc.Types[componentID("R")].(*ir.Model)
	require.True(t, ok)
	kept := r.Unmodeled

	for _, tc := range []struct{ key, want string }{
		{"openapi:x-effective", `"2021-1-1"`},
		{"openapi:x-window", `{"from":"2021-1-1","to":"2022-2-2"}`},
		{"openapi:not", `{"const":"2021-1-1"}`},
	} {
		entry, found := kept[tc.key]
		require.True(t, found, "%s is preserved; got %v", tc.key, kept)
		assert.Equal(t, tc.want, string(entry.Value),
			"%s keeps the date as written, not as RFC 3339 spells it", tc.key)
	}
}

func assertConstraints(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	m, ok := doc.Types[componentID("S")].(*ir.Model)
	require.True(t, ok)
	ratio, ok := propByWire(m, "ratio")
	require.True(t, ok)
	c := ratio.Constraints
	require.NotNil(t, c)
	// Exact decimal strings — a float64 path would corrupt all three.
	require.NotNil(t, c.Min)
	require.NotNil(t, c.Max)
	require.NotNil(t, c.MultipleOf)
	assert.Equal(t, ir.BigVal("0.30000000000000004"), *c.Min)
	assert.Equal(t, ir.BigVal("9007199254740993"), *c.Max)
	assert.Equal(t, ir.BigVal("0.1"), *c.MultipleOf)

	// The object's own cardinality bound lands on the Model, not on a property
	// and not nowhere (GitHub #129).
	require.NotNil(t, m.Constraints)
	require.NotNil(t, m.Constraints.MinProps)
	require.NotNil(t, m.Constraints.MaxProps)
	assert.Equal(t, int64(1), *m.Constraints.MinProps)
	assert.Equal(t, int64(4), *m.Constraints.MaxProps)

	assertLengthAndCollectionBounds(t, doc, m)
	assertCoDeclaredBounds(t, m, diags)
	assertCoDeclaredBoundKept(t, doc, m)
}

// assertCoDeclaredBounds pins the 2020-12 rule that a side declaring both of
// its keywords carries both: minimum and exclusiveMinimum are independent and
// conjunctive, ir.Constraints has a field for each, and neither is chosen over
// the other (GitHub #33, #425).
//
// Both directions are here on purpose. The property bounded below has the
// inclusive keyword as its tighter bound and the one bounded above the
// exclusive one, so a reader that kept the tighter alone answers the two
// differently — and a reader that always kept the exclusive keyword passes the
// second on its own.
func assertCoDeclaredBounds(t *testing.T, m *ir.Model, diags []ir.Diagnostic) {
	t.Helper()
	low, ok := propByWire(m, "atLeastTen")
	require.True(t, ok)
	require.NotNil(t, low.Constraints)
	require.NotNil(t, low.Constraints.Min)
	require.NotNil(t, low.Constraints.ExclusiveMin)
	assert.Equal(t, ir.BigVal("10"), *low.Constraints.Min, "minimum as written")
	assert.Equal(t, ir.BigVal("0"), *low.Constraints.ExclusiveMin,
		"and the looser exclusiveMinimum beside it, not dropped for being implied")

	high, ok := propByWire(m, "underTen")
	require.True(t, ok)
	require.NotNil(t, high.Constraints)
	require.NotNil(t, high.Constraints.Max)
	require.NotNil(t, high.Constraints.ExclusiveMax)
	assert.Equal(t, ir.BigVal("100"), *high.Constraints.Max, "maximum as written")
	assert.Equal(t, ir.BigVal("10"), *high.Constraints.ExclusiveMax, "and exclusiveMaximum beside it")

	for _, d := range diags {
		assert.NotContains(t, d.Message, "exclusiveMinimum",
			"a pair that reaches two fields is not a degradation to report")
	}
}

// assertCoDeclaredBoundKept is the losslessness half of the same rule: with a
// field per keyword there is nothing left over, so neither carrier keeps a bound
// verbatim. Nothing is restated beside constraints that hold it all — an entry
// there would give one bound two homes, and {minimum: 10, exclusiveMinimum: 0}
// is told from {minimum: 10} by the fields themselves (GitHub #286).
func assertCoDeclaredBoundKept(t *testing.T, doc *ir.Document, m *ir.Model) {
	t.Helper()
	low, ok := propByWire(m, "atLeastTen")
	require.True(t, ok)
	assert.Empty(t, low.Unmodeled, "the property keeps nothing beside its constraints")

	high, ok := propByWire(m, "underTen")
	require.True(t, ok)
	assert.Empty(t, high.Unmodeled, "and neither does the side settled the other way")

	alias, ok := doc.Types[componentID("Bounded")].(*ir.Scalar)
	require.True(t, ok, "a component reducing to a shared primitive owns an alias node")
	require.NotNil(t, alias.Constraints)
	require.NotNil(t, alias.Constraints.Min)
	require.NotNil(t, alias.Constraints.ExclusiveMin)
	assert.Equal(t, ir.BigVal("0"), *alias.Constraints.ExclusiveMin,
		"a node carries both bounds, exactly as a property does")
	assert.Empty(t, alias.Unmodeled)
}

// assertLengthAndCollectionBounds pins the non-numeric bounds: a string length
// pair on the declaring property, and a collection bound on the List the array
// position hoisted, which is the node that describes the collection.
func assertLengthAndCollectionBounds(t *testing.T, doc *ir.Document, m *ir.Model) {
	t.Helper()
	label, ok := propByWire(m, "label")
	require.True(t, ok)
	require.NotNil(t, label.Constraints)
	require.NotNil(t, label.Constraints.MinLength)
	require.NotNil(t, label.Constraints.MaxLength)
	assert.Equal(t, int64(2), *label.Constraints.MinLength)
	assert.Equal(t, int64(8), *label.Constraints.MaxLength)

	tags, ok := propByWire(m, "tags")
	require.True(t, ok)
	list, ok := doc.Types[tags.Type.Target].(*ir.List)
	require.True(t, ok, "an array position hoists a List")
	require.NotNil(t, list.Constraints)
	require.NotNil(t, list.Constraints.MinItems)
	require.NotNil(t, list.Constraints.MaxItems)
	assert.Equal(t, int64(1), *list.Constraints.MinItems)
	assert.Equal(t, int64(5), *list.Constraints.MaxItems)
	assert.True(t, list.Constraints.UniqueItems)
}

func assertNumericPrecision(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	m, ok := doc.Types[componentID("S")].(*ir.Model)
	require.True(t, ok)

	// Bounds beyond float64 range and a huge integer beyond int64, exact.
	bounded, ok := propByWire(m, "bounded")
	require.True(t, ok)
	require.NotNil(t, bounded.Constraints)
	require.NotNil(t, bounded.Constraints.Min)
	require.NotNil(t, bounded.Constraints.Max)
	require.NotNil(t, bounded.Constraints.MultipleOf)
	assert.Equal(t, ir.BigVal("1.8e308"), *bounded.Constraints.Min)
	assert.Equal(t, ir.BigVal("123456789012345678901234567890"), *bounded.Constraints.Max)
	assert.Equal(t, ir.BigVal("1e-30"), *bounded.Constraints.MultipleOf)

	// A leading-dot spelling is canonicalized to JSON form; a high-precision
	// decimal is kept to the last digit.
	exclusive, ok := propByWire(m, "exclusive")
	require.True(t, ok)
	require.NotNil(t, exclusive.Constraints)
	require.NotNil(t, exclusive.Constraints.ExclusiveMin)
	require.NotNil(t, exclusive.Constraints.ExclusiveMax)
	assert.Equal(t, ir.BigVal("0.5"), *exclusive.Constraints.ExclusiveMin)
	assert.Equal(t, ir.BigVal("0.12345678901234567890123456789"), *exclusive.Constraints.ExclusiveMax)

	// A default beyond float64 range is captured as a number, not a string.
	withDefault, ok := propByWire(m, "withDefault")
	require.True(t, ok)
	require.NotNil(t, withDefault.Default)
	assert.Equal(t, ir.ValueNumber, withDefault.Default.Kind)
	assert.Equal(t, ir.BigVal("1.8e308"), withDefault.Default.Num)

	// A const beyond float64 range hoists a Literal over the exact number.
	pinned, ok := propByWire(m, "pinned")
	require.True(t, ok)
	lit, ok := doc.Types[pinned.Type.Target].(*ir.Literal)
	require.True(t, ok)
	assert.Equal(t, ir.ValueNumber, lit.Value.Kind)
	assert.Equal(t, ir.BigVal("1.8e308"), lit.Value.Num)

	// Numeric enum members keep their exact value past int64 range.
	choice, ok := propByWire(m, "choice")
	require.True(t, ok)
	enum, ok := doc.Types[choice.Type.Target].(*ir.Enum)
	require.True(t, ok)
	require.Len(t, enum.Members, 2)
	assert.Equal(t, ir.BigVal("123456789012345678901234567890"), enum.Members[1].Value.Num)

	// A leading-dot example is canonicalized losslessly.
	sampled, ok := propByWire(m, "sampled")
	require.True(t, ok)
	require.Len(t, sampled.Examples, 1)
	require.NotNil(t, sampled.Examples[0].Value)
	assert.Equal(t, ir.BigVal("0.5"), sampled.Examples[0].Value.Num)

	assertYAMLIntegerBases(t, doc, m)
	assertRawPreservedNumbers(t, m)
}

// assertRawPreservedNumbers pins the second numeric channel: the constructs kept
// verbatim as raw JSON rather than lowered into a Value. It rounded every one of
// these through float64 until GitHub #32 — a 30-digit extension came back as
// 1.2345678901234568e+29 — which no case in this corpus could see, because none
// of them carried a number a float64 cannot hold.
func assertRawPreservedNumbers(t *testing.T, m *ir.Model) {
	t.Helper()
	preserved, ok := propByWire(m, "preserved")
	require.True(t, ok)
	kept := preserved.Unmodeled

	for _, tc := range []struct{ key, want string }{
		{"openapi:x-precise-limit", "123456789012345678901234567890"},
		{"openapi:x-precise-step", "1.000000000000000000001"},
		{"openapi:x-precise-scale", "1.10"},
		{"openapi:not", `{"const":123456789012345678901234567890}`},
	} {
		entry, found := kept[tc.key]
		require.True(t, found, "%s is preserved; got %v", tc.key, kept)
		assert.Equal(t, tc.want, string(entry.Value),
			"%s is preserved as written, not as a float64 can spell it", tc.key)
	}
}

// assertYAMLIntegerBases pins the value every numeric site stores for an integer
// whose YAML base comes from its prefix. Reading the source spelling as base 10
// would store 644 where the document said 420, and would do it silently.
func assertYAMLIntegerBases(t *testing.T, doc *ir.Document, m *ir.Model) {
	t.Helper()
	mode, ok := propByWire(m, "mode")
	require.True(t, ok)
	require.NotNil(t, mode.Default)
	assert.Equal(t, ir.BigVal("420"), mode.Default.Num, "0644 is octal in YAML")

	enum, ok := doc.Types[mode.Type.Target].(*ir.Enum)
	require.True(t, ok)
	require.Len(t, enum.Members, 2)
	assert.Equal(t, ir.BigVal("420"), enum.Members[0].Value.Num)
	assert.Equal(t, ir.BigVal("493"), enum.Members[1].Value.Num)

	bounds, ok := propByWire(m, "bounds")
	require.True(t, ok)
	require.NotNil(t, bounds.Constraints)
	require.NotNil(t, bounds.Constraints.Min)
	require.NotNil(t, bounds.Constraints.Max)
	require.NotNil(t, bounds.Constraints.MultipleOf)
	assert.Equal(t, ir.BigVal("15"), *bounds.Constraints.Min, "017 is octal")
	assert.Equal(t, ir.BigVal("31"), *bounds.Constraints.Max, "0x1f is hex")
	assert.Equal(t, ir.BigVal("10"), *bounds.Constraints.MultipleOf, "0b1010 is binary")
	require.Len(t, bounds.Examples, 1)
	require.NotNil(t, bounds.Examples[0].Value)
	assert.Equal(t, ir.BigVal("30"), bounds.Examples[0].Value.Num, "0b11110 is binary")

	// "_" is a separator, and a leading zero YAML resolves as a decimal float
	// still has to come out as a JSON-valid literal.
	counted, ok := propByWire(m, "counted")
	require.True(t, ok)
	require.NotNil(t, counted.Default)
	assert.Equal(t, ir.BigVal("1000000"), counted.Default.Num)
	loose, ok := propByWire(m, "loose")
	require.True(t, ok)
	require.NotNil(t, loose.Default)
	assert.Equal(t, ir.BigVal("9"), loose.Default.Num)
}
