// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"encoding/json/jsontext"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// TestRef_InAnotherDocumentsContentIsReadInThatDocument pins how a schema $ref
// in content another document holds is read: in that document, which declares
// nothing at the pointer it spells although the source does (GitHub #762). It
// is reported once, unresolved, at the $ref where that document writes it, and
// lowers as any, naming nothing of the source's.
func TestRef_InAnotherDocumentsContentIsReadInThatDocument(t *testing.T) {
	t.Parallel()
	doc, diags, dir := compileBeside(t, map[string]string{"ext.yaml": `paths:
  /x:
    get:
      responses:
        "200":
          description: ok
          content: {application/json: {schema: {$ref: '#/components/schemas/Thing'}}}
`}, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /p: {$ref: './ext.yaml#/paths/~1x'}
components:
  schemas:
    Thing: {type: object}
`)

	want := []ir.Diagnostic{{Severity: ir.SeverityError, Code: "openapi/unresolved-ref",
		Message:    `unresolved $ref "#/components/schemas/Thing": it names nothing in the document it points into`,
		Provenance: ir.Provenance{Source: 1, Pointer: "/paths/~1x/get/responses/200/content/application~1json/schema"}}}
	assert.Empty(t, cmp.Diff(want, diags))
	assert.Equal(t, filepath.Join(dir, "ext.yaml"), doc.Sources[1].Path)
	assert.Equal(t, ir.TypeID("t/prim/any"), onlyBodyTarget(t, doc))
}

// TestUnresolvedRef_InAnotherDocumentsContentIsReportedOnceWhereItIsWritten
// pins the report of a $ref in content another document holds, at an allOf
// branch and at a union branch beside structural keywords. One that document
// does not resolve, because it declares nothing there or the $ref names a
// third document that cannot be read, draws one report, at the $ref in that
// document, in the same words at either position. One it resolves draws none
// and names what that document declares.
func TestUnresolvedRef_InAnotherDocumentsContentIsReportedOnceWhereItIsWritten(t *testing.T) {
	t.Parallel()
	const (
		allOf = "/paths/~1x/get/responses/200/content/application~1json/schema/allOf/0"
		oneOf = "/paths/~1x/get/responses/200/content/application~1json/schema/oneOf/0"
		gone  = "it names nothing in the document it points into"
	)
	// unread is why a $ref in dir's ext.yaml naming its third.yaml is unresolved.
	unread := func(dir string) string {
		return "the document it names could not be read: open " + filepath.Join(dir, "third.yaml") +
			": no such file or directory"
	}
	allOfOf := func(ref string) string { return "{allOf: [{$ref: '" + ref + "'}, {type: object}]}" }
	oneOfOf := func(ref string) string {
		return "{type: object, properties: {x: {type: string}}, oneOf: [{$ref: '" + ref + "'}]}"
	}
	for _, tc := range []struct {
		name, schema, at, ref string
		why                   func(dir string) string // nil when the $ref resolves
	}{
		{"an allOf branch it declares", allOfOf("#/components/schemas/Thing"), allOf, "", nil},
		{"a union branch it declares", oneOfOf("#/components/schemas/Thing"), oneOf, "", nil},
		{"an allOf branch it lacks", allOfOf("#/components/schemas/Gone"), allOf, "#/components/schemas/Gone",
			func(string) string { return gone }},
		{"a union branch it lacks", oneOfOf("#/components/schemas/Gone"), oneOf, "#/components/schemas/Gone",
			func(string) string { return gone }},
		{"an allOf branch naming a third document", allOfOf("./third.yaml#/Thing"), allOf, "./third.yaml#/Thing", unread},
		{"a union branch naming a third document", oneOfOf("./third.yaml#/Thing"), oneOf, "./third.yaml#/Thing", unread},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags, dir := compileBeside(t, map[string]string{"ext.yaml": `paths:
  /x:
    get:
      responses:
        "200":
          description: ok
          content: {application/json: {schema: ` + tc.schema + `}}
components:
  schemas:
    Thing: {type: object}
`}, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /p: {$ref: './ext.yaml#/paths/~1x'}
components:
  schemas:
    Thing: {type: object}
`)

			var want []ir.Diagnostic
			if tc.why != nil {
				want = []ir.Diagnostic{{Severity: ir.SeverityError, Code: "openapi/unresolved-ref",
					Message:    `unresolved $ref "` + tc.ref + `": ` + tc.why(dir),
					Provenance: ir.Provenance{Source: 1, Pointer: jsontext.Pointer(tc.at)}}}
			}
			assert.Empty(t, cmp.Diff(want, withCode(diags, "openapi/unresolved-ref")))
			if tc.why == nil {
				thing, ok := doc.Types[ids.In("ext.yaml").NamedType("/components/schemas/Thing")]
				require.True(t, ok, "ext.yaml's Thing is lowered")
				assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/components/schemas/Thing"},
					thing.Common().Provenance)
			}
		})
	}
}

// TestRef_IntoAnotherDocumentNamesTheTypeLoweredThere pins what a schema $ref
// that lands in another document draws: it resolves, so nothing is reported,
// and the schema it lands on is lowered where that document declares it, under
// the label that document is read by, which is the type the $ref names.
func TestRef_IntoAnotherDocumentNamesTheTypeLoweredThere(t *testing.T) {
	t.Parallel()
	doc, diags, dir := compileBeside(t, map[string]string{"other.yaml": "X: {type: string}\n"},
		`openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    A: {$ref: './other.yaml#/X'}
`)

	assert.Empty(t, diags)
	assert.Equal(t, filepath.Join(dir, "other.yaml"), doc.Sources[1].Path)
	x, ok := doc.Types[ids.In("other.yaml").AnonType("/X")].(*ir.Scalar)
	require.True(t, ok, "other.yaml's X is lowered")
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/X"}, x.Provenance)
	require.NotNil(t, x.Base)
	assert.Equal(t, ir.TypeID("t/prim/string"), x.Base.Target)
	a, ok := doc.Types[ids.Document{}.NamedType("/components/schemas/A")].(*ir.Scalar)
	require.True(t, ok)
	require.NotNil(t, a.Base)
	assert.Equal(t, x.ID, a.Base.Target, "A names the schema other.yaml declares")
}

