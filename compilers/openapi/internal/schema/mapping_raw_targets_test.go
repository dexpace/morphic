package schema_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// rawBase is a discriminated Pet whose mapping is mapping, followed by whatever
// the row declares after it.
func rawBase(mapping string) string {
	return "    Pet:\n" +
		"      type: object\n" +
		"      required: [kind]\n" +
		"      properties: {kind: {type: string}}\n" +
		"      discriminator: {propertyName: kind, " + mapping + "}\n"
}

// rawSpec writes components in the order given, and the root-level extensions
// tail after them. With reversed set it declares the components in the opposite
// order and the extensions ahead of them.
func rawSpec(version string, components []string, tail string, reversed bool) string {
	if !reversed {
		return openapitest.ComponentSpecVer(version, strings.Join(components, "")) + tail
	}
	rev := make([]string, len(components))
	for i, c := range components {
		rev[len(components)-1-i] = c
	}
	doc := openapitest.ComponentSpecVer(version, strings.Join(rev, ""))
	return strings.Replace(doc, "paths: {}\n", tail+"paths: {}\n", 1)
}

// wantMapping names a discriminator, one of its mapping entries (or its default
// when tag is empty), and the type of the given kind that entry must resolve to.
type wantMapping struct {
	base   ir.TypeID
	tag    string
	target ir.TypeID
	kind   ir.TypeKind
}

// TestDiscriminatorMapping_ToRawTargetsIsOrderInvariant pins that a mapping or
// defaultMapping naming a position the parsed model holds as raw YAML, an
// extension's value or an enum member, resolves to the type at that position
// whichever order the document declares things in (GitHub #757). Each row is
// written in the order that failed, with the mapping before any $ref to its
// target, and compiled again reversed. Neither compile may report an error,
// and both must produce the same registry, except for the row that records why
// not.
func TestDiscriminatorMapping_ToRawTargetsIsOrderInvariant(t *testing.T) {
	t.Parallel()
	const (
		petSub = "allOf: [{$ref: '#/components/schemas/Pet'}], type: object"
		cat    = "  Cat: {" + petSub + ", properties: {meow: {type: string}}}\n"
	)
	pet := componentID("Pet")
	anon := func(pointer string) ir.TypeID { return ir.TypeID("t/anon/" + pointer) }
	model := func(base ir.TypeID, tag, pointer string) wantMapping {
		return wantMapping{base: base, tag: tag, target: anon(pointer), kind: ir.KindModel}
	}
	tests := []struct {
		name       string
		version    string
		components []string
		tail       string
		wants      []wantMapping
		// registryDiffers, when set, is why the two registries may differ.
		registryDiffers string
	}{
		{
			name:       "an extension's value named by a mapping alone",
			components: []string{rawBase("mapping: {c: '#/x-lib/Cat'}")},
			tail:       "x-lib:\n" + cat,
			wants:      []wantMapping{model(pet, "c", "x-lib/Cat")},
		},
		{
			name: "an extension's value also named by a $ref",
			components: []string{
				rawBase("mapping: {c: '#/x-lib/Cat'}"),
				"    Holder: {$ref: '#/x-lib/Cat'}\n",
			},
			tail:  "x-lib:\n" + cat,
			wants: []wantMapping{model(pet, "c", "x-lib/Cat")},
		},
		{
			name: "an enum member",
			components: []string{
				rawBase("mapping: {e: '#/components/schemas/Kennel/properties/e/enum/0'}"),
				"    Kennel:\n      type: object\n      properties:\n        e:\n          enum:\n" +
					"            - {type: object, properties: {wingspan: {type: number}}}\n",
			},
			// A member that is an object is lowered to a literal, which no model
			// subtypes, so only the resolution is pinned here, not a variant.
			wants: []wantMapping{{pet, "e", anon("components/schemas/Kennel/properties/e/enum/0"), ir.KindLiteral}},
			registryDiffers: "a reference to an enum member also lowers it as a schema in one order, " +
				"interning the member's property types too (GitHub #730)",
		},
		{
			name:       "a defaultMapping to an extension's value",
			version:    "3.2.0",
			components: []string{rawBase("defaultMapping: '#/x-lib/Cat'")},
			tail:       "x-lib:\n" + cat,
			wants:      []wantMapping{model(pet, "", "x-lib/Cat")},
		},
		{
			name:       "a mapping in a raw schema only a $ref reaches",
			components: []string{"    Holder: {$ref: '#/x-lib/Inner'}\n"},
			tail: "x-lib:\n" +
				"  Inner:\n    type: object\n    required: [tag]\n    properties: {tag: {type: string}}\n" +
				"    discriminator: {propertyName: tag, mapping: {d: '#/x-lib/Dog'}}\n" +
				"  Dog: {allOf: [{$ref: '#/x-lib/Inner'}], type: object}\n",
			wants: []wantMapping{model(anon("x-lib/Inner"), "d", "x-lib/Dog")},
		},
		{
			name:       "a mapping in a raw schema only another mapping reaches",
			components: []string{rawBase("mapping: {i: '#/x-lib/Inner'}")},
			tail: "x-lib:\n" +
				"  Inner:\n    allOf: [{$ref: '#/components/schemas/Pet'}]\n    type: object\n" +
				"    required: [tag]\n    properties: {tag: {type: string}}\n" +
				"    discriminator: {propertyName: tag, mapping: {d: '#/x-lib/Dog'}}\n" +
				"  Dog: {allOf: [{$ref: '#/x-lib/Inner'}], type: object}\n",
			wants: []wantMapping{
				model(pet, "i", "x-lib/Inner"),
				model(anon("x-lib/Inner"), "d", "x-lib/Dog"),
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			version := tc.version
			if version == "" {
				version = "3.1.0"
			}
			first, diags := parseFull(t, rawSpec(version, tc.components, tc.tail, false))
			openapitest.RequireNoErrorDiags(t, diags)
			last, diags := parseFull(t, rawSpec(version, tc.components, tc.tail, true))
			openapitest.RequireNoErrorDiags(t, diags)

			for _, doc := range []*ir.Document{first, last} {
				for _, want := range tc.wants {
					base, ok := doc.Types[want.base].(*ir.Model)
					require.True(t, ok, "%s is a model", want.base)
					require.NotNil(t, base.Discriminator, "%s is discriminated", want.base)

					got := base.Discriminator.Default
					if want.tag != "" {
						got = base.Discriminator.Mapping[want.tag]
					}
					assert.Equal(t, want.target, got, "%s %q", want.base, want.tag)

					target, ok := doc.Types[want.target]
					require.True(t, ok, "the target %s is in the registry", want.target)
					assert.Equal(t, want.kind, target.Kind(), "the kind of %s", want.target)
				}
			}

			if tc.registryDiffers != "" {
				t.Log(tc.registryDiffers)
				return
			}
			assert.Empty(t, cmp.Diff(first.Types, last.Types),
				"declaring the mapping before or after what names its target must not change the registry")
		})
	}
}
