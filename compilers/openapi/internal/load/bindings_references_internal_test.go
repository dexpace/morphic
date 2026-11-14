// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"encoding/json/jsontext"
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// sitedFailuresSpec carries one unresolvable $ref at each of the six positions
// GitHub #385 names (parameter, callback, requestBody, response, header,
// pathItem), plus a securityScheme entry, a $ref this compile does not leave
// the document for, and a component schema: nine positions in all, each naming
// its own target.
const sitedFailuresSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    parameters:
      - {$ref: '#/components/parameters/GhostParam'}
    get:
      operationId: getA
      callbacks:
        bad: {$ref: '#/components/callbacks/GhostCb'}
      requestBody: {$ref: '#/components/requestBodies/GhostBody'}
      responses:
        "200": {$ref: '#/components/responses/GhostResp'}
        "201":
          description: ok
          headers:
            X-H: {$ref: '#/components/headers/GhostHeader'}
  /ref: {$ref: '#/components/pathItems/GhostItem'}
  /ext: {$ref: 'other.yaml#/paths/~1x'}
components:
  schemas:
    S: {$ref: '#/components/schemas/GhostSchema'}
  securitySchemes:
    ghost: {$ref: '#/components/securitySchemes/GhostScheme'}
`

// TestBindingDiags_SitesEachFailureAtItsReference pins the shape of every
// failure the bindings report, a schema's $ref or a Reference Object's alike:
// the $ref's own pointer, the exact code and severity, and a message naming the
// reference as written and why, in the compiler's own words.
func TestBindingDiags_SitesEachFailureAtItsReference(t *testing.T) {
	t.Parallel()
	_, diags, err := Load(t.Context(), 0, compilers.Source{Path: "root.yaml", Data: []byte(sitedFailuresSpec)}, Options{})
	require.NoError(t, err)

	const nothing = "it names nothing in the document it points into"
	assertFailures(t, diags, map[jsontext.Pointer]string{
		"/paths/~1a/parameters/0":                  `unresolved $ref "#/components/parameters/GhostParam": ` + nothing,
		"/paths/~1a/get/callbacks/bad":             `unresolved $ref "#/components/callbacks/GhostCb": ` + nothing,
		"/paths/~1a/get/requestBody":               `unresolved $ref "#/components/requestBodies/GhostBody": ` + nothing,
		"/paths/~1a/get/responses/200":             `unresolved $ref "#/components/responses/GhostResp": ` + nothing,
		"/paths/~1a/get/responses/201/headers/X-H": `unresolved $ref "#/components/headers/GhostHeader": ` + nothing,
		"/paths/~1ref":                             `unresolved $ref "#/components/pathItems/GhostItem": ` + nothing,
		"/paths/~1ext": `unresolved $ref "other.yaml#/paths/~1x": it names another document, and external ` +
			"references are not read",
		"/components/schemas/S":             `unresolved $ref "#/components/schemas/GhostSchema": ` + nothing,
		"/components/securitySchemes/ghost": `unresolved $ref "#/components/securitySchemes/GhostScheme": ` + nothing,
	})
}

// TestBindingDiags_AChainIsReportedWhereItFails pins the failure of a reference
// object whose target is a reference that fails: it is reported once, at the
// $ref whose own hop fails, in whichever document writes it, and nothing is
// reported at a reference whose chain passes that one, as for a schema $ref
// (docs/compilers/openapi.md#references).
func TestBindingDiags_AChainIsReportedWhereItFails(t *testing.T) {
	t.Parallel()

	t.Run("within the source", func(t *testing.T) {
		t.Parallel()
		_, diags, err := Load(t.Context(), 0, compilers.Source{Path: "root.yaml", Data: []byte(`openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      parameters:
        - {$ref: '#/components/parameters/Alias'}
      responses:
        "200": {$ref: '#/components/responses/X'}
components:
  parameters:
    Alias: {$ref: 'other.yaml#/components/parameters/P'}
  responses:
    X: {$ref: '#/components/responses/A'}
    A: {$ref: '#/components/responses/B'}
    B: {$ref: '#/components/responses/Missing'}
`)}, Options{})
		require.NoError(t, err)

		assertFailures(t, diags, map[jsontext.Pointer]string{
			"/components/parameters/Alias": `unresolved $ref "other.yaml#/components/parameters/P": ` +
				"it names another document, and external references are not read",
			"/components/responses/B": `unresolved $ref "#/components/responses/Missing": ` +
				"it names nothing in the document it points into",
		})
	})

	t.Run("within another document", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "ext.yaml", "components:\n  responses:\n    A: {$ref: '#/components/responses/Missing'}\n")
		src := compilers.Source{Path: filepath.Join(dir, "root.yaml"), Data: []byte(`openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      responses:
        "200": {$ref: 'ext.yaml#/components/responses/A'}
components:
  responses:
    Missing: {description: the source has one by this name}
