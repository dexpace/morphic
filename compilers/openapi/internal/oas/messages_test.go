package oas_test

import (
	"encoding/json/jsontext"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/ir"
)

// TestCheck_AFindingIsMadeOnceWhereItIsPlaced pins that a finding two positions
// draw about one node, as an alias mounts it at both, is made once, and that
// ReadAt names the position that read it first where that is not where it is
// placed. The locator stands in for the source's, placing both tags at the
// node an anchor writes.
func TestCheck_AFindingIsMadeOnceWhereItIsPlaced(t *testing.T) {
	var n yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(
		"openapi: 3.1.0\ninfo: {title: t, version: v}\ncomponents: {}\ntags: [{name: [x]}, {name: [x]}]\n"), &n))
	anchor := strings.NewReplacer("/tags/0", "/x-tag", "/tags/1", "/x-tag")
	locate := func(p jsontext.Pointer) ir.Provenance {
		return ir.Provenance{Pointer: jsontext.Pointer(anchor.Replace(string(p)))}
	}
	for _, readAt := range []bool{false, true} {
		diags := oas.NewChecker().Check(&n, "3.1.0", oas.Options{Locate: locate, ReadAt: readAt})
		require.Len(t, diags, 1, "one node, one finding: %+v", diags)
		assert.Equal(t, jsontext.Pointer("/x-tag/name"), diags[0].Provenance.Pointer)
		want := `Tag Object field "name" must be a string in OpenAPI 3.1 (Tag Object)`
		if readAt {
			want += "; read at /tags/0/name"
		}
		assert.Equal(t, want, diags[0].Message)
		assert.Equal(t, `Tag Object field "name" must be a string in OpenAPI 3.1 (Tag Object)`,
			oas.AsPlaced(diags[0]).Message, "the finding as placed, whichever position read it")
	}
}

// TestCheck_ReadAtNamesNoPositionAFindingIsPlacedAt pins that a finding read
// where it is placed, or placed nowhere, names no position.
func TestCheck_ReadAtNamesNoPositionAFindingIsPlacedAt(t *testing.T) {
	var n yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("openapi: 3.1.0\ninfo: {title: t, version: v}\ncomponents: {}\ntags: [{name: [x]}]\n"), &n))
	for _, locate := range []oas.Locator{
		func(p jsontext.Pointer) ir.Provenance { return ir.Provenance{Pointer: p} },
		func(jsontext.Pointer) ir.Provenance { return ir.Provenance{} },
	} {
		diags := oas.NewChecker().Check(&n, "3.1.0", oas.Options{Locate: locate, ReadAt: true})
		require.Len(t, diags, 1)
		assert.NotContains(t, diags[0].Message, "read at")
		assert.Equal(t, diags[0], oas.AsPlaced(diags[0]))
	}
}

// TestCheck_AFlowsRequirementNamesTheKeyItIsHeldUnder pins how a rule that
// rests on the key an object is held under spells it: as the key, which the
// document writes, not as a field, which it does not.
func TestCheck_AFlowsRequirementNamesTheKeyItIsHeldUnder(t *testing.T) {
	diags := run(t, "3.1.0", oas.OAuthFlows, `{implicit: {scopes: {}}}`)
	require.Len(t, diags, 1)
	assert.Equal(t, `OAuth Flow Object field "authorizationUrl" is REQUIRED when held under implicit|authorizationCode `+
		`in OpenAPI 3.1 (OAuth Flow Object)`, diags[0].Message)
}
