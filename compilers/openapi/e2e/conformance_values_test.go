// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

func assertDefaults(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	model, ok := doc.Types[componentID("S")].(*ir.Model)
	require.True(t, ok)
	property, ok := propByWire(model, "n")
	require.True(t, ok)
	require.NotNil(t, property.Default)
	assert.Equal(t, ir.ValueNumber, property.Default.Kind)
	assert.Equal(t, ir.BigVal("9007199254740993"), property.Default.Number)

	// A declaration-site default reaches the referencing property, and the
	// declaration keeps it verbatim because the type node has no field for it.
	viaRef, ok := propByWire(model, "viaRef")
	require.True(t, ok)
	require.NotNil(t, viaRef.Default, "the referent's default binds the reference site")
	assert.Equal(t, ir.BigVal("41"), viaRef.Default.Number)

	decl, ok := doc.Types[componentID("DeclaredDefault")].(*ir.Scalar)
	require.True(t, ok)
	entry, ok := decl.Unmodeled["openapi:default"]
	require.True(t, ok, "the declaration's own default is kept as residue")
	assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
	assert.JSONEq(t, `41`, string(entry.Value))

	assertNonScalarDefaults(t, model)
}

// assertNonScalarDefaults pins the Value payloads a default reaches beyond string
// and number: a boolean, a null, a base64 !!binary scalar, and a sequence. Each
// but the null, which has none, selects a different payload field, and Kind is
// what says which one carries meaning.
func assertNonScalarDefaults(t *testing.T, model *ir.Model) {
	t.Helper()
	flag, ok := propByWire(model, "flag")
	require.True(t, ok)
	require.NotNil(t, flag.Default)
	assert.Equal(t, ir.Value{Kind: ir.ValueBool, Bool: true}, *flag.Default)

	cleared, ok := propByWire(model, "cleared")
	require.True(t, ok)
	require.NotNil(t, cleared.Default, "a written null is a default, not the absence of one")
	assert.Equal(t, ir.Value{Kind: ir.ValueNull}, *cleared.Default)
	assert.True(t, cleared.Type.Nullable, "and the type admits it")

	blob, ok := propByWire(model, "blob")
	require.True(t, ok)
	require.NotNil(t, blob.Default)
	assert.Equal(t, ir.ValueBytes, blob.Default.Kind)
	assert.Equal(t, []byte("hi"), blob.Default.Bytes, "a !!binary default is decoded, not kept as text")

	list, ok := propByWire(model, "list")
	require.True(t, ok)
	require.NotNil(t, list.Default)
	assert.Equal(t, ir.ValueList, list.Default.Kind)
	require.Len(t, list.Default.List, 2)
	assert.Equal(t, ir.BigVal("1"), list.Default.List[0].Number)
	assert.Equal(t, ir.BigVal("2"), list.Default.List[1].Number)
}