`)}

		_, diags, err := Load(t.Context(), 0, src, Options{Reader: reader.Disk{}})
		require.NoError(t, err)

		require.Len(t, diags, 1, "%+v", diags)
		assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/components/responses/A"}, diags[0].Provenance,
			"where ext.yaml writes the $ref that fails")
		assert.Equal(t, `unresolved $ref "#/components/responses/Missing": it names nothing in the document it `+
			"points into", diags[0].Message, "read against ext.yaml, not the source")
	})
}

// TestResolve_OverlayIntroducedReferenceNamesTheOverlay pins the other half of
// placing a pointer of the source: a $ref an overlay adds is attributed to the
// overlay's own source index, not the base document's, because the overlay is
// what wrote it.
func TestResolve_OverlayIntroducedReferenceNamesTheOverlay(t *testing.T) {
	t.Parallel()
	_, diags, err := Load(t.Context(), 0, openapitest.SourceOf(graftBase),
		overlayOptions("  - target: $.components\n    update: {schemas: {Ghost: {$ref: '#/components/schemas/Missing'}}, "+
			"parameters: {Lost: {$ref: '#/components/parameters/Missing'}}}\n"))
	require.NoError(t, err)

	found := map[jsontext.Pointer]string{}
	for _, d := range diags {
		if d.Code != diag.UnresolvedRef {
			continue
		}
		found[d.Provenance.Pointer] = d.Message
		assert.Equal(t, 1, d.Provenance.WrittenBy, "the overlay introduced this $ref, so it is named its writer: %+v", d)
	}
	assert.Equal(t, map[jsontext.Pointer]string{
		"/components/schemas/Ghost": `unresolved $ref "#/components/schemas/Missing": it names nothing in the ` +
			"document it points into",
		"/components/parameters/Lost": `unresolved $ref "#/components/parameters/Missing": it names nothing in ` +
			"the document it points into",
	}, found, "diagnostics: %+v", diags)
}

// TestResolve_AMappingChainReadsADefinitionWhereItIsWritten pins GitHub #996:
// a mapping value's chain meets a "#/$defs/..." $ref as a $ref's own chain
// does, read by the $defs rule from where that $ref is written, never from the
// schema the chain came through. Where the rule finds the definition, the
// chain ends there; where it finds none, the chain fails at that $ref, which
// is reported once, there, and the definition the chain came past is not read:
// S's X, whether it is a schema or names another document. The chain's end is
// what tells, since a chain that read S's X would end on it.
func TestResolve_AMappingChainReadsADefinitionWhereItIsWritten(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "other.yaml", "Y: {type: object}\n")
	const pet = "    Pet:\n      type: object\n      properties: {k: {type: string}}\n" +
		"      discriminator: {propertyName: k, mapping: {c: '#/x-lib/S'}}\n"
	for name, tc := range map[string]struct {
		holder string
		x      string
		want   []string
	}{
		"no definition, one the chain came past": {
			"{type: object, properties: {p: {$ref: '#/$defs/X'}}}", "{type: object}",
			[]string{"/components/schemas/Holder/properties/p error " + diag.UnresolvedRef}},
		"no definition, one naming another document the chain came past": {
			"{type: object, properties: {p: {$ref: '#/$defs/X'}}}", "{$ref: 'other.yaml#/Y'}",
			[]string{"/components/schemas/Holder/properties/p error " + diag.UnresolvedRef}},
		"the holder's own definition": {
			"{type: object, $defs: {X: {type: string}}, properties: {p: {$ref: '#/$defs/X'}}}",
			"{type: object}", []string{}},
	} {
		src := openapitest.ComponentSpec(pet+"    Holder: "+tc.holder+"\n") +
			"x-lib:\n  S: {$defs: {X: " + tc.x + "}, $ref: '#/components/schemas/Holder/properties/p'}\n"
		doc, diags, err := Load(t.Context(), 0, compilers.Source{Path: filepath.Join(dir, "root.yaml"), Data: []byte(src)},
			Options{Reader: reader.Disk{}})
		require.NoError(t, err, name)
		require.NotNil(t, doc, name)

		assert.Equal(t, tc.want, severityLines(diags), name)
		m, ok := doc.Bindings.Table().Mapping(bind.Position{Pointer: "/components/schemas/Pet/discriminator"}, "#/x-lib/S")
		require.True(t, ok, name)
		if len(tc.want) == 0 {
			assert.Equal(t, bind.Position{Pointer: "/components/schemas/Holder/$defs/X"}, m.Chain.End, name)
		} else {
			assert.Equal(t, bind.Unresolved, m.Chain.Status, name)
		}
	}
}

// severityLines renders diags as "pointer severity code", sorted.
func severityLines(diags []ir.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, fmt.Sprintf("%s %s %s", d.Provenance.Pointer, d.Severity, d.Code))
	}
	slices.Sort(out)
	return out
}

// assertFailures requires got to hold exactly one unresolved-ref error per
// pointer in want and nothing else, each message as want says.
func assertFailures(t *testing.T, got []ir.Diagnostic, want map[jsontext.Pointer]string) {
	t.Helper()
	seen := make(map[jsontext.Pointer]int, len(got))
	for _, d := range got {
		seen[d.Provenance.Pointer]++
		message, ok := want[d.Provenance.Pointer]
		if !assert.True(t, ok, "a report at %s: %+v", d.Provenance.Pointer, got) {
			continue
		}
		assert.Equal(t, diag.UnresolvedRef, d.Code)
		assert.Equal(t, ir.SeverityError, d.Severity)
		assert.Equal(t, message, d.Message)
	}
	for pointer := range want {
		assert.Equal(t, 1, seen[pointer], "one report at %s: %+v", pointer, got)
	}
}