// TestRef_IntoADocumentTheCompileDoesNotListIsUnresolved pins what a lowering
// does with a $ref the bindings land in a document its load lists no
// Document.Sources entry for: it lowers nothing at that document's
// coordinates, and reports each such $ref, and a discriminator mapping naming
// the same target, unresolved where the source writes it, though the pointer
// it spells is a source component's. Such a mapping tags no subtype, which
// takes its own name.
func TestRef_IntoADocumentTheCompileDoesNotListIsUnresolved(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ext.yaml"),
		[]byte("components:\n  schemas:\n    Sub: {type: object, properties: {kind: {type: string}}}\n"), 0o600))
	loaded, diags, err := load.Load(t.Context(), 0, compilers.Source{Path: filepath.Join(dir, "root.yaml"), Data: []byte(`openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    A: {$ref: './ext.yaml#/components/schemas/Sub'}
    Pet:
      oneOf: [{$ref: './ext.yaml#/components/schemas/Sub'}]
      discriminator: {propertyName: kind, mapping: {x: './ext.yaml#/components/schemas/Sub'}}
    Base:
      type: object
      properties: {kind: {type: string}}
      discriminator: {propertyName: kind, mapping: {x: './ext.yaml#/components/schemas/Sub'}}
    Sub: {allOf: [{$ref: '#/components/schemas/Base'}]}
`)}, load.Options{Reader: reader.Disk{}})
	require.NoError(t, err)
	require.Empty(t, diags)
	require.Len(t, loaded.Others.List(), 1, "sanity: the load reads ext.yaml")
	unlisted := *loaded
	unlisted.Others = load.Others{}

	l := schema.LowererOver(lowering.New(0, unlisted.Doc, unlisted.Source, lowering.GroupByTags, lowering.Limits{},
		lowering.StreamingMedia{}, lowering.ExtensionPromotions{}, lowering.NullableWidening{}, overlay.Origin{}).WithLoaded(&unlisted))
	l.Diags.AppendAll(schema.LowerComponentSchemas(t.Context(), l.Ctx, l.Types))
	doc := l.Document(t)

	unresolved := func(pointer jsontext.Pointer, message string) ir.Diagnostic {
		return ir.Diagnostic{Severity: ir.SeverityError, Code: "openapi/unresolved-ref", Message: message,
			Provenance: ir.Provenance{Pointer: pointer}}
	}
	assert.Empty(t, cmp.Diff([]ir.Diagnostic{
		unresolved("/components/schemas/A", `unresolved $ref "./ext.yaml#/components/schemas/Sub"`),
		unresolved("/components/schemas/Pet/oneOf/0", `unresolved $ref "./ext.yaml#/components/schemas/Sub"`),
		unresolved("/components/schemas/Pet/discriminator/mapping/x",
			`discriminator mapping "x" references unresolved schema "./ext.yaml#/components/schemas/Sub"`),
		unresolved("/components/schemas/Base/discriminator/mapping/x",
			`discriminator mapping "x" references unresolved schema "./ext.yaml#/components/schemas/Sub"`),
	}, withCode(l.Diags.List(), "openapi/unresolved-ref")))
	sub, ok := doc.Types[ids.Document{}.NamedType("/components/schemas/Sub")].(*ir.Model)
	require.True(t, ok)
	assert.Equal(t, "Sub", sub.DiscriminatorValue, "the mapping tags no subtype, which takes its own name")
	for id := range doc.Types {
		space, _ := ir.IDSpace(ir.IDKindType, string(id))
		assert.NotContains(t, space, "@", "%s is lowered in a document the compile does not list", id)
	}
}

func TestLower_UnresolvedRefDiagnostics(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Owner:
      type: object
      properties:
        ghost: {$ref: '#/components/schemas/Ghost'}
`)
	doc, diags := schema.LowerSpec(t, spec)
	require.NotNil(t, doc)
	assert.True(t, openapitest.HasDiag(diags, diag.UnresolvedRef), "unresolved ref diagnostic emitted")
}

// TestSchemaExamples_RefdSubSchemaKeepsThem pins the examples of a $ref'd
// internal sub-schema whose body reduces to a shared primitive: the alias
// hoisted for the reference is the first node its pointer owns, and the
// examples belong there rather than on the shared primitive every other string
// in the document also uses.
func TestSchemaExamples_RefdSubSchemaKeepsThem(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `
openapi: 3.1.0
info: {title: t, version: '1'}
paths:
  /x:
    get:
      operationId: x
      parameters:
        - {name: q, in: query, schema: {$ref: '#/components/parameters/P/schema'}}
      responses: {'200': {description: ok}}
components:
  parameters:
    P:
      name: p
      in: query
      schema: {type: string, example: sub-example}
`)
	openapitest.RequireNoErrorDiags(t, diags)
	td, ok := doc.Types["t/anon/components/parameters/P/schema"]
	require.True(t, ok, "the referenced sub-schema is hoisted at its own pointer")
	require.Len(t, td.Common().Examples, 1)
	require.NotNil(t, td.Common().Examples[0].Value)
	assert.Equal(t, "sub-example", td.Common().Examples[0].Value.Str)
	assert.Empty(t, doc.Types["t/prim/string"].Common().Examples,
		"the shared primitive carries no per-declaration annotation")
}

// TestSchemaExamples_ComponentRefSiblingKeepsThem pins the examples a component
// writes beside a $ref. They annotate the position they are written at, not the
// referent, so they belong on the alias this component interns — the rule a
// property ($ref plus a sibling example) and a hoisted sub-schema already
// follow. Reading them off the target instead would attribute one component's
// example to every other reference to the same schema.
func TestSchemaExamples_ComponentRefSiblingKeepsThem(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `
openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
components:
  schemas:
    Base: {type: string}
    Alias:
      $ref: '#/components/schemas/Base'
      example: alias-level
`)
	openapitest.RequireNoErrorDiags(t, diags)
	alias, ok := doc.Types["t/openapi/components/schemas/Alias"]
	require.True(t, ok)
	require.Len(t, alias.Common().Examples, 1)
	require.NotNil(t, alias.Common().Examples[0].Value)
	assert.Equal(t, "alias-level", alias.Common().Examples[0].Value.Str)

	base, ok := doc.Types["t/openapi/components/schemas/Base"]
	require.True(t, ok)
	assert.Empty(t, base.Common().Examples,
		"the referent carries no example the alias wrote beside its own $ref")
}

func TestSchema_EmptyFragmentRef(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Owner:
      type: object
      properties:
        p: {$ref: '#'}
`)
	doc, diags := schema.LowerSpec(t, spec)
	require.NotNil(t, doc)
	assert.True(t, openapitest.HasDiag(diags, diag.UnresolvedRef), "the '#' ref form is unresolved")
}

