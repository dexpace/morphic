// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapi_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
)

// TestSwitchOptions_EveryNameIsAMarker holds each option that switches a
// policy to the marker the policy leaves on what it fills, the title-names
// policy aside, which leaves none: a switch spelled as a literal can name a
// marker no code writes, and a marker renamed beside its literal leaves a
// caller switching off a name nothing reads.
func TestSwitchOptions_EveryNameIsAMarker(t *testing.T) {
	t.Parallel()
	markers := map[string]bool{}
	for _, h := range []string{
		string(openapi.NullableInferenceHeuristic), string(openapi.DiscriminatorInferenceHeuristic),
		string(openapi.IdempotencyInferenceHeuristic), string(openapi.SecretInferenceHeuristic),
		string(openapi.HTTPDateInferenceHeuristic), string(openapi.ReplacementInferenceHeuristic),
		string(openapi.PaginationInferenceHeuristic), string(lowering.ExtensionPromotionHeuristic),
		string(lowering.StreamingInferenceHeuristic), string(lowering.BinaryInferenceHeuristic),
	} {
		markers[h] = true
	}

	var o openapi.Options
	for name := range openapi.SwitchOptions(&o) {
		if name == "title-names" {
			continue
		}
		assert.True(t, markers[name], "option %q is no marker a policy leaves", name)
		delete(markers, name)
	}
	assert.Empty(t, markers, "a marker with no option to switch it off")
}

// TestDecodeOptions_RefusesARetiredName pins that the option nullable-inference
// replaces is no alias: the old spelling is an unknown option, refused as any
// is, and the list the refusal prints names its successor.
func TestDecodeOptions_RefusesARetiredName(t *testing.T) {
	t.Parallel()
	_, err := openapi.New().DecodeOptions(compilers.OptionSet{
		Settings: map[string]string{"nullable-widening": "false"},
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, `unknown option "nullable-widening"`)
	assert.ErrorContains(t, err, "nullable-inference")

	_, err = openapi.New().DecodeOptions(compilers.OptionSet{
		Settings: map[string]string{"nullable-inference": "false"},
	})
	assert.NoError(t, err, "the successor is accepted")
}
