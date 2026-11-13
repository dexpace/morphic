// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// TestServerName_ReadsTheDefaultKey pins a server's name read from the
// key the default maps: text names it and marks the server, and empty text or
// any other value names nothing, the latter reported.
func TestServerName_ReadsTheDefaultKey(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		value    string
		want     string
		reported bool
	}{
		{`"stream"`, "stream", false},
		{`""`, "", false},
		{`3`, "", true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Parallel()
			var prov ir.Provenance
			name, diags := lowering.ServerName(promoter(lowering.ExtensionPromotions{}),
				ir.Unmodeled{"openapi:x-fern-server-name": vendorExtension(tc.value)}, &prov)
			assert.Equal(t, tc.want, name)
			assert.Equal(t, tc.want != "", prov.Inferred == lowering.ExtensionPromotionHeuristic)
			assert.Equal(t, tc.reported, len(diags) == 1, "reports: %+v", diags)
		})
	}
	name, diags := lowering.ServerName(promoter(lowering.ExtensionPromotions{}),
		ir.Unmodeled{"openapi:x-fern-server-name": vendorExtension(`"s"`)}, nil)
	assert.Empty(t, name, "nowhere to record the guess, so none is made")
	assert.Empty(t, diags)
}

// TestTokenEndpointAuthMethods_ReadsASet pins the reading of the one
// target no default key names: a method or a list of distinct ones fills the
// field and marks the flow, an empty list states nothing, and a list holding a
// blank or a repeat, which no set holds, is reported as any other shape is.
func TestTokenEndpointAuthMethods_ReadsASet(t *testing.T) {
	t.Parallel()
	policy := mapping(lowering.TargetTokenEndpointAuthMethods, lowering.ExtensionSource{Key: "x-auth"})
	for _, tc := range []struct {
		value    string
		want     []string
		reported bool
	}{
		{`"private_key_jwt"`, []string{"private_key_jwt"}, false},
		{`["client_secret_basic","private_key_jwt"]`, []string{"client_secret_basic", "private_key_jwt"}, false},
		{`[]`, nil, false},
		{`["a","a"]`, nil, true},
		{`["a",""]`, nil, true},
		{`{"m":"a"}`, nil, true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Parallel()
			var inferred ir.Inferred
			methods, diags := lowering.TokenEndpointAuthMethods(promoter(policy),
				ir.Unmodeled{"openapi:x-auth": vendorExtension(tc.value)}, &inferred)
			assert.Equal(t, tc.want, methods)
			assert.Equal(t, tc.want != nil, inferred == lowering.ExtensionPromotionHeuristic)
			assert.Equal(t, tc.reported, len(diags) == 1, "reports: %+v", diags)
		})
	}
	methods, diags := lowering.TokenEndpointAuthMethods(promoter(policy),
		ir.Unmodeled{"openapi:x-auth": vendorExtension(`"a"`)}, nil)
	assert.Nil(t, methods, "nowhere to record the guess, so none is made")
	assert.Empty(t, diags)
}

// TestAllowReserved_OnlyTrueAsksForIt pins the reading of a key asking
// for a parameter's reserved characters unescaped: true asks it and marks the
// parameter, false asks nothing, a value the source lists asks it, and any
// other shape is reported.
func TestAllowReserved_OnlyTrueAsksForIt(t *testing.T) {
	t.Parallel()
	plain := mapping(lowering.TargetAllowReserved, lowering.ExtensionSource{Key: "x-raw"})
	listed := mapping(lowering.TargetAllowReserved, lowering.ExtensionSource{Key: "x-raw", Values: []string{"yes"}})
	for _, tc := range []struct {
		policy   lowering.ExtensionPromotions
		value    string
		want     bool
		reported bool
	}{
		{plain, `true`, true, false},
		{plain, `false`, false, false},
		{plain, `"yes"`, false, true},
		{listed, `"yes"`, true, false},
		{listed, `true`, false, true},
	} {
		var prov ir.Provenance
		reserved, diags := lowering.AllowReserved(promoter(tc.policy), ir.Unmodeled{"openapi:x-raw": vendorExtension(tc.value)}, &prov)
		assert.Equal(t, tc.want, reserved, tc.value)
		assert.Equal(t, tc.want, prov.Inferred == lowering.ExtensionPromotionHeuristic, tc.value)
		assert.Equal(t, tc.reported, len(diags) == 1, "%s reports: %+v", tc.value, diags)
	}
	reserved, diags := lowering.AllowReserved(promoter(plain), ir.Unmodeled{"openapi:x-raw": vendorExtension(`true`)}, nil)
	assert.False(t, reserved, "nowhere to record the guess, so none is made")
	assert.Empty(t, diags)
}
