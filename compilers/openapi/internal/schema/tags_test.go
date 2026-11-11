// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// taggedVariants declares Json and Text, objects requiring a `type` each pins
// to a value other than its name, and Plain, which pins nothing.
const taggedVariants = "    Json: {type: object, required: [type], properties: {type: {type: string, enum: [json]}}}\n" +
	"    Text: {type: object, required: [type], properties: {type: {const: text}}}\n" +
	"    Plain: {type: object, required: [type], properties: {type: {type: string}}}\n"

// TestDiscriminatorInference_MapsAnUnmappedDiscriminator pins the policy's
// reading of a declared discriminator with no mapping
// (docs/compilers/openapi.md#discriminator-inference): where every variant pins
// the tag to a distinct string and one of them is not the name the implicit
// mapping routes it by, the mapping is read off the pinned values, the union
// marked inferred and the reading reported once. Where the names are the
// values, or a variant pins none or one another pins, nothing is read in.
func TestDiscriminatorInference_MapsAnUnmappedDiscriminator(t *testing.T) {
	t.Parallel()
	const disc = "      discriminator: {propertyName: type}\n"
	cases := []struct {
		name, union, rest string
		want              map[string]string // tag -> the variant's target, "" for no mapping
		report            string
	}{
		{"names disagree", "      oneOf: [{$ref: '#/components/schemas/Json'}, {$ref: '#/components/schemas/Text'}]\n", "",
			map[string]string{"json": "Json", "text": "Text"}, `"json" where it routes by "Json", "text" where it routes by "Text"`},
		{"inline variant", "      oneOf: [{$ref: '#/components/schemas/json'}, " +
			"{type: object, required: [type], properties: {type: {const: other}}}]\n",
			"    json: {type: object, required: [type], properties: {type: {const: json}}}\n",
			map[string]string{"json": "json", "other": "U/oneOf/1"}, `"other" where it names none`},
		{"beside a null branch", "      oneOf: [{$ref: '#/components/schemas/Json'}, {type: 'null'}, {$ref: '#/components/schemas/Text'}]\n", "",
			map[string]string{"json": "Json", "text": "Text"}, `"json" where it routes by "Json"`},
		{"names agree", "      oneOf: [{$ref: '#/components/schemas/json'}, {$ref: '#/components/schemas/text'}]\n",
			"    json: {type: object, required: [type], properties: {type: {const: json}}}\n" +
				"    text: {type: object, required: [type], properties: {type: {const: text}}}\n", nil, ""},
		{"a variant pins none", "      oneOf: [{$ref: '#/components/schemas/Json'}, {$ref: '#/components/schemas/Plain'}]\n", "", nil, ""},
		{"two variants pin one", "      oneOf: [{$ref: '#/components/schemas/Json'}, {$ref: '#/components/schemas/Json2'}]\n",
			"    Json2: {type: object, required: [type], properties: {type: {const: json}}}\n", nil, ""},
		{"through an alias", "      anyOf: [{$ref: '#/components/schemas/Aliased'}, {$ref: '#/components/schemas/Text'}]\n",
			"    Aliased: {type: object, required: [type], properties: {type: {$ref: '#/components/schemas/TagAlias'}}}\n" +
				"    TagAlias: {$ref: '#/components/schemas/Tag'}\n    Tag: {const: tagged}\n",
			map[string]string{"tagged": "Aliased", "text": "Text"}, `"tagged" where it routes by "Aliased"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			for _, doc := range taggedOrders(t, "    U:\n"+disc+c.union, taggedVariants+c.rest) {
				u := taggedUnion(t, doc)
				require.NotNil(t, u.Discriminator)
				assert.False(t, u.Discriminator.Inferred, "the discriminator is declared")
				if c.want == nil {
					assert.Nil(t, u.Discriminator.Mapping, "the implicit mapping stands")
					assert.Empty(t, u.Provenance.Inferred)
					assert.False(t, openapitest.HasDiagCodeAt(doc.Diagnostics, diag.InferredDiscriminator, unionAt+"/discriminator"))
					continue
				}
				assert.Equal(t, mappingOf(c.want), u.Discriminator.Mapping)
				assert.Equal(t, openapi.DiscriminatorInferenceHeuristic, u.Provenance.Inferred)
				assert.Contains(t, openapitest.DiagMessageAt(t, doc.Diagnostics, diag.InferredDiscriminator, ir.SeverityInfo,
					unionAt+"/discriminator"), c.report)
			}
		})
	}
}

// TestDiscriminatorInference_LeavesADeclaredMapping pins that a mapping the
// document writes decides the tags, whatever the variants pin: the policy
// reads only a discriminator declaring none.
func TestDiscriminatorInference_LeavesADeclaredMapping(t *testing.T) {
	t.Parallel()
	union := "    U:\n      discriminator: {propertyName: type, mapping: {j: Json, t: '#/components/schemas/Text'}}\n" +
		"      oneOf: [{$ref: '#/components/schemas/Json'}, {$ref: '#/components/schemas/Text'}]\n"
	for _, doc := range taggedOrders(t, union, taggedVariants) {
		u := taggedUnion(t, doc)
		require.NotNil(t, u.Discriminator)
		assert.Equal(t, mappingOf(map[string]string{"j": "Json", "t": "Text"}), u.Discriminator.Mapping)
		assert.Empty(t, u.Provenance.Inferred)
		assert.Zero(t, openapitest.CountDiagsAt(doc.Diagnostics, diag.InferredDiscriminator, ir.SeverityInfo))
	}
}

// TestDiscriminatorInference_NamesAFewDisagreements pins that the report names
// the first few variants whose pinned tag the implicit mapping misses and
// counts the rest, so a union of many stays one line.
func TestDiscriminatorInference_NamesAFewDisagreements(t *testing.T) {
	t.Parallel()
	var branches, variants strings.Builder
	for i := range 5 {
		fmt.Fprintf(&branches, "{$ref: '#/components/schemas/V%d'}, ", i)
		fmt.Fprintf(&variants, "    V%d: {type: object, required: [k], properties: {k: {const: v%d}}}\n", i, i)
	}
	union := "    U:\n      discriminator: {propertyName: k}\n      oneOf: [" + strings.TrimSuffix(branches.String(), ", ") + "]\n"
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", union+variants.String()))
	openapitest.RequireNoErrorDiags(t, diags)
	require.Len(t, taggedUnion(t, doc).Discriminator.Mapping, 5)
	assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.InferredDiscriminator, ir.SeverityInfo, unionAt+"/discriminator"),
		`"v2" where it routes by "V2", 2 more)`)
}

// TestDiscriminatorInference_InfersAnUndeclaredDiscriminator pins the policy's
// reading of a union declaring no discriminator: where every variant requires
// one property and pins it to a distinct string, the first such property of
// the first variant is the inferred tag, and the union is tagged by it. A
// property a variant may leave out, lacks, may hold null or pins to no string,
// and a variant that is no model, read nothing in.
func TestDiscriminatorInference_InfersAnUndeclaredDiscriminator(t *testing.T) {
	t.Parallel()
	variant := func(name, props string, required ...string) string {
		return fmt.Sprintf("    %s: {type: object, required: [%s], properties: {%s}}\n", name, strings.Join(required, ", "), props)
	}
	union := "    U:\n      oneOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]\n"
	cases := []struct {
		name, rest string
		tag        string
		want       map[string]string
	}{
		{"pinned and required", variant("A", "kind: {const: a}", "kind") + variant("B", "kind: {type: string, enum: [b]}", "kind"),
			"kind", map[string]string{"a": "A", "b": "B"}},
		{"the first property of the first variant", variant("A", "x: {const: a}, y: {const: c}", "x", "y") +
			variant("B", "y: {const: d}, x: {const: b}", "x", "y"), "x", map[string]string{"a": "A", "b": "B"}},
		{"a later property where an earlier one is shared", variant("A", "v: {const: one}, kind: {const: a}", "v", "kind") +
			variant("B", "v: {const: one}, kind: {const: b}", "v", "kind"), "kind", map[string]string{"a": "A", "b": "B"}},
		{"not required", variant("A", "kind: {const: a}", "kind") + variant("B", "kind: {const: b}, other: {type: string}", "other"), "", nil},
		{"nullable", variant("A", "kind: {const: a}", "kind") + variant("B", "kind: {type: [string, 'null'], enum: [b, null]}", "kind"), "", nil},
		{"a number", variant("A", "kind: {const: 1}", "kind") + variant("B", "kind: {const: 2}", "kind"), "", nil},
		{"absent from a variant", variant("A", "kind: {const: a}", "kind") + variant("B", "other: {const: b}", "other"), "", nil},
		{"a variant no model", variant("A", "kind: {const: a}", "kind") + "    B: {type: string}\n", "", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			for _, doc := range taggedOrders(t, union, c.rest) {
				u := taggedUnion(t, doc)
				if c.want == nil {
					assert.Nil(t, u.Discriminator)
					assert.False(t, u.WireTagged)
					assert.Empty(t, u.Provenance.Inferred)
					continue
				}
				assert.True(t, u.WireTagged)
				assert.Equal(t, &ir.Discriminator{PropertyName: c.tag, Mapping: mappingOf(c.want), Inferred: true}, u.Discriminator)
				assert.Equal(t, openapi.DiscriminatorInferenceHeuristic, u.Provenance.Inferred)
				assert.False(t, openapitest.HasDiagCodeAt(doc.Diagnostics, diag.InferredDiscriminator, unionAt),
					"an undeclared discriminator departs from nothing the document says, so it is not reported")
			}
		})
	}
}

// TestDiscriminatorInference_LeavesALoneOrNullableVariant pins the unions an
// undeclared discriminator is never inferred for though every variant pins a
// tag: one naming a schema twice, whose two variants share the tag, one whose
// variant admits null, which no tag names, and one of a single variant, which
// a tag tells from nothing.
func TestDiscriminatorInference_LeavesALoneOrNullableVariant(t *testing.T) {
	t.Parallel()
	const a = "    A: {type: object, required: [kind], properties: {kind: {const: a}}}\n"
	for name, rest := range map[string]string{
		"one schema twice": "    U: {oneOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/A'}]}\n" + a,
		"nullable": "    U: {oneOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]}\n" + a +
			"    B: {type: [object, 'null'], required: [kind], properties: {kind: {const: b}}}\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", rest))
			openapitest.RequireNoErrorDiags(t, diags)
			u := taggedUnion(t, doc)
			assert.Nil(t, u.Discriminator, "got %+v", u.Variants)
		})
	}
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    U: {oneOf: [{$ref: '#/components/schemas/A'}]}\n"+a))
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Nil(t, taggedUnion(t, doc).Discriminator, "one variant needs no tag")
}

// TestDiscriminatorInference_DisabledReadsNothing pins that the policy turned
// off leaves both readings undone: the unmapped discriminator routes by name
// and the undeclared union stays untagged, nothing marked or reported.
func TestDiscriminatorInference_DisabledReadsNothing(t *testing.T) {
	t.Parallel()
	src := openapitest.ComponentSpecVer("3.1.0", "    U:\n      discriminator: {propertyName: type}\n"+
		"      oneOf: [{$ref: '#/components/schemas/Json'}, {$ref: '#/components/schemas/Text'}]\n"+
		"    V: {anyOf: [{$ref: '#/components/schemas/Json'}, {$ref: '#/components/schemas/Text'}]}\n"+taggedVariants)
	doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{openapitest.SourceOf(src)},
		compilers.Options{FormatOptions: openapi.Options{DiscriminatorInference: openapi.DiscriminatorInference{Disabled: true}}})
	require.NoError(t, err)
	openapitest.RequireNoErrorDiags(t, diags)
	u := taggedUnion(t, doc)
	require.NotNil(t, u.Discriminator)
	assert.Nil(t, u.Discriminator.Mapping)
	v, ok := typeByName(doc, "V").(*ir.Union)
	require.True(t, ok)
	assert.Nil(t, v.Discriminator)
	for _, n := range []*ir.Union{u, v} {
		assert.Empty(t, n.Provenance.Inferred)
	}
	assert.Zero(t, openapitest.CountDiagsAt(diags, diag.InferredDiscriminator, ir.SeverityInfo))
}

// TestDiscriminatorInference_ReadsADistributedUnion pins that the reading
// meets a union whose variants compose the body beside it: each variant's tag
// is read across its composition, and the mapping read off them replaces the
// implicit one written out for the composed variants.
func TestDiscriminatorInference_ReadsADistributedUnion(t *testing.T) {
	t.Parallel()
	union := "    U:\n      type: object\n      properties: {note: {type: string}}\n" +
		"      discriminator: {propertyName: type}\n" +
		"      oneOf: [{$ref: '#/components/schemas/Json'}, {$ref: '#/components/schemas/Text'}]\n"
	for _, doc := range taggedOrders(t, union, taggedVariants) {
		u := taggedUnion(t, doc)
		require.Len(t, u.Variants, 2)
		require.NotNil(t, u.Discriminator)
		assert.Equal(t, map[string]ir.TypeID{"json": u.Variants[0].Type.Target, "text": u.Variants[1].Type.Target},
			u.Discriminator.Mapping)
		assert.Equal(t, openapi.DiscriminatorInferenceHeuristic, u.Provenance.Inferred)
	}
}

// unionAt is where taggedOrders' union U is written.
const unionAt = "/components/schemas/U"

// taggedOrders compiles union and rest in both declaration orders, requires
// each to validate and the two to agree on their types and on what they
// report, and returns both: the reading runs once every node is interned, so a
// variant lowered after the union must read alike.
func taggedOrders(t *testing.T, union, rest string) []*ir.Document {
	t.Helper()
	docs := make([]*ir.Document, 0, 2)
	for _, src := range []string{union + rest, rest + union} {
		doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", src))
		openapitest.RequireNoErrorDiags(t, diags)
		assert.Empty(t, validated(t, doc), "the document must validate")
		doc.Diagnostics = diags
		docs = append(docs, doc)
	}
	assert.Empty(t, cmp.Diff(docs[0].Types, docs[1].Types, orderInvariantIR()...),
		"declaration order must not change the types")
	assert.Equal(t, reported(docs[0].Diagnostics), reported(docs[1].Diagnostics))
	return docs
}

// taggedUnion returns the union U lowers to.
func taggedUnion(t *testing.T, doc *ir.Document) *ir.Union {
	t.Helper()
	u, ok := typeByName(doc, "U").(*ir.Union)
	require.True(t, ok, "U lowers to a union; got %T", typeByName(doc, "U"))
	return u
}

// mappingOf spells a mapping by the component names, or the positions under
// U, its targets lower from.
func mappingOf(want map[string]string) map[string]ir.TypeID {
	out := make(map[string]ir.TypeID, len(want))
	for tag, target := range want {
		if strings.HasPrefix(target, "U/") {
			out[tag] = ir.TypeID("t/anon/components/schemas/" + target)
			continue
		}
		out[tag] = componentID(target)
	}
	return out
}
