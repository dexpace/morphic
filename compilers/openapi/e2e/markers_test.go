// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/v2"
	"maps"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// markersSpec holds one use of each reading that leaves a marker named for
// its policy: a streaming media type, a binary one and a nullable keyword.
const markersSpec = `openapi: 3.0.3
info: {title: Markers, version: "1.0.0"}
paths:
  /events:
    get:
      operationId: listEvents
      responses:
        '200':
          description: ok
          content:
            application/x-ndjson:
              schema: {type: object, properties: {id: {type: string}}}
  /avatar:
    put:
      operationId: putAvatar
      requestBody:
        content:
          image/png:
            schema: {type: string}
      responses: {'204': {description: ok}}
components:
  schemas:
    Pet:
      type: object
      properties:
        name: {type: string}
    Owner:
      type: object
      properties:
        pet: {allOf: [{$ref: '#/components/schemas/Pet'}], nullable: true}
`

// inferredMark matches every value an inferred member holds in an encoded
// document.
var inferredMark = regexp.MustCompile(`"inferred":"([^"]*)"`)

// compileMarkers compiles markersSpec under the text options settings,
// as a caller of the command line does, and counts each marker the encoded
// document holds.
func compileMarkers(t *testing.T, settings map[string]string) (map[string]int, []ir.Diagnostic) {
	t.Helper()
	opts, err := openapi.New().DecodeOptions(compilers.OptionSet{Settings: settings})
	require.NoError(t, err)
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{openapitest.SourceOf(markersSpec)}, compilers.Options{FormatOptions: opts})
	require.NoError(t, err)
	encoded, err := json.Marshal(doc)
	require.NoError(t, err)
	counts := map[string]int{}
	for _, m := range inferredMark.FindAllSubmatch(encoded, -1) {
		counts[string(m[1])]++
	}
	return counts, diags
}

// TestMarkers_NameTheirOptions pins that the streaming, binary and nullable
// readings each leave the marker their option is spelled as: the document
// holds each once or twice as the spec uses it, the one nullable reading is
// reported under the policy's name, and switching an option off by that name
// removes that marker and no other.
func TestMarkers_NameTheirOptions(t *testing.T) {
	t.Parallel()
	all := map[string]int{"binary-inference": 1, "nullable-inference": 1, "streaming-inference": 2}

	counts, diags := compileMarkers(t, nil)
	assert.Equal(t, all, counts)
	var infos []ir.Diagnostic
	for _, d := range diags {
		if d.Severity == ir.SeverityInfo {
			infos = append(infos, d)
		}
	}
	require.Len(t, infos, 1, "the one info; the spec's own warning is a validation finding: %+v", diags)
	assert.Equal(t, "openapi/inferred-nullable", infos[0].Code)
	assert.EqualValues(t, "/components/schemas/Owner/properties/pet/nullable", infos[0].Provenance.Pointer)
	assert.Contains(t, infos[0].Message, "nullable-inference policy")

	for _, option := range []string{"streaming-inference", "binary-inference", "nullable-inference"} {
		t.Run(option, func(t *testing.T) {
			t.Parallel()
			want := maps.Clone(all)
			delete(want, option)
			got, _ := compileMarkers(t, map[string]string{option: "false"})
			assert.Equal(t, want, got, "%s=false removes its marker and no other", option)
		})
	}
}
