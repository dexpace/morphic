// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestInlinePrincipal_AUnionStandingKeepsItsDiscriminatorsKeys holds the
// discriminator an allOf entry writes beside the union standing at the
// position (docs/compilers/openapi.md#allof-refinement) to keep what it
// declares that no IR field holds, an extension and a key the specification
// does not define, on the node, as the same discriminator written at the
// position does, each at the entry writing it.
func TestInlinePrincipal_AUnionStandingKeepsItsDiscriminatorsKeys(t *testing.T) {
	t.Parallel()
	const (
		disc   = "discriminator: {propertyName: k, x-b: 5, mapped: D}"
		branch = "oneOf: [{$ref: '#/components/schemas/S'}, {$ref: '#/components/schemas/Str'}]"
	)
	targets := []string{"S: {type: object, properties: {k: {type: string}}}", "Str: {type: string}"}
	for _, site := range conjSites {
		if site.name != "component" && site.name != "property" {
			continue
		}
		t.Run(site.name, func(t *testing.T) {
			t.Parallel()
			at := jsontext.Pointer(site.pointer)
			for _, sp := range []struct {
				schema  string
				written jsontext.Pointer
			}{
				{"{allOf: [{" + disc + ", " + branch + "}]}", at + "/allOf/0/discriminator"},
				{"{" + disc + ", " + branch + "}", at + "/discriminator"},
			} {
				doc, _ := compileUnrefused(t, site.write("3.1.0", sp.schema, targets...))
				node := kindNode(t, doc, entryPositionID(at))
				require.Equal(t, ir.KindUnion, node.Kind(), sp.schema)
				for key, reason := range map[string]ir.UnmodeledReason{
					"openapi:discriminator/x-b":    ir.ReasonVendorExtension,
					"openapi:discriminator/mapped": ir.ReasonOutOfScope,
				} {
					kept, ok := node.Common().Unmodeled[key]
					if assert.True(t, ok, "%s keeps %s", sp.schema, key) {
						assert.Equal(t, reason, kept.Reason, "%s: %s", sp.schema, key)
						assert.Equal(t, sp.written+"/"+jsontext.Pointer(key[len("openapi:discriminator/"):]),
							kept.Provenance.Pointer, "%s: %s is kept where it is written", sp.schema, key)
					}
				}
			}
		})
	}
}
