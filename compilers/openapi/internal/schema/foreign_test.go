package schema_test

import (
	"encoding/json/jsontext"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// compileBeside compiles root as the root.yaml of a directory holding files,
// with external references allowed, and returns the document, its diagnostics
// and the directory. The one document the source reads is Sources[1].
func compileBeside(t *testing.T, files map[string]string, root string) (*ir.Document, []ir.Diagnostic, string) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
	}
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: []byte(root)}},
		compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})
	require.NoError(t, err)
	require.NotNil(t, doc)
	require.Len(t, doc.Sources, 2, "the source and the one document it reads")
	return doc, diags, dir
}

// onlyBodyTarget returns the type of the one response body of the document's
// one operation.
func onlyBodyTarget(t *testing.T, doc *ir.Document) ir.TypeID {
	t.Helper()
	require.Len(t, doc.Services, 1)
	require.Len(t, doc.Services[0].Groups, 1)
	require.Len(t, doc.Services[0].Groups[0].Operations, 1)
	op := doc.Services[0].Groups[0].Operations[0]
	require.Len(t, op.Responses, 1)
	return openapitest.BodyTarget(t, op.Responses[0].Payload)
}

// withCode returns the diagnostics among diags that carry code.
func withCode(diags []ir.Diagnostic, code string) []ir.Diagnostic {
	var out []ir.Diagnostic
	for _, d := range diags {
		if d.Code == code {
			out = append(out, d)
		}
	}
	return out
}

// dynamicRefNotExpanded is the report of a $dynamicRef written in another
// document, at the pointer in that document's Document.Sources entry.
func dynamicRefNotExpanded(pointer jsontext.Pointer) ir.Diagnostic {
	return ir.Diagnostic{Severity: ir.SeverityInfo, Code: "openapi/degraded-construct",
		Message: "$dynamicRef was not expanded because it is written in another document, whose " +
			"$dynamicAnchor it names, and only the source's anchors are indexed; it is kept verbatim under Unmodeled",
		Provenance: ir.Provenance{Source: 1, Pointer: pointer}}
}

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

// TestDynamicRef_InAnotherDocumentsContentIsNotExpanded pins a $dynamicRef in
// content another document holds: its fragment names that document's
// $dynamicAnchor, which this compile does not index, so it is kept verbatim on
// the type lowered where that document writes it, rather than expanded to the
// source's anchor of the same name (GitHub #762).
func TestDynamicRef_InAnotherDocumentsContentIsNotExpanded(t *testing.T) {
	t.Parallel()
	doc, diags, _ := compileBeside(t, map[string]string{"ext.yaml": `paths:
  /x:
    get:
      responses:
        "200":
          description: ok
          content: {application/json: {schema: {$dynamicRef: '#node'}}}
components:
  schemas:
    Other: {$dynamicAnchor: node, type: object}
`}, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /p: {$ref: './ext.yaml#/paths/~1x'}
components:
  schemas:
    Node: {$dynamicAnchor: node, type: object}
`)

	const body = jsontext.Pointer("/paths/~1x/get/responses/200/content/application~1json/schema")
	assert.Empty(t, cmp.Diff([]ir.Diagnostic{dynamicRefNotExpanded(body + "/$dynamicRef")}, diags))
	assert.Equal(t, ids.In("ext.yaml").AnonType(body), onlyBodyTarget(t, doc),
		"lowered where ext.yaml writes it")
	held, ok := doc.Types[onlyBodyTarget(t, doc)].(*ir.Scalar)
	require.True(t, ok, "the position holds a node of its own for the reference it keeps")
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: body}, held.Provenance)
	require.NotNil(t, held.Base)
	assert.Equal(t, ir.TypeID("t/prim/any"), held.Base.Target,
		"the source's anchor of the same name is not what the reference names")
	assert.Equal(t, ir.ReasonDegradedLowering, held.Unmodeled["openapi:$dynamicRef"].Reason)
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: body + "/$dynamicRef"},
		held.Unmodeled["openapi:$dynamicRef"].Provenance)
}

// TestDynamicRef_InAnotherDocumentsContentAHoistReachesIsNotExpanded pins the
// same for a schema a $ref reaches: the source's Held names the schema of
// parameter P, a $ref into ext.yaml, so that schema is lowered where ext.yaml
// declares it, and its $dynamicRef is kept on the property, not expanded to
// the source's anchor of that name (GitHub #762).
func TestDynamicRef_InAnotherDocumentsContentAHoistReachesIsNotExpanded(t *testing.T) {
	t.Parallel()
	doc, diags, _ := compileBeside(t, map[string]string{"ext.yaml": `Param:
  name: q
  in: query
  schema: {type: object, properties: {inner: {$dynamicRef: '#node'}}}
`}, `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  parameters:
    P: {$ref: './ext.yaml#/Param'}
  schemas:
    Node: {$dynamicAnchor: node, type: object}
    Held: {$ref: '#/components/parameters/P/schema'}
`)

	ext := ids.In("ext.yaml")
	inner := jsontext.Pointer("/Param/schema/properties/inner")
	assert.Empty(t, cmp.Diff([]ir.Diagnostic{dynamicRefNotExpanded(inner + "/$dynamicRef")}, diags))

	held, ok := doc.Types[ids.Document{}.NamedType("/components/schemas/Held")].(*ir.Scalar)
	require.True(t, ok)
	require.NotNil(t, held.Base)
	assert.Equal(t, ext.AnonType("/Param/schema"), held.Base.Target, "Held names the schema ext.yaml declares")
	model, ok := doc.Types[held.Base.Target].(*ir.Model)
	require.True(t, ok)
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/Param/schema"}, model.Provenance)
	require.Len(t, model.Properties, 1)
	prop := model.Properties[0]
	assert.Equal(t, ext.Prop(inner), prop.ID)
	assert.Equal(t, ir.TypeID("t/prim/any"), prop.Type.Target)
	assert.Equal(t, ir.ReasonDegradedLowering, prop.Unmodeled["openapi:$dynamicRef"].Reason)
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

	l := lowererOver(lowering.New(0, unlisted.Doc, unlisted.Source, lowering.GroupByTags, lowering.Limits{},
		lowering.StreamingMedia{}, lowering.ExtensionPromotions{}, overlay.Origin{}).WithLoaded(&unlisted))
	l.diags.AppendAll(schema.LowerComponentSchemas(t.Context(), l.ctx, l.types, &l.anchors))

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
	}, withCode(l.diags.List(), "openapi/unresolved-ref")))
	sub, ok := l.out.Types[ids.Document{}.NamedType("/components/schemas/Sub")].(*ir.Model)
	require.True(t, ok)
	assert.Equal(t, "Sub", sub.DiscriminatorValue, "the mapping tags no subtype, which takes its own name")
	for id := range l.out.Types {
		space, _ := ir.IDSpace(ir.IDKindType, string(id))
		assert.NotContains(t, space, "@", "%s is lowered in a document the compile does not list", id)
	}
}