// assertYAMLTimestampScalars covers a YAML 1.1 quirk: an unquoted date like
// 2021-01-01 resolves to tag !!timestamp, not !!str. It must survive as the
// literal string everywhere OpenAPI's JSON data model can carry one — enum,
// const, a property default, a schema-level example, a media-type example, and
// a string field (GitHub #1280) — with nothing dropped or degraded to null.
//
// "Everywhere" spans both channels a date can land in. It used to mean only the
// ir.Value one, and the raw-JSON one went on rewriting a date to RFC 3339 with
// this fixture green (GitHub #242); assertRawPreservedDates is the other half.
func assertYAMLTimestampScalars(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	// R's `not` announces the validation-only carve-out. That is the only notice
	// any site here is allowed to say, and it is not about a date: one that was
	// dropped or degraded reports itself as a warning, which is what this fixture
	// exists to catch.
	for _, diagnostic := range diags {
		assert.Equal(t, ir.SeverityInfo, diagnostic.Severity,
			"every unquoted date converts cleanly; nothing is dropped or degraded: %+v", diagnostic)
		assert.Equal(t, "openapi/validation-only-keyword", diagnostic.Code,
			"the validation-only carve-out is the only other notice this fixture expects: %+v", diagnostic)
	}

	enum, ok := doc.Types[componentID("D")].(*ir.Enum)
	require.True(t, ok, "D stays a closed Enum of the real dates, not a union of null literals")
	assert.Equal(t, ir.PrimitiveDate, enum.ValueType)
	require.Len(t, enum.Members, 2)
	assert.Equal(t, ir.Value{Kind: ir.ValueString, String: "2021-01-01"}, enum.Members[0].Value)
	assert.Equal(t, ir.Value{Kind: ir.ValueString, String: "2022-02-02"}, enum.Members[1].Value)

	literal, ok := doc.Types[componentID("K")].(*ir.Literal)
	require.True(t, ok, "K's const hoists a real Literal, not the schemaless top type")
	assert.Equal(t, ir.Value{Kind: ir.ValueString, String: "2021-01-01"}, literal.Value)

	model, ok := doc.Types[componentID("S")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, model.Properties, 1)
	prop := model.Properties[0]
	require.NotNil(t, prop.Default, "the property default is preserved")
	assert.Equal(t, ir.Value{Kind: ir.ValueString, String: "2021-01-01"}, *prop.Default)
	require.Len(t, prop.Examples, 1, "the schema-level example is preserved")
	require.NotNil(t, prop.Examples[0].Value)
	assert.Equal(t, ir.Value{Kind: ir.ValueString, String: "2021-01-01"}, *prop.Examples[0].Value)

	op, ok := opByName(doc, "getItem")
	require.True(t, ok)
	require.NotNil(t, op.Responses[0].Payload)
	require.Len(t, op.Responses[0].Payload.Contents, 1)
	mediaExamples := op.Responses[0].Payload.Contents[0].Examples
	require.Len(t, mediaExamples, 1, "the media-type example is preserved")
	require.NotNil(t, mediaExamples[0].Value)
	assert.Equal(t, ir.Value{Kind: ir.ValueString, String: "2021-01-01"}, *mediaExamples[0].Value)

	assert.Equal(t, "2021-01-01", doc.Version, "a string field holds the date as its text")
	assert.Equal(t, "2021-01-01", op.Docs.Summary, "a string field holds the date as its text")

	assertRawPreservedDates(t, doc)
}