func TestSchema_EmptyStringRefMirrorBranches(t *testing.T) {
	t.Parallel()
	// An empty-string $ref has IsReference()==false (the ref value is "") yet a
	// non-nil oas3 Ref pointer, exercising that mirror path in schema.Ref and a
	// branch named by its position, since a $ref that suggests no name leaves it
	// its own.
	spec := openapitest.ComponentSpec(`    Owner:
      type: object
      properties:
        p: {$ref: ''}
    U:
      oneOf:
        - {$ref: ''}
        - {type: string}
`)
	doc, diags := schema.LowerSpec(t, spec)
	require.NotNil(t, doc)
	assert.GreaterOrEqual(t, openapitest.CountDiagsAt(diags, diag.UnresolvedRef, ir.SeverityError), 2, "both empty refs are unresolved")
	u, ok := typeByName(doc, "U").(*ir.Union)
	require.True(t, ok)
	assert.Contains(t, []string{u.Variants[0].Name.Hint, u.Variants[1].Name.Hint}, "variant_0")
}

// TestSchema_ARefThroughAnEmptyRefLendsNothing pins a use site whose $ref lands
// on a schema writing an empty $ref beside its annotations. That $ref names the
// document's root, which holds no schema, so the schema lowers to an alias over
// the top type and the chain ends on no schema: the use site takes no
// description, default or discriminator from it, as from one whose $ref names
// nothing.
func TestSchema_ARefThroughAnEmptyRefLendsNothing(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Empty: {$ref: '', description: from-empty, default: e}
    Base: {$ref: '', type: object, properties: {kind: {type: string}}, discriminator: {propertyName: kind}}
    Sub:
      type: object
      allOf: [{$ref: '#/components/schemas/Base'}]
    Owner:
      type: object
      properties:
        p: {$ref: '#/components/schemas/Empty'}
`)
	doc, _ := schema.LowerSpec(t, spec)
	require.NotNil(t, doc)
	owner, ok := typeByName(doc, "Owner").(*ir.Model)
	require.True(t, ok)
	require.Len(t, owner.Properties, 1)
	assert.Empty(t, owner.Properties[0].Docs.Description, "no description comes through the empty $ref")
	assert.Nil(t, owner.Properties[0].Default, "nor a default")
	sub, ok := typeByName(doc, "Sub").(*ir.Model)
	require.True(t, ok)
	assert.Empty(t, sub.DiscriminatorValue, "nor a discriminator Base's alias does not hold")
}

// TestComponentConstraints_RefSiblingKeepsThem pins the constraint counterpart
// of TestSchemaExamples_ComponentRefSiblingKeepsThem. A bound written beside a
// $ref constrains the position it is written at, so it belongs on the alias the
// component interns — as a property in the same shape already keeps one. The
// referent must not gain it: every other reference to that schema is unbounded.
func TestComponentConstraints_RefSiblingKeepsThem(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `
openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
components:
  schemas:
    Base: {type: integer}
    Bounded:
      $ref: '#/components/schemas/Base'
      minimum: 5
`)
	openapitest.RequireNoErrorDiags(t, diags)
	bounded, ok := doc.Types["t/openapi/components/schemas/Bounded"].(*ir.Scalar)
	require.True(t, ok, "a component aliasing another schema interns as a Scalar")
	require.NotNil(t, bounded.Constraints)
	require.NotNil(t, bounded.Constraints.Min)
	assert.Equal(t, ir.BigVal("5"), *bounded.Constraints.Min)

	base, ok := doc.Types["t/openapi/components/schemas/Base"]
	require.True(t, ok)
	if s, isScalar := base.(*ir.Scalar); isScalar {
		assert.Nil(t, s.Constraints, "the referent stays unbounded")
	}
}

// TestSchemaSiblings_RefdSubSchemaKeepsThem is the sub-schema arm of the rule
// TestSchemaExamples_ComponentRefSiblingKeepsThem pins for components: a $ref'd
// internal sub-schema that is itself a $ref carrying siblings keeps them on the
// alias hoisted at its own pointer. Resolving the chain to its end before
// hoisting would read the sub-schema as its target and lose both annotations
// before anything could record them — and would attribute nothing to the
// referent, which every other reference to it shares.
func TestSchemaSiblings_RefdSubSchemaKeepsThem(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `
openapi: 3.1.0
info: {title: t, version: '1'}
paths:
  /x:
    get:
      operationId: x
      parameters:
        - {name: q, in: query, schema: {$ref: '#/components/schemas/Holder/properties/inner'}}
      responses: {'200': {description: ok}}
components:
  schemas:
    Base: {type: integer}
    Holder:
      type: object
      properties:
        inner:
          $ref: '#/components/schemas/Base'
          minimum: 7
          example: 9
`)
	openapitest.RequireNoErrorDiags(t, diags)
	inner, ok := doc.Types["t/anon/components/schemas/Holder/properties/inner"].(*ir.Scalar)
	require.True(t, ok, "the referenced sub-schema hoists an alias at its own pointer")

	require.NotNil(t, inner.Base)
	assert.Equal(t, ir.TypeID("t/openapi/components/schemas/Base"), inner.Base.Target, "the alias points at what it referenced")
	require.NotNil(t, inner.Constraints)
	require.NotNil(t, inner.Constraints.Min)
	assert.Equal(t, ir.BigVal("7"), *inner.Constraints.Min)
	require.Len(t, inner.Examples, 1)
	require.NotNil(t, inner.Examples[0].Value)
	assert.Equal(t, ir.BigVal("9"), inner.Examples[0].Value.Num)

	base, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/Base")].(*ir.Scalar)
	require.True(t, ok)
	assert.Nil(t, base.Constraints, "the referent stays unbounded")
	assert.Empty(t, base.Common().Examples, "and unannotated")
}

// TestSchemaSiblings_RefdSubSchemaBareRefAliases is the no-siblings control: a
// sub-schema that is a bare $ref still interns as an alias to its target rather
// than as a second structural copy of it under the sub-schema's own ID.
func TestSchemaSiblings_RefdSubSchemaBareRefAliases(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `
openapi: 3.1.0
info: {title: t, version: '1'}
paths:
  /x:
    get:
      operationId: x
      parameters:
        - {name: q, in: query, schema: {$ref: '#/components/schemas/Holder/properties/inner'}}
      responses: {'200': {description: ok}}
components:
  schemas:
    Base: {type: object, properties: {n: {type: string}}}
    Holder:
      type: object
      properties:
        inner: {$ref: '#/components/schemas/Base'}
`)
	openapitest.RequireNoErrorDiags(t, diags)
	inner, ok := doc.Types["t/anon/components/schemas/Holder/properties/inner"].(*ir.Scalar)
	require.True(t, ok, "a bare $ref sub-schema aliases rather than copying its target")
	require.NotNil(t, inner.Base)
	assert.Equal(t, ir.TypeID("t/openapi/components/schemas/Base"), inner.Base.Target)

	_, isModel := doc.Types[ir.TypeID("t/openapi/components/schemas/Base")].(*ir.Model)
	assert.True(t, isModel, "the structure lives at the component it was declared at")
}

// TestCrossPathReference_SubtreeIsNamedByItsDeclaration is the #529
// reproduction. A $ref from one path's schema into another path's body once
// interned the subtree beneath its target when the referencing path lowered
// first, under names composed from the reference's own guess.
//
// For each root the owner can declare its schema at, three documents are
// compiled: the owner alone, the owner with /a's reference declared first, and
// the owner declared first. The registry at the owner's schema pointer must
// come out identical in all three.
func TestCrossPathReference_SubtreeIsNamedByItsDeclaration(t *testing.T) {
	t.Parallel()
	const preamble = "openapi: 3.1.0\ninfo: {title: O, version: \"1.0.0\"}\n"
	const ownerSchema = "{type: array, items: {type: array, items: {type: object, properties: {v: {type: string}}}}}"

	for _, root := range crossPathSchemaRoots() {
		t.Run(root.name, func(t *testing.T) {
			t.Parallel()
			aBlock := "  /a:\n    get:\n      operationId: getA\n      responses:\n" +
				"        \"200\":\n          description: ok\n          content:\n" +
				"            application/json: {schema: {$ref: '#" + root.fragment() + "/items', description: x}}\n"

			pathsAlone := "paths:\n" + root.block(ownerSchema)
			pathsRefFirst := "paths:\n" + aBlock + root.block(ownerSchema)
			pathsOwnerFirst := "paths:\n" + root.block(ownerSchema) + aBlock
			webhooksBlock := ""
			if root.webhook {
				pathsAlone = "paths: {}\n"
				pathsRefFirst = "paths:\n" + aBlock
				pathsOwnerFirst = "paths:\n" + aBlock
				webhooksBlock = "webhooks:\n" + root.block(ownerSchema)
			}

			alone, diags := parseFull(t, preamble+pathsAlone+webhooksBlock)
			openapitest.RequireNoErrorDiags(t, diags)
			refFirst, diags := parseFull(t, preamble+pathsRefFirst+webhooksBlock)
			openapitest.RequireNoErrorDiags(t, diags)
			ownerFirst, diags := parseFull(t, preamble+pathsOwnerFirst+webhooksBlock)
			openapitest.RequireNoErrorDiags(t, diags)

			aloneSub := subtreeAt(alone, root.pointer)
			require.NotEmpty(t, aloneSub, "the owner's own declaration hoists a subtree")
			assert.Empty(t, cmp.Diff(aloneSub, subtreeAt(refFirst, root.pointer)),
				"the reference declared first must not change the owner's own subtree")
			assert.Empty(t, cmp.Diff(aloneSub, subtreeAt(ownerFirst, root.pointer)),
				"nor must declaring the owner first")
		})
	}
}

// TestUndeclaredRegion_TwoReferencesAgreeInBothOrders is #529's second gap: a
// position no declaration lowers, such as one beneath not, if, a $defs entry or
// a dependentSchemas entry (the others kept verbatim in Unmodeled,
// docs/ir/boundaries.md#validation-only-constructs, and a $defs entry nowhere,
// docs/compilers/openapi.md#vocabulary-coverage), is reached only by
// references. Two references into one such region, one aimed at the outer
// position and one beneath it, name the inner position alike in either order,
// because its name is a function of its coordinate (lowering.NameAt).
func TestUndeclaredRegion_TwoReferencesAgreeInBothOrders(t *testing.T) {
	t.Parallel()
	const preamble = "openapi: 3.1.0\ninfo: {title: O, version: \"1.0.0\"}\npaths:\n"
	const innerSchema = "{type: array, items: {type: array, items: {type: object, properties: {v: {type: string}}}}}"

	for _, tc := range []struct {
		name string
		wrap func(inner string) string
		// suffix is the RFC 6901 pointer from the schema root to the outer,
		// undeclared position wrap creates.
		suffix string
	}{
		{"not", func(inner string) string { return "{type: object, not: " + inner + "}" }, "not"},
		{"if", func(inner string) string { return "{type: object, if: " + inner + "}" }, "if"},
		{"a $defs entry", func(inner string) string {
			return "{type: object, $defs: {D: " + inner + "}}"
		}, "$defs/D"},
		{"a dependentSchemas entry", func(inner string) string {
			return "{type: object, dependentSchemas: {k: " + inner + "}}"
		}, "dependentSchemas/k"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			bBlock := "  /b:\n    get:\n      operationId: getB\n      responses:\n" +
				"        \"200\":\n          description: ok\n          content:\n" +
				"            application/json: {schema: " + tc.wrap(innerSchema) + "}\n"
			outer := "/paths/~1b/get/responses/200/content/application~1json/schema/" + tc.suffix

			aBlock := "  /a:\n    get:\n      operationId: getA\n      responses:\n" +
				"        \"200\":\n          description: ok\n          content:\n" +
				"            application/json: {schema: {$ref: '#" + outer + "/items'}}\n"
			cBlock := "  /c:\n    get:\n      operationId: getC\n      responses:\n" +
				"        \"200\":\n          description: ok\n          content:\n" +
				"            application/json: {schema: {$ref: '#" + outer + "'}}\n"

			// /a's reference is aimed one level beneath /c's — declared first,
			// this is the order that once named it wrong.
			innerFirst, diags := parseFull(t, preamble+bBlock+aBlock+cBlock)
			openapitest.RequireNoErrorDiags(t, diags)
			outerFirst, diags := parseFull(t, preamble+bBlock+cBlock+aBlock)
			openapitest.RequireNoErrorDiags(t, diags)

			assert.NotEmpty(t, innerFirst.Types, "the region is reachable at all")
			assert.Empty(t, cmp.Diff(innerFirst.Types, outerFirst.Types),
				"the undeclared region is named the same whichever reference reaches it first")
		})
	}
}

// TestCrossPathReference_ComposedMultipartKeepsItsEncodingKeys pins that a
// multipart body naming another operation's composed body keys its parts by
// the properties that body's model holds, in either declaration order.
//
// /b's multipart body composes Base through allOf, so the property "file" that
// /a's encoding key names lives on Base. The key is found by flattening the
// composition (partMembers), which needs /b's node, and /a's reference leaves
// building it to /b, so the keys are settled once every node exists (partKeys);
// read earlier, the key would fall back to a property ID that exists nowhere.
func TestCrossPathReference_ComposedMultipartKeepsItsEncodingKeys(t *testing.T) {
	t.Parallel()
	const preamble = "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths:\n"
	const aBlock = `  /a:
    post:
      operationId: postA
      requestBody:
        content:
          multipart/form-data:
            schema:
              $ref: '#/paths/~1b/post/requestBody/content/multipart~1form-data/schema'
            encoding:
              file: {contentType: application/octet-stream}
      responses:
        "204": {description: ok}
`
	const bBlock = `  /b:
    post:
      operationId: postB
      requestBody:
        content:
          multipart/form-data:
            schema:
              allOf:
                - $ref: '#/components/schemas/Base'
              type: object
              properties:
                meta: {type: string}
      responses:
        "204": {description: ok}
`
	const components = "components:\n  schemas:\n    Base:\n      type: object\n      properties:\n" +
		"        file: {type: string, format: binary}\n"

	for _, tc := range []struct{ name, spec string }{
		{"reference declared first", preamble + aBlock + bBlock + components},
		{"owner declared first", preamble + bBlock + aBlock + components},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, tc.spec)
			openapitest.RequireNoErrorDiags(t, diags)

			base, ok := typeByName(doc, "Base").(*ir.Model)
			require.True(t, ok, "Base lowers to a model")
			baseFile, ok := openapitest.PropsByWire(base.Properties)["file"]
			require.True(t, ok, "Base declares file")

			postA := openapitest.FindOp(t, doc, "postA")
			require.NotNil(t, postA.Request)
			require.Len(t, postA.Request.Contents, 1)
			enc := postA.Request.Contents[0].Encoding
			require.Len(t, enc, 1, "meta carries no explicit encoding and is left out")
			for propID := range enc {
				assert.Equal(t, baseFile.ID, propID,
					"the encoding key names the property Base itself declares, not a pointer under /a")
			}
		})
	}
}

// TestCrossPathReference_ADiagnosticIsReportedOnce pins that a $ref naming a
// position under another operation, declared before it, adds no diagnostic to
// what the position reports lowered alone: the reference leaves lowering it to
// its declaration, so it is lowered, and reports, once. The fixture
// co-declares oneOf and anyOf at the referenced position, which reports a
// message naming only those keywords (preserveUnusedCombinator).
func TestCrossPathReference_ADiagnosticIsReportedOnce(t *testing.T) {
	t.Parallel()
	const preamble = "openapi: 3.1.0\ninfo: {title: O, version: \"1.0.0\"}\npaths:\n"
	const ownerSchema = "{type: object, oneOf: [{type: object, properties: {x: {type: string}}}], " +
		"anyOf: [{type: object, properties: {y: {type: string}}}]}"
	const bBlock = "  /b:\n    get:\n      operationId: getB\n      responses:\n" +
		"        \"200\":\n          description: ok\n          content:\n" +
		"            application/json: {schema: " + ownerSchema + "}\n"
	const aBlock = "  /a:\n    get:\n      operationId: getA\n      responses:\n" +
		"        \"200\":\n          description: ok\n          content:\n" +
		"            application/json: {schema: {$ref: " +
		"'#/paths/~1b/get/responses/200/content/application~1json/schema', description: x}}\n"

	_, aloneDiags := parseFull(t, preamble+bBlock)
	openapitest.RequireNoErrorDiags(t, aloneDiags)
	require.NotEmpty(t, aloneDiags, "the fixture is chosen to emit a diagnostic when lowered once")

	_, refFirstDiags := parseFull(t, preamble+aBlock+bBlock)
	openapitest.RequireNoErrorDiags(t, refFirstDiags)

	assert.Equal(t, sortedDiagStrings(aloneDiags), sortedDiagStrings(refFirstDiags),
		"the reference adds no copy of a diagnostic the declaration reports")
}

// TestComponentBody_SubtreeIsNamedByItsDeclaration is #529 for a schema inside
// a component response, parameter, request body or header. Components lower
// before operations, so a schema's $ref into the body always reaches it first;
// it builds nothing, and the component, used from /u, names the subtree
// (GitHub #747).
//
// Each row compiles the owner alone and with a $ref from components/schemas to
// its items, the components declared in both orders. The registry at the
// owner's schema pointer must come out identical.
func TestComponentBody_SubtreeIsNamedByItsDeclaration(t *testing.T) {
	t.Parallel()
	const preamble = "openapi: 3.1.0\ninfo: {title: O, version: \"1.0.0\"}\n"
	const ownerSchema = "{type: array, items: {type: array, items: {type: object, properties: {v: {type: string}}}}}"
	for _, tc := range []struct{ name, pointer, paths, owner string }{
		{"response", "/components/responses/R/content/application~1json/schema",
			"paths:\n  /u:\n    get:\n      operationId: useR\n      responses:\n        \"200\": {$ref: '#/components/responses/R'}\n",
			"  responses:\n    R: {description: ok, content: {application/json: {schema: " + ownerSchema + "}}}\n"},
		{"parameter", "/components/parameters/P/schema",
			"paths:\n  /u:\n    get:\n      operationId: useP\n      parameters: [{$ref: '#/components/parameters/P'}]\n      responses: {\"204\": {description: ok}}\n",
			"  parameters:\n    P: {name: q, in: query, schema: " + ownerSchema + "}\n"},
		{"request body", "/components/requestBodies/B/content/application~1json/schema",
			"paths:\n  /u:\n    post:\n      operationId: useB\n      requestBody: {$ref: '#/components/requestBodies/B'}\n" +
				"      responses: {\"204\": {description: ok}}\n",
			"  requestBodies:\n    B: {content: {application/json: {schema: " + ownerSchema + "}}}\n"},
		{"header", "/components/headers/H/schema",
			"paths:\n  /u:\n    get:\n      operationId: useH\n      responses:\n        \"200\":\n" +
				"          description: ok\n          headers: {X-H: {$ref: '#/components/headers/H'}}\n",
			"  headers:\n    H: {schema: " + ownerSchema + "}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			refs := "  schemas:\n    Out: {$ref: '#" + tc.pointer + "/items'}\n"
			alone, diags := parseFull(t, preamble+tc.paths+"components:\n"+tc.owner)
			openapitest.RequireNoErrorDiags(t, diags)
			aloneSub := subtreeAt(alone, tc.pointer)
			require.NotEmpty(t, aloneSub, "the owner's declaration hoists a subtree")

			for _, order := range []struct{ name, comps string }{
				{"owner declared first", tc.owner + refs},
				{"reference declared first", refs + tc.owner},
			} {
				doc, diags := parseFull(t, preamble+tc.paths+"components:\n"+order.comps)
				openapitest.RequireNoErrorDiags(t, diags)
				assert.Empty(t, cmp.Diff(aloneSub, subtreeAt(doc, tc.pointer)), order.name)
			}
		})
	}
}

// TestRefSiteKeywords_KeptAtEveryPosition covers the census the $ref path never
// ran (GitHub #283). A keyword beside a `$ref` is conjoined with it, and the
// alias over the target has no property set, member set, value or encoding to
// hold it, so each must be kept verbatim.
//
// Every row runs at a component (annotation.HomeOwnNode, kept on the alias it
// hoists) and at a property (annotation.HomeCarrier, kept on itself), which
// take different paths.
//
// allOf is here for another reason (GitHub #406): a $ref site elects no
// composition family, so only refSiteUnhomedKeywords carries it to the alias.
func TestRefSiteKeywords_KeptAtEveryPosition(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ keyword, sibling, target, raw string }{
		{"format", "format: email", "BaseStr", `"email"`},
		{"enum", "enum: [a, b]", "BaseStr", `["a","b"]`},
		{"const", "const: a", "BaseStr", `"a"`},
		{"required", "required: [a]", "BaseObj", `["a"]`},
		{"additionalProperties", "additionalProperties: false", "BaseObj", `false`},
		{"allOf", "allOf: [{type: string}]", "BaseObj", `[{"type":"string"}]`},
	} {
		t.Run(tc.keyword, func(t *testing.T) {
			t.Parallel()
			site := "{$ref: '#/components/schemas/" + tc.target + "', " + tc.sibling + "}"
			doc, diags := schema.LowerSpec(t, keywordCensusSpec(
				"    Alias: "+site+"\n"+
					"    Holder: {type: object, properties: {p: "+site+"}}\n"))
			openapitest.RequireNoErrorDiags(t, diags)
			key := "openapi:" + tc.keyword

			alias, ok := typeByName(doc, "Alias").(*ir.Scalar)
			require.True(t, ok, "the component position hoists an alias to hold what it wrote")
			require.NotNil(t, alias.Base)
			assert.Equal(t, componentID(tc.target), alias.Base.Target, "and still aliases the target")
			assertKeptVerbatim(t, alias.Unmodeled, key, tc.raw)
			assert.Contains(t,
				openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/Alias"),
				"no home for "+tc.keyword)

			holder, ok := typeByName(doc, "Holder").(*ir.Model)
			require.True(t, ok)
			p, ok := openapitest.PropsByWire(holder.Properties)["p"]
			require.True(t, ok)
			assert.Equal(t, componentID(tc.target), p.Type.Target,
				"the carrier still resolves straight to the target; only the loss is now recorded")
			assertKeptVerbatim(t, p.Unmodeled, key, tc.raw)
			assert.Contains(t,
				openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo,
					"/components/schemas/Holder/properties/p"),
				"no home for "+tc.keyword)
		})
	}
}

// TestRefSiteKeywords_UnionKeptAtEveryPosition covers oneOf/anyOf co-declared
// beside a $ref at the same level (GitHub #406). Under JSON Schema 2020-12 —
// and so OpenAPI 3.1 — `$ref` conjoins with its siblings, so the union
// narrows the referenced schema; the union family's one keeper,
// preserveUnionSiblings, was reached only through the structural-body path
// (lowerBesideUnmodeledUnion), so a $ref-peeling declaration reached it not
// at all. Both ref-site positions reach that keeper, a carrier through its body
// (preserveUnionSiblingsAt), instead of a second one.
//
// Both positions are checked, as TestRefSiteKeywords_KeptAtEveryPosition
// checks the rest of the census: a component hoists an alias to hold the
// union, a property keeps it on itself.
func TestRefSiteKeywords_UnionKeptAtEveryPosition(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ keyword, sibling string }{
		{"oneOf", "oneOf: [{type: string}, {type: integer}]"},
		{"anyOf", "anyOf: [{type: string}, {type: integer}]"},
	} {
		t.Run(tc.keyword, func(t *testing.T) {
			t.Parallel()
			site := "{$ref: '#/components/schemas/BaseObj', " + tc.sibling + "}"
			doc, diags := schema.LowerSpec(t, keywordCensusSpec(
				"    Alias: "+site+"\n"+
					"    Holder: {type: object, properties: {p: "+site+"}}\n"))
			openapitest.RequireNoErrorDiags(t, diags)
			key := "openapi:" + tc.keyword
			raw := `[{"type":"string"},{"type":"integer"}]`

			alias, ok := typeByName(doc, "Alias").(*ir.Scalar)
			require.True(t, ok, "the component position hoists an alias to hold the union its $ref cannot")
			require.NotNil(t, alias.Base)
			assert.Equal(t, componentID("BaseObj"), alias.Base.Target, "and still aliases the target")
			assertKeptVerbatim(t, alias.Unmodeled, key, raw)
			assert.Contains(t,
				openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/Alias"),
				"union branches kept verbatim")

			holder, ok := typeByName(doc, "Holder").(*ir.Model)
			require.True(t, ok)
			p, ok := openapitest.PropsByWire(holder.Properties)["p"]
			require.True(t, ok)
			assert.Equal(t, componentID("BaseObj"), p.Type.Target,
				"the carrier still resolves straight to the target; only the loss is now recorded")
			assertKeptVerbatim(t, p.Unmodeled, key, raw)
			assert.Contains(t,
				openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo,
					"/components/schemas/Holder/properties/p"),
				"union branches kept verbatim")
		})
	}
}

// TestRefSiteKeywords_SiblingsWithATypedHomeAreUntouched is the control the
// census has to leave alone. A bound and a description written at the identical
// position always did reach a field — the alias's Constraints and the property's
// own — so neither may acquire an Unmodeled entry or a diagnostic now.
func TestRefSiteKeywords_SiblingsWithATypedHomeAreUntouched(t *testing.T) {
	t.Parallel()
	site := "{$ref: '#/components/schemas/BaseStr', minLength: 3, description: kept}"
	doc, diags := schema.LowerSpec(t, keywordCensusSpec(
		"    Alias: "+site+"\n"+
			"    Holder: {type: object, properties: {p: "+site+"}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Zero(t, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityInfo),
		"nothing was degraded: %+v", diags)

	alias, ok := typeByName(doc, "Alias").(*ir.Scalar)
	require.True(t, ok)
	require.NotNil(t, alias.Constraints)
	assert.Equal(t, int64(3), *alias.Constraints.MinLength)
	assert.Equal(t, "kept", alias.Docs.Description)
	assert.Empty(t, unmodeledKeys(alias.Unmodeled))

	holder, ok := typeByName(doc, "Holder").(*ir.Model)
	require.True(t, ok)
	p, ok := openapitest.PropsByWire(holder.Properties)["p"]
	require.True(t, ok)
	require.NotNil(t, p.Constraints)
	assert.Equal(t, int64(3), *p.Constraints.MinLength)
	assert.Equal(t, "kept", p.Docs.Description)
	assert.Empty(t, unmodeledKeys(p.Unmodeled))
}

// TestRefSiteKeywords_AllOfBranchKeepsWhatTheAliasCannotHold runs the same
// census at the third $ref site: an allOf branch spelled as a reference, which
// homes its siblings on an alias exactly as a component does.
//
// `required` is the branch keyword this composition does read — requireMembers
// lands every branch's list on the composed model — so it must not be kept, or
// one keyword would be read twice.
func TestRefSiteKeywords_AllOfBranchKeepsWhatTheAliasCannotHold(t *testing.T) {
	t.Parallel()
	doc, diags := schema.LowerSpec(t, keywordCensusSpec(
		"    S: {allOf: [{$ref: '#/components/schemas/BaseObj', format: email, required: [a]}]}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	m, ok := typeByName(doc, "S").(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, m.Base, "the branch still composes")
	branch, ok := doc.Types[m.Base.Target].(*ir.Scalar)
	require.True(t, ok, "through an alias hoisted at the branch position")
	assert.Equal(t, []string{"openapi:format"}, unmodeledKeys(branch.Unmodeled),
		"required is read by the composition, so only the format is kept")
	assert.Contains(t,
		openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/S/allOf/0"),
		"no home for format")
}

// TestRefSiteKeywords_AllOfBranchKeepsUnion covers the same #406 gap at the
// third $ref site: an allOf branch spelled as a $ref that co-declares
// oneOf/anyOf, or a nested allOf, beside it. fillAllOf's branch census never
// named oneOf/anyOf/allOf, so a branch's own $ref beside one composed
// straight to the target with the sibling read by nothing (no alias, no
// Unmodeled, no diagnostic) — the same mechanism as the component and
// carrier positions.
//
// allOf reuses refSiteUnhomedKeywords, the same list the component and
// carrier sites use; oneOf/anyOf reuse preserveUnionSiblings, the same
// keeper. Neither gets a keeper of its own for the branch position.
func TestRefSiteKeywords_AllOfBranchKeepsUnion(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ keyword, sibling, raw, wantMsg string }{
		{"oneOf", "oneOf: [{type: string}, {type: integer}]",
			`[{"type":"string"},{"type":"integer"}]`,
			"there is nothing at this position to distribute the union's composition across"},
		{"anyOf", "anyOf: [{type: string}, {type: integer}]",
			`[{"type":"string"},{"type":"integer"}]`,
			"there is nothing at this position to distribute the union's composition across"},
		{"allOf", "allOf: [{type: string}]", `[{"type":"string"}]`, "no home for allOf"},
	} {
		t.Run(tc.keyword, func(t *testing.T) {
			t.Parallel()
			doc, diags := schema.LowerSpec(t, keywordCensusSpec(
				"    S: {allOf: [{$ref: '#/components/schemas/BaseObj', "+tc.sibling+"}]}\n"))
			openapitest.RequireNoErrorDiags(t, diags)

			m, ok := typeByName(doc, "S").(*ir.Model)
			require.True(t, ok)
			require.NotNil(t, m.Base, "the branch still composes")
			branch, ok := doc.Types[m.Base.Target].(*ir.Scalar)
			require.True(t, ok, "the branch position hoists an alias over its target, exactly as a component or carrier would")
			require.NotNil(t, branch.Base)
			assert.Equal(t, componentID("BaseObj"), branch.Base.Target)
			assertKeptVerbatim(t, branch.Unmodeled, "openapi:"+tc.keyword, tc.raw)
			assert.Contains(t,
				openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/S/allOf/0"),
				tc.wantMsg)
		})
	}
}

// TestSchemaRef_ToAPointerEndingInSlashNamesAnEmptyKey pins a $ref ending in
// '/' as RFC 6901 reads it (GitHub #770): naming the member whose key is the
// empty string, never the parent. Where there is none it is unresolved; where
// there is one, it lowers that.
func TestSchemaRef_ToAPointerEndingInSlashNamesAnEmptyKey(t *testing.T) {
	t.Parallel()
	_, diags := lowerSpecOnly(t, openapitest.ComponentSpec("    A: {type: object, properties: {p: {$ref: '#/x-lib/T/'}}}\n")+
		"x-lib:\n  T: {type: object, properties: {t: {type: string}}}\n")
	assert.Equal(t, `unresolved $ref "#/x-lib/T/": it names nothing in the document it points into`,
		openapitest.DiagMessageAt(t, diags, diag.UnresolvedRef, ir.SeverityError, "/components/schemas/A/properties/p"))

	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec("    A: {type: object, properties: {p: {$ref: '#/x-lib/T/'}}}\n")+
		"x-lib:\n  T: {'': {type: integer}, type: object}\n")
	openapitest.RequireNoErrorDiags(t, diags)
	empty, ok := doc.Types[ir.TypeID("t/anon/x-lib/T/")].(*ir.Scalar)
	require.True(t, ok, "the empty key's schema, not T's object")
	assert.Equal(t, ir.TypeID("t/prim/integer"), empty.Base.Target)
}

// crossPathSchemaRoot names one shape /paths (or /webhooks) can hold a schema
// at, for TestCrossPathReference_SubtreeIsNamedByItsDeclaration. The pointer
// alone names none of them: each is named for an operationId, a response, a
// parameter or a webhook, which the namer reads from the document.
type crossPathSchemaRoot struct {
	name string
	// block returns /b's (or the webhook's) top-level path-item entry with
	// schema spliced in, indented to sit directly under paths: or webhooks:.
	block func(schema string) string
	// webhook is true when block belongs under webhooks: instead of paths:.
	webhook bool
	// pointer is the RFC 6901 pointer to the schema position block declares.
	pointer string
	// ref spells pointer as a $ref fragment when that needs escaping beyond
	// RFC 6901; empty means pointer is written as it is.
	ref string
}

// fragment returns the pointer as a $ref spells it.
func (r crossPathSchemaRoot) fragment() string {
	if r.ref != "" {
		return r.ref
	}
	return r.pointer
}

func crossPathSchemaRoots() []crossPathSchemaRoot {
	return []crossPathSchemaRoot{
		{
			name: "response",
			block: func(schema string) string {
				return "  /b:\n    get:\n      operationId: getB\n      responses:\n" +
					"        \"200\":\n          description: ok\n          content:\n" +
					"            application/json: {schema: " + schema + "}\n"
			},
			pointer: "/paths/~1b/get/responses/200/content/application~1json/schema",
		},
		{
			name: "request body",
			block: func(schema string) string {
				return "  /b:\n    post:\n      operationId: postB\n      requestBody:\n" +
					"        content:\n          application/json: {schema: " + schema + "}\n" +
					"      responses: {\"204\": {description: ok}}\n"
			},
			pointer: "/paths/~1b/post/requestBody/content/application~1json/schema",
		},
		{
			name: "parameter",
			block: func(schema string) string {
				return "  /b:\n    get:\n      operationId: getB\n      parameters:\n" +
					"        - {name: q, in: query, schema: " + schema + "}\n" +
					"      responses: {\"204\": {description: ok}}\n"
			},
			pointer: "/paths/~1b/get/parameters/0/schema",
		},
		{
			name: "header",
			block: func(schema string) string {
				return "  /b:\n    get:\n      operationId: getB\n      responses:\n" +
					"        \"200\":\n          description: ok\n          headers:\n" +
					"            X-H: {schema: " + schema + "}\n"
			},
			pointer: "/paths/~1b/get/responses/200/headers/X-H/schema",
		},
		{
			name: "webhook",
			block: func(schema string) string {
				return "  hook:\n    post:\n      operationId: hookB\n      requestBody:\n" +
					"        content:\n          application/json: {schema: " + schema + "}\n" +
					"      responses: {\"204\": {description: ok}}\n"
			},
			webhook: true,
			pointer: "/webhooks/hook/post/requestBody/content/application~1json/schema",
		},
		{
			name: "callback",
			block: func(schema string) string {
				return "  /b:\n    post:\n      operationId: postB\n      responses: {\"204\": {description: ok}}\n" +
					"      callbacks:\n        cb:\n          '{$request.body#/url}':\n            post:\n" +
					"              operationId: cbB\n              requestBody:\n                content:\n" +
					"                  application/json: {schema: " + schema + "}\n" +
					"              responses: {\"204\": {description: ok}}\n"
			},
			pointer: "/paths/~1b/post/callbacks/cb/{$request.body#~1url}/post/requestBody/content/application~1json/schema",
			ref:     "/paths/~1b/post/callbacks/cb/%7B$request.body%23~1url%7D/post/requestBody/content/application~1json/schema",
		},
	}
}

// subtreeAt returns the registry entries at or under the node pointer itself
// derives an ID from, so two documents naming the same position under
// different, immaterial surroundings (an unrelated path declared before or
// after it) can be compared on that position alone.
func subtreeAt(doc *ir.Document, pointer string) map[ir.TypeID]ir.TypeDef {
	base := "t/anon" + pointer
	out := map[ir.TypeID]ir.TypeDef{}
	for id, td := range doc.Types {
		if s := string(id); s == base || strings.HasPrefix(s, base+"/") {
			out[id] = td
		}
	}
	return out
}

// sortedDiagStrings renders diags as a sorted multiset of their full values, so
// two diagnostic lists can be compared on what they report rather than on the
// order two different lowering walks happened to append it in.
func sortedDiagStrings(diags []ir.Diagnostic) []string {
	out := make([]string, len(diags))
	for i, d := range diags {
		out[i] = fmt.Sprintf("%s\t%s\t%s\t%+v", d.Severity, d.Code, d.Message, d.Provenance)
	}
	slices.Sort(out)
	return out
}

// assertKeptVerbatim asserts p holds key as a degraded-lowering entry whose raw
// payload is raw.
func assertKeptVerbatim(t *testing.T, p ir.Unmodeled, key, raw string) {
	t.Helper()
	entry, ok := p[key]
	require.True(t, ok, "%q is kept verbatim; kept instead: %v", key, unmodeledKeys(p))
	assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
	assert.JSONEq(t, raw, string(entry.Value))
}
