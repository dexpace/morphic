// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
)

// landingsSpec adds to kept-ref-landings a second kept keyword naming a position
// a lowered keyword names too, and a discriminator, kept beside a scalar, whose
// mapping value names a position nothing else does.
func landingsSpec(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(conformanceDir, "kept-ref-landings.yaml"))
	require.NoError(t, err)
	return string(data) + `    AlsoKept:
      not: {$ref: '#/components/schemas/Typed/$defs/Sub'}
    Mapped:
      type: string
      $defs: {D: {type: object, properties: {mapped: {type: string}}}}
      discriminator:
        propertyName: kind
        mapping: {x: '#/components/schemas/Mapped/$defs/D'}
`
}

// compileLandings compiles src, a document of no other file, verified
// (compileVerified), and requires a document.
func compileLandings(t *testing.T, name string, src []byte) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags := compileVerified(t, compilers.Source{Path: name, Data: src}, openapi.Options{})
	require.NotNil(t, doc)
	return doc, diags
}

// TestCompile_KeptRefLandingsAreLoweredOnceInEitherOrder pins the positions a
// kept keyword's $ref, and a kept discriminator's mapping value, lands on: each
// is one node, in the namespace of positions no declaration names, so the
// document is the same whichever order the declarations are written in, and no
// two nodes are declared at one pointer.
func TestCompile_KeptRefLandingsAreLoweredOnceInEitherOrder(t *testing.T) {
	t.Parallel()
	src := landingsSpec(t)
	forward, forwardDiags := compileLandings(t, "spec.yaml", []byte(src))
	backward, backwardDiags := compileLandings(t, "spec.yaml", reversed(t, src))

	assert.Empty(t, cmp.Diff(forward, backward,
		cmpopts.IgnoreFields(ir.SourceInfo{}, "Hash"), cmpopts.IgnoreFields(ir.Provenance{}, "Position"),
		cmpopts.IgnoreFields(ir.Document{}, "Diagnostics")), "the two orders lower to one document")
	assert.Equal(t, findingsAt(forwardDiags), findingsAt(backwardDiags))

	landings := []string{
		"/components/schemas/Typed/$defs/Sub", "/components/schemas/Kept/$defs/Sub",
		"/components/schemas/Through/$defs/Mid", "/components/schemas/Through/$defs/End",
		"/components/schemas/Mapped/$defs/D",
	}
	for name, doc := range map[string]*ir.Document{"forward": forward, "backward": backward} {
		for _, pointer := range landings {
			id := ir.TypeID("type/anonymous" + pointer)
			require.Contains(t, doc.Types, id, "%s: %s is lowered", name, pointer)
			assert.NotContains(t, doc.Types, ir.TypeID("type/openapi"+pointer),
				"%s: nothing lowered at %s in the namespace of a declaration", name, pointer)
			var at int
			for _, td := range doc.Types {
				if td.Common().Provenance.Pointer == jsontext.Pointer(pointer) {
					at++
				}
			}
			assert.Equal(t, 1, at, "%s: one node is declared at %s", name, pointer)
		}
	}
}