// assertRawPreservedDates pins the source spelling of a date kept as raw JSON,
// at both sites that channel has: a vendor extension and the validation-only
// validation-only carve-out. Each wants the text the source wrote — resolving
// the tag and re-rendering it hands the construct a padding, a time and a zone
// nobody asked for, and the resolved form cannot be read back to the spelling.
func assertRawPreservedDates(t *testing.T, doc *ir.Document) {
	t.Helper()
	model, ok := doc.Types[componentID("R")].(*ir.Model)
	require.True(t, ok)
	kept := model.Unmodeled

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

// assertRefCollectionBounds holds each position to the one home a collection
// bound written for one use of a shared list has, and the one bound the census
// leaves verbatim to the one report the whole document draws.
func assertRefCollectionBounds(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	holder, ok := doc.Types[componentID("Holder")].(*ir.Model)
	require.True(t, ok)
	minOne, maxThree := &ir.Constraints{MinItems: new(int64(1))}, &ir.Constraints{MaxItems: new(int64(3))}

	for wire, want := range map[string]*ir.Constraints{"plain": nil, "beside": minOne, "nullable": maxThree} {
		property, found := propByWire(holder, wire)
		require.True(t, found, wire)
		assert.Equal(t, want, property.Constraints, wire)
		assert.Empty(t, property.Unmodeled, wire)
	}
	unstated, found := propByWire(holder, "unstated")
	require.True(t, found)
	assert.Nil(t, unstated.Constraints)
	assert.Equal(t, ir.ReasonDegradedLowering, unstated.Unmodeled["openapi:uniqueItems"].Reason)

	element, ok := doc.Types[componentID("Element")].(*ir.List)
	require.True(t, ok)
	alias, ok := doc.Types[element.ElementType.Target].(*ir.Scalar)
	require.True(t, ok)
	assert.Equal(t, maxThree, alias.Constraints)

	named, ok := doc.Types[componentID("Named")].(*ir.Scalar)
	require.True(t, ok)
	assert.Equal(t, &ir.Constraints{UniqueItems: true}, named.Constraints)

	op, found := opByName(doc, "listTags")
	require.True(t, found)
	require.Len(t, op.Params, 1)
	assert.Equal(t, minOne, op.Params[0].Constraints)
	require.Len(t, op.Responses, 1)
	require.Len(t, op.Responses[0].Headers, 1)
	assert.Equal(t, &ir.Constraints{MaxItems: new(int64(5))}, op.Responses[0].Headers[0].Constraints)
	body, ok := doc.Types[op.Responses[0].Payload.Contents[0].Type.Target].(*ir.Scalar)
	require.True(t, ok)
	assert.Equal(t, &ir.Constraints{UniqueItems: true}, body.Constraints)

	assert.Len(t, diags, 1, "only the bound left verbatim is reported: %+v", diags)
}

func assertConstraints(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	model, ok := doc.Types[componentID("S")].(*ir.Model)
	require.True(t, ok)
	ratio, ok := propByWire(model, "ratio")
	require.True(t, ok)
	constraints := ratio.Constraints
	require.NotNil(t, constraints)
	// Exact decimal strings — a float64 path would corrupt all three.
	require.NotNil(t, constraints.Min)
	require.NotNil(t, constraints.Max)
	require.NotNil(t, constraints.MultipleOf)
	assert.Equal(t, ir.BigVal("0.30000000000000004"), *constraints.Min)
	assert.Equal(t, ir.BigVal("9007199254740993"), *constraints.Max)
	assert.Equal(t, ir.BigVal("0.1"), *constraints.MultipleOf)

	// The object's own cardinality bound lands on the Model, not on a property
	// and not nowhere (GitHub #129).
	require.NotNil(t, model.Constraints)
	require.NotNil(t, model.Constraints.MinProperties)
	require.NotNil(t, model.Constraints.MaxProperties)
	assert.Equal(t, int64(1), *model.Constraints.MinProperties)
	assert.Equal(t, int64(4), *model.Constraints.MaxProperties)

	assertLengthAndCollectionBounds(t, doc, model)
	assertCoDeclaredBounds(t, model, diags)
	assertCoDeclaredBoundKept(t, doc, model)
	assertHugeCountKept(t, doc, diags)
	assertBoundsATypeCannotTake(t, doc, model, diags)
}

// assertBoundsATypeCannotTake pins a value bound beside a type it does not
// apply to: it reaches no Constraints, the property's included, and is kept
// verbatim once, with one info, on the node the position owns, an alias where
// it lowered to a shared primitive, while a bound the type takes beside it
// lands on that node, the property holding none.
func assertBoundsATypeCannotTake(t *testing.T, doc *ir.Document, model *ir.Model, diags []ir.Diagnostic) {
	t.Helper()
	for _, tc := range []struct {
		wire, at string
		held     *ir.Constraints
		kept     []string
	}{
		{"id", "/components/schemas/S/properties/id", &ir.Constraints{Pattern: "^a"}, []string{"openapi:maximum", "openapi:minimum"}},
		{"meta", "/components/schemas/S/properties/meta", nil, []string{"openapi:minLength"}},
		{"list", "/components/schemas/S/properties/list", nil, []string{"openapi:maxLength"}},
	} {
		property, ok := propByWire(model, tc.wire)
		require.True(t, ok, tc.wire)
		assert.Nil(t, property.Constraints, tc.wire)
		assert.Empty(t, property.Unmodeled, tc.wire)
		node, ok := doc.Types[property.Type.Target]
		require.True(t, ok, tc.wire)
		if sc, isScalar := node.(*ir.Scalar); isScalar {
			assert.Equal(t, tc.held, sc.Constraints, tc.wire)
		} else {
			assert.Nil(t, tc.held, "%s holds its bound on a %T", tc.wire, node)
		}
		assert.Equal(t, ir.TypeID("type/anonymous"+tc.at), node.Common().ID, "%s owns its node", tc.wire)
		assert.Equal(t, tc.kept, slices.Sorted(maps.Keys(node.Common().Unmodeled)), tc.wire)
		assert.Equal(t, []ir.Severity{ir.SeverityInfo}, diagsAt(diags, "openapi/degraded-construct", tc.at), tc.wire)
	}
	port, ok := doc.Types[componentID("Port")].(*ir.Scalar)
	require.True(t, ok)
	assert.Nil(t, port.Constraints)
	assert.Equal(t, ir.RawValue("65535"), unmodeledEntry(t, port.Unmodeled, "openapi:maxLength").Value)
	assert.Equal(t, []ir.Severity{ir.SeverityInfo}, diagsAt(diags, "openapi/degraded-construct", "/components/schemas/Port"))
}

// assertHugeCountKept pins a count past 64 bits: no Constraints field states it,
// so it is kept verbatim under its keyword, with one info at its schema, beside
// the count the field does state.
func assertHugeCountKept(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	t.Helper()
	scalar, ok := doc.Types[componentID("HugeCount")].(*ir.Scalar)
	require.True(t, ok)
	assert.Equal(t, &ir.Constraints{MinLength: new(int64(1))}, scalar.Constraints)
	require.Contains(t, scalar.Unmodeled, "openapi:maxLength")
	assert.Equal(t, ir.RawValue("99999999999999999999"), scalar.Unmodeled["openapi:maxLength"].Value)
	assert.Len(t, diagsUnder(diags, "/components/schemas/HugeCount"), 1)
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
func assertCoDeclaredBounds(t *testing.T, model *ir.Model, diags []ir.Diagnostic) {
	t.Helper()
	low, ok := propByWire(model, "atLeastTen")
	require.True(t, ok)
	require.NotNil(t, low.Constraints)
	require.NotNil(t, low.Constraints.Min)
	require.NotNil(t, low.Constraints.ExclusiveMin)
	assert.Equal(t, ir.BigVal("10"), *low.Constraints.Min, "minimum as written")
	assert.Equal(t, ir.BigVal("0"), *low.Constraints.ExclusiveMin,
		"and the looser exclusiveMinimum beside it, not dropped for being implied")

	high, ok := propByWire(model, "underTen")
	require.True(t, ok)
	require.NotNil(t, high.Constraints)
	require.NotNil(t, high.Constraints.Max)
	require.NotNil(t, high.Constraints.ExclusiveMax)
	assert.Equal(t, ir.BigVal("100"), *high.Constraints.Max, "maximum as written")
	assert.Equal(t, ir.BigVal("10"), *high.Constraints.ExclusiveMax, "and exclusiveMaximum beside it")

	for _, diagnostic := range diags {
		assert.NotContains(t, diagnostic.Message, "exclusiveMinimum",
			"a pair that reaches two fields is not a degradation to report")
	}
}

// assertCoDeclaredBoundKept is the losslessness half of the same rule: with a
// field per keyword there is nothing left over, so neither carrier keeps a bound
// verbatim. Nothing is restated beside constraints that hold it all — an entry
// there would give one bound two homes, and {minimum: 10, exclusiveMinimum: 0}
// is told from {minimum: 10} by the fields themselves (GitHub #286).
func assertCoDeclaredBoundKept(t *testing.T, doc *ir.Document, model *ir.Model) {
	t.Helper()
	low, ok := propByWire(model, "atLeastTen")
	require.True(t, ok)
	assert.Empty(t, low.Unmodeled, "the property keeps nothing beside its constraints")

	high, ok := propByWire(model, "underTen")
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
func assertLengthAndCollectionBounds(t *testing.T, doc *ir.Document, model *ir.Model) {
	t.Helper()
	label, ok := propByWire(model, "label")
	require.True(t, ok)
	require.NotNil(t, label.Constraints)
	require.NotNil(t, label.Constraints.MinLength)
	require.NotNil(t, label.Constraints.MaxLength)
	assert.Equal(t, int64(2), *label.Constraints.MinLength)
	assert.Equal(t, int64(8), *label.Constraints.MaxLength)

	tags, ok := propByWire(model, "tags")
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
	model, ok := doc.Types[componentID("S")].(*ir.Model)
	require.True(t, ok)

	// Bounds beyond float64 range and a huge integer beyond int64, exact.
	bounded, ok := propByWire(model, "bounded")
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
	exclusive, ok := propByWire(model, "exclusive")
	require.True(t, ok)
	require.NotNil(t, exclusive.Constraints)
	require.NotNil(t, exclusive.Constraints.ExclusiveMin)
	require.NotNil(t, exclusive.Constraints.ExclusiveMax)
	assert.Equal(t, ir.BigVal("0.5"), *exclusive.Constraints.ExclusiveMin)
	assert.Equal(t, ir.BigVal("0.12345678901234567890123456789"), *exclusive.Constraints.ExclusiveMax)

	// A default beyond float64 range is captured as a number, not a string.
	withDefault, ok := propByWire(model, "withDefault")
	require.True(t, ok)
	require.NotNil(t, withDefault.Default)
	assert.Equal(t, ir.ValueNumber, withDefault.Default.Kind)
	assert.Equal(t, ir.BigVal("1.8e308"), withDefault.Default.Number)

	// A const beyond float64 range hoists a Literal over the exact number.
	pinned, ok := propByWire(model, "pinned")
	require.True(t, ok)
	lit, ok := doc.Types[pinned.Type.Target].(*ir.Literal)
	require.True(t, ok)
	assert.Equal(t, ir.ValueNumber, lit.Value.Kind)
	assert.Equal(t, ir.BigVal("1.8e308"), lit.Value.Number)

	// Numeric enum members keep their exact value past int64 range.
	choice, ok := propByWire(model, "choice")
	require.True(t, ok)
	enum, ok := doc.Types[choice.Type.Target].(*ir.Enum)
	require.True(t, ok)
	require.Len(t, enum.Members, 2)
	assert.Equal(t, ir.BigVal("123456789012345678901234567890"), enum.Members[1].Value.Number)

	// A leading-dot example is canonicalized losslessly.
	sampled, ok := propByWire(model, "sampled")
	require.True(t, ok)
	require.Len(t, sampled.Examples, 1)
	require.NotNil(t, sampled.Examples[0].Value)
	assert.Equal(t, ir.BigVal("0.5"), sampled.Examples[0].Value.Number)

	assertYAMLIntegerBases(t, doc, model)
	assertRawPreservedNumbers(t, model)
}

// assertRawPreservedNumbers pins the second numeric channel: the constructs kept
// verbatim as raw JSON rather than lowered into a Value. It rounded every one of
// these through float64 until GitHub #32 — a 30-digit extension came back as
// 1.2345678901234568e+29 — which no case in this corpus could see, because none
// of them carried a number a float64 cannot hold.
func assertRawPreservedNumbers(t *testing.T, model *ir.Model) {
	t.Helper()
	preserved, ok := propByWire(model, "preserved")
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
// whose base comes from its prefix in YAML 1.2's core schema, 0o or 0x. Reading
// the source spelling as base 10 would store 644 where the document said 420,
// and would do it silently.
func assertYAMLIntegerBases(t *testing.T, doc *ir.Document, model *ir.Model) {
	t.Helper()
	mode, ok := propByWire(model, "mode")
	require.True(t, ok)
	require.NotNil(t, mode.Default)
	assert.Equal(t, ir.BigVal("420"), mode.Default.Number, "0o644 is octal in YAML 1.2")

	enum, ok := doc.Types[mode.Type.Target].(*ir.Enum)
	require.True(t, ok)
	require.Len(t, enum.Members, 2)
	assert.Equal(t, ir.BigVal("420"), enum.Members[0].Value.Number)
	assert.Equal(t, ir.BigVal("493"), enum.Members[1].Value.Number)

	bounds, ok := propByWire(model, "bounds")
	require.True(t, ok)
	require.NotNil(t, bounds.Constraints)
	require.NotNil(t, bounds.Constraints.Min)
	require.NotNil(t, bounds.Constraints.Max)
	require.NotNil(t, bounds.Constraints.MultipleOf)
	assert.Equal(t, ir.BigVal("15"), *bounds.Constraints.Min, "0o17 is octal")
	assert.Equal(t, ir.BigVal("31"), *bounds.Constraints.Max, "0x1f is hex")
	assert.Equal(t, ir.BigVal("10"), *bounds.Constraints.MultipleOf, "10 is a plain integer")
	require.Len(t, bounds.Examples, 1)
	require.NotNil(t, bounds.Examples[0].Value)
	assert.Equal(t, ir.BigVal("30"), bounds.Examples[0].Value.Number, "30 is a plain integer")

	// A plain digit string is the integer it spells, and a leading zero is a
	// base ten integer that still has to come out as a JSON-valid literal.
	counted, ok := propByWire(model, "counted")
	require.True(t, ok)
	require.NotNil(t, counted.Default)
	assert.Equal(t, ir.BigVal("1000000"), counted.Default.Number)
	loose, ok := propByWire(model, "loose")
	require.True(t, ok)
	require.NotNil(t, loose.Default)
	assert.Equal(t, ir.BigVal("9"), loose.Default.Number)
}

// assertReferenceBounds pins that a bound written beside a schema that names
// another is read by the types the position admits: held where the named type
// takes it, and kept verbatim with one report where it does not. Failure's
// positions each keep what their named type cannot take, and `taken` and
// `open` are the controls that hold theirs.
func assertReferenceBounds(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	const failure = "/components/schemas/Failure/properties/"
	reported := []string{
		failure + "refined", failure + "site", failure + "nullable", failure + "voiced/allOf/1",
		failure + "chained", failure + "inline", failure + "composed", failure + "counted",
		"/components/schemas/Aliased",
		"/paths/~1names~1{name}/get/parameters/0/schema", "/paths/~1names~1{name}/get/parameters/1/schema",
		"/paths/~1names~1{name}/get/responses/200/headers/X-Tags/schema",
	}
	got := make([]string, 0, len(diags))
	for _, diagnostic := range diags {
		assert.Equal(t, diag.DegradedConstruct, diagnostic.Code, "%+v", diagnostic)
		assert.Equal(t, ir.SeverityInfo, diagnostic.Severity, "%+v", diagnostic)
		got = append(got, string(diagnostic.Provenance.Pointer))
	}
	assert.ElementsMatch(t, reported, got)

	holder, ok := doc.Types[componentID("Failure")].(*ir.Model)
	require.True(t, ok)
	for wire, key := range map[string]string{
		"refined": "openapi:minLength", "site": "openapi:minLength", "nullable": "openapi:minLength",
		"voiced": "openapi:minLength", "chained": "openapi:minLength", "counted": "openapi:minItems",
	} {
		property, found := propByWire(holder, wire)
		require.True(t, found, wire)
		assert.Nil(t, property.Constraints, wire)
		assert.Equal(t, ir.ReasonDegradedLowering, unmodeledEntry(t, property.Unmodeled, key).Reason, wire)
	}
	inline, found := propByWire(holder, "inline")
	require.True(t, found)
	assert.Nil(t, inline.Constraints)
	assert.Empty(t, inline.Unmodeled, "the list the entry builds stands at the property and keeps the bound")
	list, ok := doc.Types[inline.Type.Target].(*ir.List)
	require.True(t, ok, "%T", doc.Types[inline.Type.Target])
	assert.Equal(t, ir.TypeID("type/anonymous/components/schemas/Failure/properties/inline"), list.ID)
	assert.Equal(t, ir.ReasonDegradedLowering, unmodeledEntry(t, list.Unmodeled, "openapi:minLength").Reason)

	composed, found := propByWire(holder, "composed")
	require.True(t, found)
	minProps := &ir.Constraints{MinProperties: new(int64(2))}
	assert.Nil(t, composed.Constraints, "the property leaves the count to the model it owns")
	model, ok := doc.Types[composed.Type.Target].(*ir.Model)
	require.True(t, ok, "%T", doc.Types[composed.Type.Target])
	assert.Equal(t, minProps, model.Constraints)
	assert.Contains(t, model.Unmodeled, "openapi:minLength")

	taken, found := propByWire(holder, "taken")
	require.True(t, found)
	assert.Equal(t, &ir.Constraints{MinLength: new(int64(1))}, taken.Constraints)
	assert.Empty(t, taken.Unmodeled)
	open, found := propByWire(holder, "open")
	require.True(t, found)
	assert.Equal(t, &ir.Constraints{Min: new(ir.BigVal("2")), MinLength: new(int64(1))}, open.Constraints)
	assert.Empty(t, open.Unmodeled)

	aliased, ok := doc.Types[componentID("Aliased")].(*ir.Scalar)
	require.True(t, ok)
	assert.Nil(t, aliased.Constraints)
	assert.Contains(t, aliased.Unmodeled, "openapi:minimum")

	op, found := opByName(doc, "getName")
	require.True(t, found)
	name, found := openapitest.ParamBySource(op, "name")
	require.True(t, found)
	assert.Equal(t, &ir.Constraints{MinLength: new(int64(2))}, name.Constraints)
	assert.Contains(t, name.Unmodeled, "openapi:schema/minimum")
	tags, found := openapitest.ParamBySource(op, "tags")
	require.True(t, found)
	assert.Equal(t, &ir.Constraints{MaxItems: new(int64(3))}, tags.Constraints)
	assert.Contains(t, tags.Unmodeled, "openapi:schema/maxLength")
	require.Len(t, op.Responses, 1)
	header, found := headerByWire(op.Responses[0].Headers, "X-Tags")
	require.True(t, found)
	assert.Nil(t, header.Constraints)
	assert.Contains(t, header.Unmodeled, "openapi:schema/pattern")
}

// assertUnionJoinedValueSets holds union-joined-value-sets to
// docs/compilers/openapi.md#allof-refinement: the value sets of a union's
// branches are one set to the conjunct beside them, whether the branches share
// members, nest, or number more than a set joins beside its first. A set lying
// within what the union admits is the position's, and the union is kept; one
// that does not leaves the union the position's node, with the set kept.
func assertUnionJoinedValueSets(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	memberStrings := func(enum *ir.Enum) []string {
		out := make([]string, 0, len(enum.Members))
		for _, enumMember := range enum.Members {
			out = append(out, enumMember.Value.String)
		}
		return out
	}

	nested, ok := typeByName(doc, "Nested").(*ir.Enum)
	require.True(t, ok, "a set within the nested union's members is the position")
	assert.Equal(t, []string{"s1", "p"}, memberStrings(nested))
	assertKeptRaw(t, nested.Unmodeled, "openapi:anyOf", `[{"enum":["s1","s2","s3","s4","s5"]},{"anyOf":[{"enum":["p","q"]},{"enum":["s1"]}]}]`)

	shared, ok := typeByName(doc, "Shared").(*ir.Union)
	require.True(t, ok, "a set outside what the union admits leaves the union the position's")
	assert.Len(t, shared.Variants, 2)
	assertKeptRaw(t, shared.Unmodeled, "openapi:enum", `["b","x"]`)

	many, ok := typeByName(doc, "Many").(*ir.Union)
	require.True(t, ok, "the ten branches are the union the position is")
	assert.Len(t, many.Variants, 10)
	assertKeptRaw(t, many.Unmodeled, "openapi:enum", `["v3","zz"]`)

	assertNoErrorDiags(t, diags)
}
